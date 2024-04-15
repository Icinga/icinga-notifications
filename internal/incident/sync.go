package incident

import (
	"context"
	"errors"
	"github.com/icinga/icinga-notifications/internal/event"
	"github.com/icinga/icinga-notifications/internal/recipient"
	"github.com/icinga/icinga-notifications/internal/rule"
	"github.com/icinga/icinga-notifications/internal/utils"
	"github.com/icinga/icingadb/pkg/types"
	"github.com/jmoiron/sqlx"
	"go.uber.org/zap"
	"time"
)

// Sync initiates an *incident.IncidentRow from the current incident state and syncs it with the database.
// Before syncing any incident related database entries, this method should be called at least once.
// Returns an error on db failure.
func (i *Incident) Sync(ctx context.Context, tx *sqlx.Tx) error {
	incidentRow := &IncidentRow{
		ID:          i.incidentRowID,
		ObjectID:    i.Object.ID,
		StartedAt:   types.UnixMilli(i.StartedAt),
		RecoveredAt: types.UnixMilli(i.RecoveredAt),
		Severity:    i.Severity,
	}

	err := incidentRow.Sync(ctx, tx, i.db, i.incidentRowID != 0)
	if err != nil {
		return err
	}

	i.incidentRowID = incidentRow.ID

	return nil
}

func (i *Incident) AddHistory(ctx context.Context, tx *sqlx.Tx, historyRow *HistoryRow, fetchId bool) (types.Int, error) {
	historyRow.IncidentID = i.incidentRowID

	stmt := utils.BuildInsertStmtWithout(i.db, historyRow, "id")
	if fetchId {
		historyId, err := utils.InsertAndFetchId(ctx, tx, stmt, historyRow)
		if err != nil {
			return types.Int{}, err
		}

		return utils.ToDBInt(historyId), nil
	} else {
		_, err := tx.NamedExecContext(ctx, stmt, historyRow)
		if err != nil {
			return types.Int{}, err
		}
	}

	return types.Int{}, nil
}

func (i *Incident) AddEscalationTriggered(ctx context.Context, tx *sqlx.Tx, state *EscalationState) error {
	state.IncidentID = i.incidentRowID

	stmt, _ := i.db.BuildUpsertStmt(state)
	_, err := tx.NamedExecContext(ctx, stmt, state)

	return err
}

// AddEvent Inserts incident history record to the database and returns an error on db failure.
func (i *Incident) AddEvent(ctx context.Context, tx *sqlx.Tx, ev *event.Event) error {
	ie := &EventRow{IncidentID: i.incidentRowID, EventID: ev.ID}
	stmt, _ := i.db.BuildInsertStmt(ie)
	_, err := tx.NamedExecContext(ctx, stmt, ie)

	return err
}

// AddRecipient adds recipient from the given *rule.Escalation to this incident.
// Syncs also all the recipients with the database and returns an error on db failure.
func (i *Incident) AddRecipient(ctx context.Context, tx *sqlx.Tx, escalation *rule.Escalation, eventId int64) error {
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

				_, err := i.AddHistory(ctx, tx, hr, false)
				if err != nil {
					i.logger.Errorw(
						"Failed to insert recipient role changed incident history", zap.String("escalation", escalation.DisplayName()),
						zap.String("recipients", r.String()), zap.Error(err),
					)

					return errors.New("failed to insert recipient role changed incident history")
				}
			}
			cr.Role = state.Role
		}

		stmt, _ := i.db.BuildUpsertStmt(cr)
		_, err := tx.NamedExecContext(ctx, stmt, cr)
		if err != nil {
			i.logger.Errorw(
				"Failed to upsert incident recipient", zap.String("escalation", escalation.DisplayName()),
				zap.String("recipient", r.String()), zap.Error(err),
			)

			return errors.New("failed to upsert incident recipient")
		}
	}

	return nil
}

// AddRuleMatched syncs the given *rule.Rule to the database.
// Returns an error on database failure.
func (i *Incident) AddRuleMatched(ctx context.Context, tx *sqlx.Tx, r *rule.Rule) error {
	rr := &RuleRow{IncidentID: i.incidentRowID, RuleID: r.ID}
	stmt, _ := i.db.BuildUpsertStmt(rr)
	_, err := tx.NamedExecContext(ctx, stmt, rr)

	return err
}

// addPendingNotifications inserts pending notification incident history of the given recipients.
func (i *Incident) addPendingNotifications(
	ctx context.Context, tx *sqlx.Tx, ev *event.Event, contactChannels contactChannels,
) ([]*NotificationEntry, error) {
	var notifications []*NotificationEntry
	for contact, channels := range contactChannels {
		for chID := range channels {
			hr := &HistoryRow{
				Key:               recipient.ToKey(contact),
				EventID:           utils.ToDBInt(ev.ID),
				Time:              types.UnixMilli(time.Now()),
				Type:              Notified,
				ChannelID:         utils.ToDBInt(chID),
				NotificationState: NotificationStatePending,
			}

			id, err := i.AddHistory(ctx, tx, hr, true)
			if err != nil {
				i.logger.Errorw(
					"Failed to insert contact pending notification incident history",
					zap.String("contact", contact.String()), zap.Error(err),
				)

				return nil, errors.New("can't insert contact pending notification incident history")
			}

			hr.ID = id.Int64
			notifications = append(notifications, &NotificationEntry{
				HistoryRowID: hr.ID,
				ContactID:    contact.ID,
				State:        NotificationStatePending,
				ChannelID:    chID,
			})
		}
	}

	return notifications, nil
}
