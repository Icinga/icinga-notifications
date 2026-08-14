package event

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/icinga/icinga-go-library/backoff"
	"github.com/icinga/icinga-go-library/database"
	"github.com/icinga/icinga-go-library/logging"
	"github.com/icinga/icinga-go-library/retry"
	"github.com/icinga/icinga-go-library/types"
	"github.com/jmoiron/sqlx"
	"go.uber.org/zap"
	"golang.org/x/sync/semaphore"
)

const (
	// claimBatchSize bounds how many pending job_queue entries are fetched from the database in a single batch for claiming.
	claimBatchSize = 64

	// maxConcurrentClaims bounds how many job_queue entries can be claimed and processed concurrently by a single node.
	maxConcurrentClaims = 4

	// The following consts are valid values for Queue.State.
	//
	// QueueStateDone ensures that a source cannot enqueue the same event after it was already processed. If the event
	// would have been already deleted from the database, but the source resubmits it due to some source-facing network
	// issues, it would have been evaluated once again. By keeping evaluated events with the QueueStateDone state in
	// the database for a few minutes eliminates this potential issue.

	// QueueStatePending is the initial state for submitting events.
	QueueStatePending int16 = 0
	// QueueStateProcessing is assigned when the ProcessQueue picked this up for processing.
	QueueStateProcessing int16 = 1
	// QueueStateDone is set after ProcessQueue successfully processed the entry.
	QueueStateDone int16 = 2
	// QueueStateError is assigned to invalid events for further processing.
	QueueStateError int16 = 64
)

var (
	// errClaimLost is returned internally by tryClaim when a candidate could not be claimed because its
	// target object is already locked, or because a concurrent node claimed the same entry first.
	errClaimLost = errors.New("claim lost")

	// errEnvelopeVersionTooNew is returned when a queue entry's envelope version is newer than the version supported by this node.
	errEnvelopeVersionTooNew = errors.New("envelope version is too new")
)

type (
	// Queue describes an event.Event enqueued for processing in the relational database.
	Queue struct {
		// ID is the SHA256 hash based on the event.Event JSON representation.
		ID types.Binary `db:"id"`

		// LastUpdate tracks the last modification, used for both processing and retention.
		LastUpdate types.UnixMilli `db:"last_update"`

		// State of the event. Check "QueueState*" consts above.
		State int16 `db:"state"`

		// Envelope is a wrapper around the JSON representation of any processable entity.
		//
		// The Envelope contains the version and format of the payload it embodies. The format denotes the
		// type of the processable entity, while the version is used to determine if the payload needs a
		// migration step before it can be deserialized into a newer version of the processable entity of
		// the same format.
		Envelope Envelope `db:"envelope"`
	}

	// QueueCallbacks describes the callbacks used by ProcessQueue to process events from the database job queue.
	QueueCallbacks struct {
		// GenObjectID is invoked whenever the queue processor needs to determine the object ID for a given event.Event.
		//
		// This should return a unique object ID from the given tags. This function is needed due to import cycles
		// between the event and object packages.
		GenObjectID func(map[string]string) types.Binary

		// ProcessEvent is invoked for each event.Event fetched from the database job queue.
		//
		// The callback must honor the provided context and return an error if the processing failed.
		ProcessEvent func(context.Context, *Event) error
	}

	// job pairs a Queue entry with its corresponding event.Event for processing.
	job struct {
		Q  *Queue
		Ev *Event
	}

	// jobProcessingLock describes a lock for a job queue entry that is currently being processed.
	jobProcessingLock struct {
		ObjectID types.Binary `db:"object_id"`
		QueueID  types.Binary `db:"job_queue_id"`
	}
)

func (jpl jobProcessingLock) buildInsertIgnore(db *database.DB) string {
	switch db.DriverName() {
	case database.PostgreSQL:
		// The reason for not using db.BuildInsertIgnoreStmt here is that this table has two potential conflict targets:
		//  - object_id: to prevent multiple nodes from processing queue entries for the same object concurrently.
		//  - job_queue_id: to ensure that a queue entry is only processed by one node at a time.
		//
		// However, since PostgreSQL only allows one conflict target constraint per statement, we cannot tell
		// it that either of the two constraints is sufficient to ignore the insert. Also, using either of them
		// as the conflict target would not be correct as we don't know which one of the two constraints will be
		// violated first, so we cannot choose one over the other. Therefore, we have omitted the `ON CONSTRAINT`
		// clause entirely and let PostgreSQL infer[^1] the conflict target automatically, which will work correctly
		// for both constraints. So, instead of patching db.BuildInsertIgnoreStmt to support this, we just write the
		// statement manually here.
		//
		// [^1]: https://www.postgresql.org/docs/current/sql-insert.html#SQL-ON-CONFLICT (See the `conflict_target` section)
		return `INSERT INTO job_processing_lock (object_id, job_queue_id) VALUES (:object_id, :job_queue_id) ON CONFLICT DO NOTHING`
	default:
		stmt, _ := db.BuildInsertIgnoreStmt(&jpl)
		return stmt
	}
}

