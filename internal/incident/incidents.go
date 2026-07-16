package incident

import (
	"context"
	stderrors "errors"
	"fmt"
	"time"

	"github.com/icinga/icinga-go-library/database"
	"github.com/icinga/icinga-go-library/logging"
	baseEv "github.com/icinga/icinga-go-library/notifications/event"
	"github.com/icinga/icinga-go-library/types"
	"github.com/icinga/icinga-notifications/internal/config"
	"github.com/icinga/icinga-notifications/internal/event"
	"github.com/icinga/icinga-notifications/internal/object"
	"github.com/jmoiron/sqlx"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// ReevaluateEscalations retriggers escalations for every incident where Incident.NextEscalationCheckAt is due.
func ReevaluateEscalations(
	ctx context.Context,
	db *database.DB,
	logger *logging.Logger,
	runtimeConfig *config.RuntimeConfig,
) error {
	query := `
		SELECT "id", "object_id" FROM "incident"
		WHERE "recovered_at" IS NULL AND "next_escalation_check_at" IS NOT NULL AND "next_escalation_check_at" <= ?
		ORDER BY "next_escalation_check_at"`

	pairCh, errCh := yield(ctx, db, logger, runtimeConfig, false, query, types.UnixMilli(time.Now()))
	var errs []error
	for pair := range pairCh {
		err := pair.Incident.RetriggerEscalations(ctx, pair.Object, &event.Event{
			Time:  time.Now(),
			Event: baseEv.Event{Incident: types.MakeBool(true)},
		})
		if err != nil {
			errs = append(errs, fmt.Errorf("cannot reevaluate incident %s escalations: %w", pair.Incident, err))
		}
	}
	if err := <-errCh; err != nil {
		return err // we don't care about the other errors if yield failed.
	}
	return stderrors.Join(errs...)
}

// ErrOpenIncidentWithoutSeverity is returned when an event tries to open a new incident without a severity.
var ErrOpenIncidentWithoutSeverity = errors.New("cannot open or escalate an incident without a severity")

// ProcessEvent from an event.Event.
//
// It might return [ErrOpenIncidentWithoutSeverity] if the event is trying to open an incident without a severity or
// [ErrSeverityChangeWithoutIncidentFlag] if the event is trying to change the severity of an incident without the
// incident flag set. In both cases, the listener should map these errors to a 400 Bad Request response to the source.
func ProcessEvent(ctx context.Context, db *database.DB, l *logging.Logging, rc *config.RuntimeConfig, ev *event.Event) error {
	i := new(Incident)
	i.initializeFields(db, rc, l.GetChildLogger("incident").SugaredLogger)
	return i.ProcessEvent(ctx, ev)
}

// Pair is a struct that holds an incident and its related object used for yielding incidents and objects together.
type Pair struct {
	Incident *Incident
	Object   *object.Object
}

// Yield returns a channel of [Pair] for all active incidents (see yield docstring).
func Yield(ctx context.Context, db *database.DB, l *logging.Logging, rc *config.RuntimeConfig) (<-chan Pair, <-chan error) {
	logger := l.GetChildLogger("incident")
	return yield(ctx, db, logger, rc, true, `SELECT * FROM "incident" WHERE "recovered_at" IS NULL`)
}

// YieldForSource returns a channel of [Pair] for all active incidents of the given source (see yield docstring).
func YieldForSource(
	ctx context.Context,
	db *database.DB,
	l *logging.Logging,
	rc *config.RuntimeConfig,
	srcID int64,
) (<-chan Pair, <-chan error) {
	query := `
		SELECT "incident".* FROM "incident"
		JOIN "object" ON "incident"."object_id" = "object"."id"
		WHERE "incident"."recovered_at" IS NULL AND "object"."source_id" = ?`
	return yield(ctx, db, l.GetChildLogger("incident"), rc, false, query, srcID)
}

// yield is a helper function that runs the given query in a separate goroutine and sends each incident-object
// pair to the returned channel.
//
// The query must not be a named query, and must ensure to provide the correct arguments (if any) for the query.
// If the restoreAll flag is set to true, the related state of each incident will be restored from the database
// before sending it to the channel.
func yield(
	ctx context.Context,
	db *database.DB,
	logger *logging.Logger,
	rc *config.RuntimeConfig,
	restoreAll bool,
	query string,
	args ...any,
) (<-chan Pair, <-chan error) {
	pairCh := make(chan Pair)
	errCh := make(chan error, 1) // buffered to avoid goroutine leak if the receiver is not ready to receive errors.

	go func() {
		defer close(pairCh)
		defer close(errCh)

		rows, err := db.QueryxContext(ctx, db.Rebind(query), args...)
		if err != nil {
			errCh <- fmt.Errorf("cannot query incidents: %w", err)
			return
		}
		defer func() { _ = rows.Close() }()

		for rows.Next() {
			i := new(Incident)
			if err := rows.StructScan(i); err != nil {
				errCh <- fmt.Errorf("cannot scan incident from row: %w", err)
				return
			}

			obj, err := object.Get(ctx, db, i.ObjectID)
			if err != nil {
				errCh <- fmt.Errorf("cannot load object for incident %s: %w", i, err)
				return
			}
			i.initializeFields(db, rc, logger.With(zap.String("incident", i.String()), zap.String("object", obj.DisplayName())))
			if restoreAll {
				if err := db.ExecTx(ctx, nil, func(ctx context.Context, tx *sqlx.Tx) error {
					return i.restoreRelatedState(ctx, tx)
				}); err != nil {
					errCh <- fmt.Errorf("cannot restore related state for incident %s: %w", i, err)
					return
				}
			}

			select {
			case pairCh <- Pair{i, obj}:
			case <-ctx.Done():
				errCh <- ctx.Err()
				return
			}
		}

		if err := rows.Err(); err != nil {
			errCh <- fmt.Errorf("incidents cursor error: %w", err)
		}
	}()

	return pairCh, errCh
}
