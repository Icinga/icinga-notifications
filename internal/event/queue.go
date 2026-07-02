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
	"strings"
	"time"

	"github.com/icinga/icinga-go-library/backoff"
	"github.com/icinga/icinga-go-library/database"
	"github.com/icinga/icinga-go-library/logging"
	"github.com/icinga/icinga-go-library/retry"
	"github.com/icinga/icinga-go-library/types"
	"github.com/icinga/icinga-notifications/internal"
	"github.com/jmoiron/sqlx"
	"go.uber.org/zap"
)

const (
	// The following consts are valid values for Queue.State.
	//
	// - queueStatePending is the initial state for submitting events.
	// - queueStateProcessing is assigned when the ListenQueue picked this up for processing.
	// - queueStateDone is set after ListenQueue successfully processed the entry.
	// - queueStateError is assigned to invalid events for further processing.
	//
	// queueStateDone ensures that a source cannot enqueue the same event after it was already processed. If the event
	// would have been already deleted from the database, but the source resubmits it due to some source-facing network
	// issues, it would have been evaluated once again. By keeping evaluated events with the queueStateDone state in
	// the database for a few minutes eliminates this potential issue.
	queueStatePending    int16 = 0
	queueStateProcessing int16 = 1
	queueStateDone       int16 = 2
	queueStateError      int16 = 64
)

// Queue describes an event.Event enqueued for processing in the relational database.
type Queue struct {
	// ID is the SHA256 hash based on the event.Event JSON representation.
	ID types.Binary `db:"id"`

	// Json serialization and Time from the event.Event. Time is required for database ordering.
	Json string          `db:"json"`
	Time types.UnixMilli `db:"time"`

	// ObjectId is the Object ID of the event, used to exclude related queue entries on the database level.
	ObjectId types.Binary `db:"object_id"`

	// UserAgent describes the submitting client; allows migrations after upgrades.
	UserAgent string `db:"user_agent"`

	// State of the event. Check "queueState*" consts above.
	State int16 `db:"state"`
}

// TableName implements [database.TableNamer].
func (q *Queue) TableName() string {
	return "event_queue"
}

// toEvent recreates an event.Event based on the JSON representation and the other fields stored in the database.
func (q *Queue) toEvent() (*Event, error) {
	var ev Event

	// NOTE: There might be breaking changes with future updates. If this happens, this function should honor the
	// UserAgent field and apply necessary migration steps.

	err := json.NewDecoder(strings.NewReader(q.Json)).Decode(&ev)
	if err != nil {
		return nil, fmt.Errorf("cannot JSON decode event queue entry: %w", err)
	}

	ev.Time = q.Time.Time()

	return &ev, nil
}

// Enqueue enqueues an event.Event into the event queue.
//
// The internally used database transaction is bound to the provided context. When calling this function from an HTTP
// handler, pass the request's context to terminate the transaction if the client disconnects prematurely.
func Enqueue(ctx context.Context, db *database.DB, ev *Event, objectID types.Binary) error {
	jsonBuff := bytes.Buffer{}
	idHash := sha256.New()

	err := json.NewEncoder(io.MultiWriter(&jsonBuff, idHash)).Encode(ev)
	if err != nil {
		return fmt.Errorf("cannot JSON encode event.Event: %w", err)
	}

	q := &Queue{
		ID:        idHash.Sum(nil),
		Json:      jsonBuff.String(),
		Time:      types.UnixMilli(ev.Time),
		ObjectId:  objectID,
		UserAgent: "icinga-notifications/" + internal.Version.Version,
		State:     queueStatePending,
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

// ProcessQueue fetches events from the database event queue and forwards them to the callback.
//
// This function blocks until either an error occurred or the passed context is finished. In either case, an error is
// being returned.
func ProcessQueue(
	ctx context.Context,
	db *database.DB,
	logger *logging.Logger,
	callback func(context.Context, *Event) error,
) error {
	selStmt := db.Rebind(`
		SELECT q1.*
		FROM event_queue q1
		LEFT JOIN event_queue q2 ON
			q2.state = ` + fmt.Sprintf("%d", queueStateProcessing) + ` AND
			q1.object_id = q2.object_id
		WHERE
			q1.state = ` + fmt.Sprintf("%d", queueStatePending) + ` AND
			q2.object_id IS NULL
		ORDER BY q1.time
		LIMIT 1`)
	updStmt := `UPDATE event_queue SET state = :state WHERE id = :id`

	for ctx.Err() == nil {
		var q Queue

		err := retry.WithBackoff(
			ctx,
			func(ctx context.Context) error {
				return db.ExecTx(
					ctx,
					&sql.TxOptions{Isolation: sql.LevelSerializable},
					func(ctx context.Context, tx *sqlx.Tx) error {
						err := tx.GetContext(ctx, &q, selStmt)
						if err != nil {
							return database.CantPerformQuery(err, selStmt)
						}

						q.State = queueStateProcessing
						_, err = tx.NamedExecContext(ctx, updStmt, q)
						if err != nil {
							return database.CantPerformQuery(err, updStmt)
						}

						return nil
					})
			},
			func(err error) bool {
				if errors.Is(err, sql.ErrNoRows) {
					return true
				}
				return retry.Retryable(err)
			},
			backoff.NewExponentialWithJitter(time.Second, 3*time.Second),
			retry.Settings{Timeout: retry.DefaultTimeout})
		if errors.Is(err, sql.ErrNoRows) {
			continue
		} else if err != nil {
			return fmt.Errorf("cannot fetch event from event queue: %w", err)
		}

		logger.Debugw("Claimed event queue entry for processing",
			zap.Stringer("id", q.ID),
			zap.Stringer("object_id", q.ObjectId))

		if ev, err := q.toEvent(); err != nil {
			logger.Errorw("Invalid event queue event cannot get decoded", zap.Stringer("id", q.ID), zap.Error(err))
			q.State = queueStateError
		} else if err = callback(ctx, ev); err != nil {
			logger.Errorw("Event queue event cannot be processed", zap.Stringer("id", q.ID), zap.Error(err))
			q.State = queueStateError
		} else {
			logger.Debugw("Processed event from event queue", zap.Stringer("id", q.ID))
			q.State = queueStateDone
		}

		err = retry.WithBackoff(
			ctx,
			func(ctx context.Context) error {
				return db.ExecTx(
					ctx,
					&sql.TxOptions{Isolation: sql.LevelSerializable},
					func(ctx context.Context, tx *sqlx.Tx) error {
						_, err = tx.NamedExecContext(ctx, updStmt, q)
						if err != nil {
							return database.CantPerformQuery(err, updStmt)
						}

						return nil
					})
			},
			retry.Retryable,
			backoff.DefaultBackoff,
			db.GetDefaultRetrySettings())
		if err != nil {
			return fmt.Errorf("cannot update queue entry %q after processing: %w", q.ID, err)
		}
	}

	return ctx.Err()
}