// TableName implements [database.TableNamer].
func (q *Queue) TableName() string {
	return "job_queue"
}

// toEvent converts the Queue entry into an event.Event.
//
// It returns an error if the envelope format doesn't match EnvelopeFmtEvent or if the payload
// cannot be deserialized into an event.Event.
func (q *Queue) toEvent() (*Event, error) {
	if q.Envelope.Format != EnvelopeFmtEvent {
		return nil, fmt.Errorf("cannot convert envelope of format %q to event.Event", q.Envelope.Format)
	}

	if q.Envelope.Version > EnvelopeEventVersion {
		return nil, fmt.Errorf("%w: got %d, supported <= %d",
			errEnvelopeVersionTooNew, q.Envelope.Version, EnvelopeEventVersion)
	}

	// NOTE: There might be breaking changes with future updates to the event.Event struct, so apply any necessary
	// migration steps here for older envelope versions before deserializing the payload into an event.Event.

	var ev Event
	err := json.NewDecoder(bytes.NewReader(q.Envelope.Payload)).Decode(&ev)
	if err != nil {
		return nil, fmt.Errorf("cannot JSON decode job queue entry: %w", err)
	}

	ev.Time = q.Envelope.Time.Time()

	return &ev, nil
}

// Enqueue enqueues an event.Event into the job queue.
//
// The internally used database transaction is bound to the provided context. When calling this function from an HTTP
// handler, pass the request's context to terminate the transaction if the client disconnects prematurely.
func Enqueue(ctx context.Context, db *database.DB, ev *Event) error {
	jsonBuff := bytes.Buffer{}
	idHash := sha256.New()

	err := json.NewEncoder(io.MultiWriter(&jsonBuff, idHash)).Encode(ev)
	if err != nil {
		return fmt.Errorf("cannot JSON encode event.Event: %w", err)
	}

	q := &Queue{
		ID:         idHash.Sum(nil),
		LastUpdate: types.UnixMilli(time.Now()),
		State:      QueueStatePending,
		Envelope: Envelope{
			Version: EnvelopeEventVersion,
			Format:  EnvelopeFmtEvent,
			Time:    types.UnixMilli(ev.Time),
			Payload: jsonBuff.Bytes(),
		},
	}
	stmt, _ := db.BuildInsertIgnoreStmt(q)

	return retry.WithBackoff(
		ctx,
		func(ctx context.Context) error {
			return db.ExecTx(
				ctx,
				&sql.TxOptions{Isolation: sql.LevelSerializable},
				func(ctx context.Context, tx *sqlx.Tx) error {
					_, err := tx.NamedExecContext(ctx, stmt, q)
					if err != nil {
						return database.CantPerformQuery(err, stmt)
					}

					return nil
				})
		},
		retry.Retryable,
		backoff.DefaultBackoff,
		db.GetDefaultRetrySettings())
}

// ProcessQueue processes events from the job queue in the database and invokes the provided callbacks for each event.
//
// This function blocks until either the provided context is canceled, or an unrecoverable error occurs.
// In either case, a non-nil error is returned.
//
// The callbacks are invoked with a context limited to one minute, and must honor that context.
func ProcessQueue(ctx context.Context, db *database.DB, logger *logging.Logger, cbs QueueCallbacks) error {
	selStmt := fmt.Sprintf(`SELECT * FROM job_queue WHERE state = %d ORDER BY last_update LIMIT %d`,
		QueueStatePending, claimBatchSize)
	// updStmt is used to update the state of a queue entry and its last_update after processing.
	updStmt := `UPDATE job_queue SET last_update = :last_update, state = :state WHERE id = :id`
	// casStmt is used to atomically transition a queue entry from QueueStatePending to QueueStateProcessing,
	// ensuring that no other node has claimed the same entry concurrently.
	casStmt := db.Rebind(`UPDATE job_queue SET last_update = ?, state = ? WHERE id = ? AND state = ?`)
	// lockStmt locks the target object, ensuring that no other node is processing a job for the same object concurrently.
	lockStmt := (jobProcessingLock{}).buildInsertIgnore(db)
	unlockStmt := `DELETE FROM job_processing_lock WHERE job_queue_id = :id`

	// Used to process claimed jobs in a separate goroutine to allow concurrent processing of multiple entries.
	sem := semaphore.NewWeighted(maxConcurrentClaims)
	jobs, errs := startClaiming(ctx, db, logger, cbs, selStmt, updStmt, casStmt, lockStmt)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case err := <-errs:
			if err != nil {
				return fmt.Errorf("cannot claim job queue: %w", err)
			}
			return nil

		case j, ok := <-jobs:
			if !ok {
				return <-errs
			}

			if err := sem.Acquire(ctx, 1); err != nil {
				return fmt.Errorf("cannot acquire semaphore for processing job queue entry: %w", err)
			}

			logger.Debugw("Claimed job queue entry for processing", zap.Stringer("id", j.Q.ID), zap.Object("event", j.Ev))

			go func(j job) {
				defer sem.Release(1)

				if err := processAndFinalize(ctx, db, logger, j, cbs, updStmt, unlockStmt); err != nil {
					logger.Errorw("Failed to process and finalize job queue entry",
						zap.Stringer("id", j.Q.ID),
						zap.Error(err))
				} else {
					logger.Debugw("Successfully processed and finalized job queue entry", zap.Stringer("id", j.Q.ID))
				}
			}(j)
		}
	}
}

