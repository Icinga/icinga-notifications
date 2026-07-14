package incident

import (
	"context"
	"database/sql"
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
	"golang.org/x/sync/errgroup"
)

// GetActiveIncidents returns a map of all active incidents for debugging purposes.
//
// The returned incidents carry their full related state (escalation states, matched rules, recipients)
// restored from the database.
func GetActiveIncidents(
	ctx context.Context, db *database.DB, logger *logging.Logger, runtimeConfig *config.RuntimeConfig,
) (map[int64]*Incident, error) {
	var is []*Incident
	err := db.SelectContext(ctx, &is, db.BuildSelectStmt(new(Incident), new(Incident))+` WHERE "recovered_at" IS NULL`)
	if err != nil {
		return nil, fmt.Errorf("cannot select all active incidents: %w", err)
	}

	m := make(map[int64]*Incident, len(is))
	for _, i := range is {
		i.initializeFields(db, runtimeConfig, logger.With(zap.String("incident", i.String())))
		err := db.ExecTx(ctx, &sql.TxOptions{}, func(ctx context.Context, tx *sqlx.Tx) error {
			return i.restoreRelatedState(ctx, tx)
		})
		if err != nil {
			return nil, err
		}
		m[i.Id] = i
	}
	return m, nil
}

// GetActiveIncidentsForSource returns a slice containing all currently open incidents belonging to a source.
func GetActiveIncidentsForSource(
	ctx context.Context, db *database.DB, logger *logging.Logger, runtimeConfig *config.RuntimeConfig, sourceID int64,
) ([]*Incident, error) {
	var is []*Incident
	err := db.SelectContext(
		ctx,
		&is,
		db.Rebind(`
			SELECT "incident".* FROM "incident"
			JOIN "object" ON "incident"."object_id" = "object"."id"
			WHERE "incident"."recovered_at" IS NULL AND "object"."source_id" = ?`),
		sourceID)
	if err != nil {
		return nil, fmt.Errorf("cannot select all active incidents: %w", err)
	}

	for _, i := range is {
		i.initializeFields(db, runtimeConfig, logger.With(zap.String("incident", i.String())))
	}
	return is, nil
}

// ReevaluateEscalations retriggers escalations for every incident where Incident.NextEscalationCheckAt is due.
func ReevaluateEscalations(
	ctx context.Context,
	db *database.DB,
	logger *logging.Logger,
	runtimeConfig *config.RuntimeConfig,
) error {
	eg, ctx := errgroup.WithContext(ctx)
	ch := make(chan *Incident, 1)

	eg.Go(func() error {
		defer close(ch)

		stmt := db.Rebind(`
			SELECT "id", "object_id" FROM "incident"
			WHERE "recovered_at" IS NULL AND "next_escalation_check_at" IS NOT NULL AND "next_escalation_check_at" <= ?
			ORDER BY "next_escalation_check_at"`)
		rows, err := db.QueryxContext(ctx, stmt, types.UnixMilli(time.Now()))
		if err != nil {
			return fmt.Errorf("cannot query incidents due for escalation reevaluation: %w", err)
		}
		defer func() { _ = rows.Close() }()

		for rows.Next() {
			i := NewIncident(db, nil, runtimeConfig, logger.SugaredLogger)
			if err := rows.StructScan(i); err != nil {
				return fmt.Errorf("cannot scan incident from row: %w", err)
			}

			i.logger = logger.With(zap.String("incident", i.String()))
			select {
			case <-ctx.Done():
				return ctx.Err()
			case ch <- i:
			}
		}

		return rows.Err()
	})

	eg.Go(func() error {
		var errs []error
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()

			case i, ok := <-ch:
				if !ok {
					return stderrors.Join(errs...)
				}

				err := i.RetriggerEscalations(ctx, &event.Event{
					Time:  time.Now(),
					Event: baseEv.Event{Incident: types.MakeBool(true)},
				})
				if err != nil {
					errs = append(errs, fmt.Errorf("cannot reevaluate incident %s escalations: %w", i, err))
				}
			}
		}
	})

	return eg.Wait()
}

// ErrOpenIncidentWithoutSeverity is returned when an event tries to open a new incident without a severity.
var ErrOpenIncidentWithoutSeverity = errors.New("cannot open or escalate an incident without a severity")

// ProcessEvent from an event.Event.
//
// It might return [ErrOpenIncidentWithoutSeverity] if the event is trying to open an incident without a severity or
// [ErrSeverityChangeWithoutIncidentFlag] if the event is trying to change the severity of an incident without the
// incident flag set. In both cases, the listener should map these errors to a 400 Bad Request response to the source.
func ProcessEvent(
	ctx context.Context,
	db *database.DB,
	logs *logging.Logging,
	runtimeConfig *config.RuntimeConfig,
	ev *event.Event,
) error {
	o := object.New(ev)
	i := NewIncident(db, o, runtimeConfig, logs.GetChildLogger("incident").With(zap.String("object", o.DisplayName())))
	return i.ProcessEvent(ctx, ev)
}
