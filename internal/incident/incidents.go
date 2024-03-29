package incident

import (
	"context"
	"database/sql"
	"errors"
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