// startClaiming fetches a batch of pending job_queue entries and attempts to claim them for processing.
//
// It fetches a bounded batch of pending entries ordered by last_update and evaluates them one by one via
// tryClaim and sends the successfully claimed jobs to the returned channel. If there are no pending events,
// or none of the fetched candidates could be claimed because their target objects are currently locked elsewhere,
// it restarts the process and fetches a new batch of candidates.
func startClaiming(
	ctx context.Context,
	db *database.DB,
	logger *logging.Logger,
	cbs QueueCallbacks,
	selStmt,
	updStmt,
	casStmt,
	lockStmt string,
) (<-chan job, <-chan error) {
	claimedJobCh := make(chan job)
	errCh := make(chan error, 1)

	go func() {
		defer close(claimedJobCh)
		defer close(errCh)

		err := retry.WithBackoff(
			ctx,
			func(ctx context.Context) error {
				for {
					var candidates []Queue
					if err := db.SelectContext(ctx, &candidates, selStmt); err != nil {
						return database.CantPerformQuery(err, selStmt)
					}

					var claimedSome bool // Whether at least one candidate was successfully claimed in this batch.
					for _, q := range candidates {
						var (
							err error
							ev  *Event
						)

						switch q.Envelope.Format {
						case EnvelopeFmtEvent:
							ev, err = q.toEvent()
						default:
							err = errors.New("unknown envelope format")
						}

						if err != nil {
							logger.Errorw("Cannot deserialize job queue entry, marking it as failed",
								zap.Stringer("envelope_format", q.Envelope.Format),
								zap.Stringer("id", q.ID),
								zap.Error(err))

							q.LastUpdate = types.UnixMilli(time.Now())
							q.State = QueueStateError
							if _, err := db.NamedExecContext(ctx, updStmt, &q); err != nil {
								return database.CantPerformQuery(err, updStmt)
							}
							continue
						}

						j := job{Q: &q, Ev: ev}
						switch err := tryClaim(ctx, db, j, casStmt, lockStmt, cbs); {
						case err == nil:
							// If the ctx is canceled while sending the claimed job, we'll inevitably leave the
							// object and queue entry locked, but the ctx cancellation will eventually terminate
							// the whole ProcessQueue call as well. So, the only way to unlock that entry is to
							// wait for the retention.ResetPruner to run, which will unlock all orphaned locks
							// and reset the state of any queue entries that are still locked after a certain timeout.
							select {
							case claimedJobCh <- j:
								claimedSome = true

							case <-ctx.Done():
								return ctx.Err()
							}

						case errors.Is(err, errClaimLost):
							// Nothing to do here, see the comment in tryClaim.

						default:
							return err
						}
					}

					if len(candidates) == 0 || !claimedSome {
						// Either there were no pending events at all, or none of the fetched candidates could be
						// claimed because their target objects are currently locked elsewhere. In either case,
						// return sql.ErrNoRows to indicate that there is nothing to process right now.
						return sql.ErrNoRows
					}
					// Otherwise, re-fetch a new batch of candidates without unnecessarily waiting for the backoff timeout.
				}
			},
			func(err error) bool {
				if errors.Is(err, sql.ErrNoRows) {
					return true
				}
				return retry.Retryable(err)
			},
			backoff.NewExponentialWithJitter(time.Second, 3*time.Second),
			retry.Settings{
				Timeout: 0,
				OnRetryableError: func(elapsed time.Duration, attempt uint64, err, lastErr error) {
					if !errors.Is(err, sql.ErrNoRows) && (lastErr == nil || err.Error() != lastErr.Error()) {
						logger.Errorw("Failed to claim job queue entry for processing, retrying",
							zap.Duration("elapsed", elapsed),
							zap.Uint64("attempt", attempt),
							zap.Error(err))
					}
				},
				OnSuccess: func(elapsed time.Duration, attempt uint64, lastErr error) {
					if attempt > 1 && !errors.Is(lastErr, sql.ErrNoRows) {
						logger.Debugw("Successfully claimed job queue entry for processing after retries",
							zap.Duration("elapsed", elapsed),
							zap.Uint64("attempt", attempt),
							zap.Error(lastErr))
					}
				},
			})
		if err != nil {
			errCh <- fmt.Errorf("cannot claim job queue: %w", err)
		}
	}()

	return claimedJobCh, errCh
}

