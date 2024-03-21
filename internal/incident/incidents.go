package incident

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/icinga/icinga-notifications/internal/config"
	"github.com/icinga/icinga-notifications/internal/event"
	"github.com/icinga/icinga-notifications/internal/object"
	"github.com/icinga/icingadb/pkg/icingadb"
	"github.com/icinga/icingadb/pkg/logging"
	"github.com/icinga/icingadb/pkg/types"
	"go.uber.org/zap"
	"sync"
	"time"
)

// ErrSuperfluousStateChange indicates a superfluous state change being ignored and stopping further processing.
var ErrSuperfluousStateChange = errors.New("ignoring superfluous state change")

var (
	currentIncidents   = make(map[*object.Object]*Incident)
	currentIncidentsMu sync.Mutex
)

// LoadOpenIncidents loads all active (not yet closed) incidents from the database and restores all their states.
// Returns error ony database failure.
func LoadOpenIncidents(ctx context.Context, db *icingadb.DB, logger *logging.Logger, runtimeConfig *config.RuntimeConfig) error {
	logger.Info("Loading all active incidents from database")

	var objectIDs []types.Binary
	err := db.SelectContext(ctx, &objectIDs, `SELECT object_id FROM incident WHERE "recovered_at" IS NULL`)
	if err != nil {
		logger.Errorw("Failed to load active incidents from database", zap.Error(err))

		return errors.New("failed to fetch open incidents")
	}

	for _, objectID := range objectIDs {
		obj, err := object.LoadFromDB(ctx, db, objectID)
		if err != nil {
			logger.Errorw("Failed to retrieve incident object from database", zap.Error(err))
			continue
		}

		incident, _, err := GetCurrent(ctx, db, obj, logger, runtimeConfig, false)
		if err != nil {
			continue
		}

		incident.RetriggerEscalations(&event.Event{
			Time:    time.Now(),
			Type:    event.TypeInternal,
			Message: "Incident reevaluation at daemon startup",
		})
	}

	return nil
}

func GetCurrent(
	ctx context.Context, db *icingadb.DB, obj *object.Object, logger *logging.Logger, runtimeConfig *config.RuntimeConfig,
	create bool,
) (*Incident, bool, error) {
	currentIncidentsMu.Lock()
	defer currentIncidentsMu.Unlock()

	created := false
	currentIncident := currentIncidents[obj]

	if currentIncident == nil {
		ir := &IncidentRow{}
		incidentLogger := logger.With(zap.String("object", obj.DisplayName()))
		incident := NewIncident(db, obj, runtimeConfig, incidentLogger)

		err := db.QueryRowxContext(ctx, db.Rebind(db.BuildSelectStmt(ir, ir)+` WHERE "object_id" = ? AND "recovered_at" IS NULL`), obj.ID).StructScan(ir)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			logger.Errorw("Failed to load incident from database", zap.String("object", obj.DisplayName()), zap.Error(err))

			return nil, false, errors.New("failed to load incident from database")
		} else if err == nil {
			incident.incidentRowID = ir.ID
			incident.StartedAt = ir.StartedAt.Time()
			incident.Severity = ir.Severity
			incident.logger = logger.With(zap.String("object", obj.DisplayName()), zap.String("incident", incident.String()))

			if err := incident.restoreEscalationsState(ctx); err != nil {
				return nil, false, err
			}

			currentIncident = incident
		}

		if create && currentIncident == nil {
			created = true
			currentIncident = incident
		}

		if currentIncident != nil {
			currentIncidents[obj] = currentIncident
		}
	}

	if !created && currentIncident != nil {
		currentIncident.Lock()
		defer currentIncident.Unlock()

		if err := currentIncident.restoreRecipients(ctx); err != nil {
			return nil, false, err
		}
	}

	return currentIncident, created, nil
}

func RemoveCurrent(obj *object.Object) {
	currentIncidentsMu.Lock()
	defer currentIncidentsMu.Unlock()

	currentIncident := currentIncidents[obj]

	if currentIncident != nil {
		delete(currentIncidents, obj)
	}
}

// GetCurrentIncidents returns a map of all incidents for debugging purposes.
func GetCurrentIncidents() map[int64]*Incident {
	currentIncidentsMu.Lock()
	defer currentIncidentsMu.Unlock()

	m := make(map[int64]*Incident)
	for _, incident := range currentIncidents {
		m[incident.incidentRowID] = incident
	}
	return m
}

// ProcessEvent from an event.Event.
//
// This function first gets this Event's object.Object and its incident.Incident. Then, after performing some safety
// checks, it calls the Incident.ProcessEvent method.
//
// The returned error might be wrapped around ErrSuperfluousStateChange.
func ProcessEvent(
	ctx context.Context,
	db *icingadb.DB,
	logs *logging.Logging,
	runtimeConfig *config.RuntimeConfig,
	ev *event.Event,
) error {
	obj, err := object.FromEvent(ctx, db, ev)
	if err != nil {
		return fmt.Errorf("cannot sync event object: %w", err)
	}

	createIncident := ev.Severity != event.SeverityNone && ev.Severity != event.SeverityOK
	currentIncident, created, err := GetCurrent(
		ctx,
		db,
		obj,
		logs.GetChildLogger("incident"),
		runtimeConfig,
		createIncident)
	if err != nil {
		return fmt.Errorf("cannot get current incident for %q: %w", obj.DisplayName(), err)
	}

	if currentIncident == nil {
		switch {
		case ev.Type == event.TypeAcknowledgement:
			return fmt.Errorf("%q does not have an active incident, ignoring acknowledgement event from source %d",
				obj.DisplayName(), ev.SourceId)
		case ev.Severity != event.SeverityOK:
			panic(fmt.Sprintf("cannot process event %v with a non-OK state %v without a known incident", ev, ev.Severity))
		default:
			return fmt.Errorf("%w: ok state event from source %d", ErrSuperfluousStateChange, ev.SourceId)
		}
	}

	return currentIncident.ProcessEvent(ctx, ev, created)
}
