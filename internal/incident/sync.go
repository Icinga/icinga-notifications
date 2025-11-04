package incident

import (
	"context"
	"fmt"
	"github.com/icinga/icinga-go-library/database"
	baseEv "github.com/icinga/icinga-go-library/notifications/event"
	"github.com/icinga/icinga-go-library/types"
	"github.com/icinga/icinga-notifications/internal/event"
	"github.com/icinga/icinga-notifications/internal/recipient"
	"github.com/icinga/icinga-notifications/internal/rule"
	"github.com/jmoiron/sqlx"
	"go.uber.org/zap"
	"time"
)

// Upsert implements the contracts.Upserter interface.
func (i *Incident) Upsert() interface{} {
	return &struct {
		Severity    baseEv.Severity `db:"severity"`
		RecoveredAt types.UnixMilli `db:"recovered_at"`
	}{Severity: i.Severity, RecoveredAt: i.RecoveredAt}
}

// Sync initiates an *incident.IncidentRow from the current incident state and syncs it with the database.
// Before syncing any incident related database entries, this method should be called at least once.
// Returns an error on db failure.
func (i *Incident) Sync(ctx context.Context, tx *sqlx.Tx) error {
	if i.Id != 0 {
		stmt, _ := i.DB.BuildUpsertStmt(i)
		_, err := tx.NamedExecContext(ctx, stmt, i)
		if err != nil {
			return fmt.Errorf("failed to upsert incident: %w", err)
		}
	} else {
		stmt := database.BuildInsertStmtWithout(i.DB, i, "id")
		incidentId, err := database.InsertObtainID(ctx, tx, stmt, i)
		if err != nil {
			return err
		}

		i.Id = incidentId
	}

	return nil
}

func (i *Incident) AddEscalationTriggered(ctx context.Context, tx *sqlx.Tx, state *EscalationState) error {
	state.IncidentID = i.Id

	stmt, _ := i.DB.BuildUpsertStmt(state)
	_, err := tx.NamedExecContext(ctx, stmt, state)

	return err
}

// AddEvent Inserts incident history record to the database and returns an error on db failure.
func (i *Incident) AddEvent(ctx context.Context, tx *sqlx.Tx, ev *event.Event) error {
	ie := &EventRow{IncidentID: i.Id, EventID: ev.ID}
	stmt, _ := i.DB.BuildInsertStmt(ie)
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
		cr := &ContactRow{IncidentID: i.Id, Role: newRole}

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
					IncidentID:       i.Id,
					EventID:          types.MakeInt(eventId, types.TransformZeroIntToNull),
					Key:              cr.Key,
					Time:             types.UnixMilli(time.Now()),
					Type:             RecipientRoleChanged,
					NewRecipientRole: newRole,
					OldRecipientRole: oldRole,
				}

				if err := hr.Sync(ctx, i.DB, tx); err != nil {
					i.logger.Errorw(
						"Failed to insert recipient role changed incident history", zap.Object("escalation", escalation),
						zap.String("recipients", r.String()), zap.Error(err),
					)
					return err
				}
			}
			cr.Role = state.Role
		}

		stmt, _ := i.DB.BuildUpsertStmt(cr)
		_, err := tx.NamedExecContext(ctx, stmt, cr)
		if err != nil {
			i.logger.Errorw(
				"Failed to upsert incident recipient", zap.Object("escalation", escalation),
				zap.String("recipient", r.String()), zap.Error(err),
			)
			return err
		}
	}

	return nil
}

// AddRuleMatched syncs the given *rule.Rule to the database.
// Returns an error on database failure.
func (i *Incident) AddRuleMatched(ctx context.Context, tx *sqlx.Tx, r *rule.Rule) error {
	rr := &RuleRow{IncidentID: i.Id, RuleID: r.ID}
	stmt, _ := i.DB.BuildUpsertStmt(rr)
	_, err := tx.NamedExecContext(ctx, stmt, rr)

	return err
}

// generateNotifications generates incident notification histories of the given recipients.
//
// This function will just insert NotificationStateSuppressed incident histories and return an empty slice if
// the current Object is muted, otherwise a slice of pending *NotificationEntry(ies) that can be used to update
// the corresponding histories after the actual notifications have been sent out.
func (i *Incident) generateNotifications(
	ctx context.Context, tx *sqlx.Tx, ev *event.Event, contactChannels rule.ContactChannels,
) ([]*NotificationEntry, error) {
	var notifications []*NotificationEntry
	suppress := i.isMuted && i.Object.IsMuted()
	for contact, channels := range contactChannels {
		for chID := range channels {
			hr := &HistoryRow{
				IncidentID:        i.Id,
				Key:               recipient.ToKey(contact),
				EventID:           types.MakeInt(ev.ID, types.TransformZeroIntToNull),
				Time:              types.UnixMilli(time.Now()),
				Type:              Notified,
				ChannelID:         types.MakeInt(chID, types.TransformZeroIntToNull),
				NotificationState: NotificationStatePending,
				Message:           types.MakeString(ev.Message, types.TransformEmptyStringToNull),
			}
			if suppress {
				hr.NotificationState = NotificationStateSuppressed
			}

			if err := hr.Sync(ctx, i.DB, tx); err != nil {
				i.logger.Errorw("Failed to insert incident notification history",
					zap.String("contact", contact.FullName),
					zap.Bool("incident_muted", i.isMuted),
					zap.Bool("object_muted", i.Object.IsMuted()),
					zap.Error(err))
				return nil, err
			}

			if !suppress {
				notifications = append(notifications, &NotificationEntry{
					HistoryRowID: hr.ID,
					ContactID:    contact.ID,
					State:        NotificationStatePending,
					ChannelID:    chID,
				})
			}
		}
	}

	return notifications, nil
}
