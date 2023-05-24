package incident

import (
	"fmt"
	"github.com/icinga/icinga-notifications/internal/event"
	"github.com/icinga/icinga-notifications/internal/recipient"
	"github.com/icinga/icinga-notifications/internal/rule"
	"github.com/icinga/icinga-notifications/internal/utils"
	"github.com/icinga/icingadb/pkg/types"
	"time"
)

// Sync initiates an *incident.IncidentRow from the current incident state and syncs it with the database.
// Before syncing any incident related database entries, this method should be called at least once.
// Returns an error on db failure.
func (i *Incident) Sync() error {
	incidentRow := &IncidentRow{
		ID:          i.incidentRowID,
		ObjectID:    i.Object.ID,
		StartedAt:   types.UnixMilli(i.StartedAt),
		RecoveredAt: types.UnixMilli(i.RecoveredAt),
		Severity:    i.Severity(),
	}

	err := incidentRow.Sync(i.db, i.incidentRowID != 0)
	if err != nil {
		return err
	}

	i.incidentRowID = incidentRow.ID

	return nil
}

func (i *Incident) AddHistory(historyRow *HistoryRow, fetchId bool) (types.Int, error) {
	historyRow.IncidentID = i.incidentRowID

	stmt := utils.BuildInsertStmtWithout(i.db, historyRow, "id")
	if fetchId {
		historyId, err := utils.InsertAndFetchId(i.db, stmt, historyRow)
		if err != nil {
			return types.Int{}, err
		}

		return utils.ToDBInt(historyId), nil
	} else {
		_, err := i.db.NamedExec(stmt, historyRow)
		if err != nil {
			return types.Int{}, err
		}
	}

	return types.Int{}, nil
}

func (i *Incident) AddEscalationTriggered(state *EscalationState, hr *HistoryRow) (types.Int, error) {
	state.IncidentID = i.incidentRowID

	stmt, _ := i.db.BuildUpsertStmt(state)
	_, err := i.db.NamedExec(stmt, state)
	if err != nil {
		return types.Int{}, err
	}

	return i.AddHistory(hr, true)
}

// AddEvent Inserts incident history record to the database and returns an error on db failure.
func (i *Incident) AddEvent(ev *event.Event) error {
	ie := &EventRow{IncidentID: i.incidentRowID, EventID: ev.ID}
	stmt, _ := i.db.BuildInsertStmt(ie)
	_, err := i.db.NamedExec(stmt, ie)

	return err
}

// AddRecipient adds recipient from the given *rule.Escalation to this incident.
// Syncs also all the recipients with the database and returns an error on db failure.
func (i *Incident) AddRecipient(escalation *rule.Escalation, eventId int64) error {
	newRole := RoleRecipient
	if i.HasManager() {
		newRole = RoleSubscriber
	}

	for _, escalationRecipient := range escalation.Recipients {
		r := escalationRecipient.Recipient
		cr := &ContactRow{IncidentID: i.incidentRowID, Role: newRole}

		recipientKey := recipient.ToKey(r)
		cr.Key = recipientKey

		state, ok := i.Recipients[recipientKey]
		if !ok {
			i.Recipients[recipientKey] = &RecipientState{Role: newRole}
		} else {
			if state.Role < newRole {
				oldRole := state.Role
				state.Role = newRole

				i.logger.Infof("Contact %q role changed from %s to %s", r, state.Role.String(), newRole.String())

				hr := &HistoryRow{
					IncidentID:       i.incidentRowID,
					EventID:          utils.ToDBInt(eventId),
					Key:              cr.Key,
					Time:             types.UnixMilli(time.Now()),
					Type:             RecipientRoleChanged,
					NewRecipientRole: newRole,
					OldRecipientRole: oldRole,
				}

				_, err := i.AddHistory(hr, false)
				if err != nil {
					return err
				}
			}
			cr.Role = state.Role
		}

		stmt, _ := i.db.BuildUpsertStmt(cr)
		_, err := i.db.NamedExec(stmt, cr)
		if err != nil {
			return fmt.Errorf("failed to upsert incident contact %s: %w", r, err)
		}
	}

	return nil
}

// AddRuleMatchedHistory syncs the given *rule.Rule and history entry to the database.
// Returns an error on database failure.
func (i *Incident) AddRuleMatchedHistory(r *rule.Rule, hr *HistoryRow) (types.Int, error) {
	rr := &RuleRow{IncidentID: i.incidentRowID, RuleID: r.ID}
	stmt, _ := i.db.BuildUpsertStmt(rr)
	_, err := i.db.NamedExec(stmt, rr)
	if err != nil {
		return types.Int{}, err
	}

	return i.AddHistory(hr, true)
}

func (i *Incident) AddSourceSeverity(severity event.Severity, sourceID int64) error {
	i.SeverityBySource[sourceID] = severity

	sourceSeverity := &SourceSeverity{
		IncidentID: i.incidentRowID,
		SourceID:   sourceID,
		Severity:   severity,
	}

	stmt, _ := i.db.BuildUpsertStmt(sourceSeverity)
	_, err := i.db.NamedExec(stmt, sourceSeverity)

	return err
}