// tryClaim attempts to atomically claim the provided job queue entry for processing by the current node.
//
// It first inserts a lock for the object into jobProcessingLock, then transitions Job.Q into QueueStateProcessing
// guarded by a compare-and-swap (CAS) operation. Both statements run in a single tx, so losing either race rolls
// back the whole attempt and never leaves an orphaned lock or a half-claimed queue entry behind.
//
// Returns errClaimLost if the object is already locked by another node, or if a concurrent one claimed this
// exact entry first.
func tryClaim(ctx context.Context, db *database.DB, j job, casStmt, lockStmt string, cbs QueueCallbacks) error {
	return db.ExecTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(ctx context.Context, tx *sqlx.Tx) error {
		queueLock := &jobProcessingLock{QueueID: j.Q.ID, ObjectID: cbs.GenObjectID(j.Ev.Tags)}
		res, err := tx.NamedExecContext(ctx, lockStmt, queueLock)
		if err != nil {
			return database.CantPerformQuery(err, lockStmt)
		}
		if affected, err := res.RowsAffected(); err != nil {
			return database.CantPerformQuery(err, lockStmt)
		} else if affected == 0 {
			return errClaimLost // Object is already locked by another node.
		}

		j.Q.LastUpdate = types.UnixMilli(time.Now())
		j.Q.State = QueueStateProcessing

		// Apparently, the object lock was successfully acquired, so now we can attempt to claim the queue entry.
		res, err = tx.ExecContext(ctx, casStmt, j.Q.LastUpdate, j.Q.State, j.Q.ID, QueueStatePending)
		if err != nil {
			return database.CantPerformQuery(err, casStmt)
		}
		if affected, err := res.RowsAffected(); err != nil {
			return database.CantPerformQuery(err, casStmt)
		} else if affected == 0 {
			return errClaimLost // Lost the race for this exact entry to a concurrent node.
		}
		return nil
	})
}

// processAndFinalize processes the claimed job queue entry and finalizes it by updating its state and unlocking the object.
//
// It invokes the provided callback to process the event, and based on the result, it updates the queue entry's state
// to either QueueStateDone or QueueStateError. Finally, it unlocks the object associated with the queue entry.
//
// The function uses a retry mechanism with backoff to ensure that the finalization steps are completed successfully,
// thus preventing orphaned locks. The only case where the finalization steps are not guaranteed to complete is when
// the provided context is canceled, in which case the retention.ResetPruner will eventually unlock the object and
// reset the queue entry's state.
func processAndFinalize(
	ctx context.Context,
	db *database.DB,
	logger *logging.Logger,
	j job,
	cbs QueueCallbacks,
	updStmt,
	unlockStmt string,
) error {
	callbackCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()

	q, ev := j.Q, j.Ev
	switch q.Envelope.Format {
	case EnvelopeFmtEvent:
		if err := cbs.ProcessEvent(callbackCtx, ev); err != nil {
			logger.Errorw("Failed to process event", zap.Stringer("id", q.ID), zap.Error(err))
			q.State = QueueStateError
		} else {
			logger.Debugw("Successfully processed event", zap.Stringer("id", q.ID))
			q.State = QueueStateDone
		}

	default:
		return fmt.Errorf("unknown envelope format: %q", q.Envelope.Format)
	}

	settings := db.GetDefaultRetrySettings()
	// The finalize transaction must not have a timeout, because it must always run to completion
	// to unlock the object, unless the context is canceled.
	settings.Timeout = 0

	return retry.WithBackoff(
		ctx,
		func(ctx context.Context) error {
			return db.ExecTx(ctx, nil, func(ctx context.Context, tx *sqlx.Tx) error {
				q.LastUpdate = types.UnixMilli(time.Now())
				if _, err := tx.NamedExecContext(ctx, updStmt, q); err != nil {
					return database.CantPerformQuery(err, updStmt)
				}
				if _, err := tx.NamedExecContext(ctx, unlockStmt, q); err != nil {
					return database.CantPerformQuery(err, unlockStmt)
				}
				return nil
			})
		},
		retry.Retryable,
		backoff.DefaultBackoff,
		settings)
}
