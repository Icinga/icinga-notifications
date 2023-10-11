package incident

import (
	"context"
	"database/sql"
	"errors"
	"github.com/icinga/icinga-notifications/internal/config"
	"github.com/icinga/icinga-notifications/internal/object"
	"github.com/icinga/icinga-notifications/internal/recipient"
	"github.com/icinga/icingadb/pkg/icingadb"
	"github.com/icinga/icingadb/pkg/logging"
	"go.uber.org/zap"
	"sync"
)

var (
	currentIncidents   = make(map[*object.Object]*Incident)
	currentIncidentsMu sync.Mutex
)

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
		incident := &Incident{
			Object:          obj,
			db:              db,
			logger:          logger.With(zap.String("object", obj.DisplayName())),
			runtimeConfig:   runtimeConfig,
			Recipients:      map[recipient.Key]*RecipientState{},
			EscalationState: map[escalationID]*EscalationState{},
			Rules:           map[ruleID]struct{}{},
		}

		err := db.QueryRowxContext(ctx, db.Rebind(db.BuildSelectStmt(ir, ir)+` WHERE "object_id" = ? AND "recovered_at" IS NULL`), obj.ID).StructScan(ir)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			logger.Errorw("Failed to load incident from database", zap.String("object", obj.DisplayName()), zap.Error(err))

			return nil, false, errors.New("failed to load incident from database")
		} else if err == nil {
			incident.incidentRowID = ir.ID
			incident.StartedAt = ir.StartedAt.Time()
			incident.logger = logger.With(zap.String("object", obj.DisplayName()), zap.String("incident", incident.String()))

			state := &EscalationState{}
			var states []*EscalationState
			err = db.SelectContext(ctx, &states, db.Rebind(db.BuildSelectStmt(state, state)+` WHERE "incident_id" = ?`), ir.ID)
			if err != nil {
				incident.logger.Errorw("Failed to load incident rule escalation states", zap.Error(err))

				return nil, false, errors.New("failed to load incident rule escalation states")
			}

			for _, state := range states {
				incident.EscalationState[state.RuleEscalationID] = state
			}

			incident.RestoreEscalationStateRules(states)

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

		contact := &ContactRow{}
		var contacts []*ContactRow
		err := db.SelectContext(ctx, &contacts, db.Rebind(db.BuildSelectStmt(contact, contact)+` WHERE "incident_id" = ?`), currentIncident.ID())
		if err != nil {
			currentIncident.logger.Errorw("Failed to reload incident recipients", zap.Error(err))

			return nil, false, errors.New("failed to load incident recipients")
		}

		recipients := make(map[recipient.Key]*RecipientState)
		for _, contact := range contacts {
			recipients[contact.Key] = &RecipientState{Role: contact.Role}
		}

		currentIncident.Recipients = recipients
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
