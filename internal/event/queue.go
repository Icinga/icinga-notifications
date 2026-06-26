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

// TODO: Garbage collect event_queue entries:
//
// - Remove queueStateDone after a few minutes to get rid of potential duplicates.
// - Change queueStateProcessing after a few minutes to queueStateError because of a crash.
// - Consider cleaning up queueStateError after a few minutes/hours.
//
// Yonas' PR can be use: https://github.com/Icinga/icinga-notifications/pull/446

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
	// ID is the SHA256 hash based on the event.Event JSON serialization and the Source ID.
	ID types.Binary `db:"id"`

	// Json, Time, and SourceId are derived from event.Event. With Json being the JSON struct serialization,
	// both Time and SourceId are mirrored as they are excluded from JSON serialization.
	Json     string          `db:"json"`
	Time     types.UnixMilli `db:"time"`
	SourceId int64           `db:"source_id"`

	// ObjectId is the Object ID of the event, used to exclude related queue entries on the database level.
	ObjectId types.Binary `db:"object_id"`

	// Version describes the submitting client; allows migrations after upgrades.
	Version string `db:"version"`

	// State of the event. One of queueStatePending, queueStateProcessing, or queueStateDone.
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
	// version field and apply necessary migration steps.

	err := json.NewDecoder(strings.NewReader(q.Json)).Decode(&ev)
	if err != nil {
		return nil, fmt.Errorf("cannot JSON decode event queue entry: %w", err)
	}

	ev.Time = q.Time.Time()
	ev.SourceId = q.SourceId

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
		ID:       idHash.Sum(nil),
		Json:     jsonBuff.String(),
		Time:     types.UnixMilli(ev.Time),
		SourceId: ev.SourceId,
		ObjectId: objectID,
		Version:  internal.Version.Version,
		State:    queueStatePending,
	}

	// The primary key, ID, is a unique hash based on the event's JSON representation. Thus, multiple submissions can
	// get ignored. A UPSERT query would be invalid, as it resets the "processed" column.
	stmt, _ := db.BuildInsertIgnoreStmt(q)
	stmt = db.Rebind(stmt)

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
}

// ListenQueue fetches events from the database event queue and forwards them to the callback.
//
// This function blocks until either an error occurred or the passed context is finished. In either case, an error is
// being returned.
func ListenQueue(
	ctx context.Context,
	db *database.DB,
	logs *logging.Logging,
	callback func(context.Context, *logging.Logger, *Event) error,
) error {
	logger := logs.GetChildLogger("event-queue")

	selStmtCols := db.BuildColumns(new(Queue))
	for i := range selStmtCols {
		selStmtCols[i] = `eq."` + selStmtCols[i] + `"`
	}
	selStmt := db.Rebind(
		`SELECT ` + strings.Join(selStmtCols, ", ") + `
		FROM event_queue eq
		WHERE
			eq.state = ` + fmt.Sprintf("%d", queueStatePending) + ` AND
			NOT EXISTS (
				SELECT 1
				FROM event_queue sub
				WHERE
					sub.state = ` + fmt.Sprintf("%d", queueStateProcessing) + ` AND
					sub.object_id = eq.object_id)
		ORDER BY eq.time LIMIT 1`)
	updStmt, _ := db.BuildUpdateStmt(new(Queue))

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
		} else if err = callback(ctx, logger, ev); err != nil {
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
