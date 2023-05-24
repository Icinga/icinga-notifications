package incident

import (
	"database/sql"
	"fmt"
	"github.com/icinga/icinga-notifications/internal/config"
	"github.com/icinga/icinga-notifications/internal/event"
	"github.com/icinga/icinga-notifications/internal/object"
	"github.com/icinga/icinga-notifications/internal/recipient"
	"github.com/icinga/icingadb/pkg/icingadb"
	"github.com/icinga/icingadb/pkg/logging"
	"sync"
)

var (
	currentIncidents   = make(map[*object.Object]*Incident)
	currentIncidentsMu sync.Mutex
)

func GetCurrent(
	db *icingadb.DB, obj *object.Object, logger *logging.Logger, runtimeConfig *config.RuntimeConfig, configFile *config.ConfigFile, create bool,
) (*Incident, bool, error) {
	currentIncidentsMu.Lock()
	defer currentIncidentsMu.Unlock()

	created := false
	currentIncident := currentIncidents[obj]

	if currentIncident == nil {
		ir := &IncidentRow{}
		incident := &Incident{Object: obj, db: db, logger: logger, runtimeConfig: runtimeConfig, configFile: configFile}
		incident.SeverityBySource = make(map[int64]event.Severity)
		incident.EscalationState = make(map[escalationID]*EscalationState)
		incident.Recipients = make(map[recipient.Key]*RecipientState)

		err := db.QueryRowx(db.Rebind(db.BuildSelectStmt(ir, ir)+` WHERE "object_id" = ? AND "recovered_at" IS NULL`), obj.ID).StructScan(ir)
		if err != nil && err != sql.ErrNoRows {
			return nil, false, fmt.Errorf("incident query failed with: %w", err)
		} else if err == nil {
			incident.incidentRowID = ir.ID
			incident.StartedAt = ir.StartedAt.Time()

			sourceSeverity := &SourceSeverity{IncidentID: ir.ID}
			var sources []SourceSeverity
			err := db.Select(
				&sources,
				db.Rebind(db.BuildSelectStmt(sourceSeverity, sourceSeverity)+` WHERE "incident_id" = ? AND "severity" != ?`),
				ir.ID, event.SeverityOK,
			)
			if err != nil {
				return nil, false, fmt.Errorf("failed to fetch incident sources Severity: %w", err)
			}

			for _, source := range sources {
				incident.SeverityBySource[source.SourceID] = source.Severity
			}

			state := &EscalationState{}
			var states []*EscalationState
			err = db.Select(&states, db.Rebind(db.BuildSelectStmt(state, state)+` WHERE "incident_id" = ?`), ir.ID)
			if err != nil {
				return nil, false, fmt.Errorf("failed to fetch incident rule escalation state: %w", err)
			}

			for _, state := range states {
				incident.EscalationState[state.RuleEscalationID] = state
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

		contact := &ContactRow{}
		var contacts []*ContactRow
		err := db.Select(&contacts, db.Rebind(db.BuildSelectStmt(contact, contact)+` WHERE "incident_id" = ?`), currentIncident.ID())
		if err != nil {
			return nil, false, fmt.Errorf("failed to fetch incident recipients: %w", err)
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
