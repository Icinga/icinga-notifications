package incident

import (
	"context"
	"fmt"
	"github.com/icinga/icinga-go-library/types"
	"github.com/icinga/icinga-notifications/internal/event"
	"github.com/icinga/icinga-notifications/internal/notification"
	"github.com/icinga/icinga-notifications/internal/recipient"
	"github.com/icinga/icinga-notifications/internal/rule"
	"github.com/icinga/icinga-notifications/internal/utils"
	"github.com/jmoiron/sqlx"
	"go.uber.org/zap"
	"time"
)

// Upsert implements the contracts.Upserter interface.
func (i *Incident) Upsert() interface{} {
	return &struct {
		Severity    event.Severity  `db:"severity"`
		RecoveredAt types.UnixMilli `db:"recovered_at"`
	}{Severity: i.Severity, RecoveredAt: i.RecoveredAt}
}

// Sync initiates an *incident.IncidentRow from the current incident state and syncs it with the database.
// Before syncing any incident related database entries, this method should be called at least once.
// Returns an error on db failure.
func (i *Incident) Sync(ctx context.Context, tx *sqlx.Tx) error {
	if i.ID != 0 {
		stmt, _ := i.DB.BuildUpsertStmt(i)
		_, err := tx.NamedExecContext(ctx, stmt, i)
		if err != nil {
			return fmt.Errorf("failed to upsert incident: %w", err)
		}
	} else {
		stmt := utils.BuildInsertStmtWithout(i.DB, i, "id")
		incidentId, err := utils.InsertAndFetchId(ctx, tx, stmt, i)
		if err != nil {
			return err
		}

		i.ID = incidentId
	}

	return nil
}

func (i *Incident) AddEscalationTriggered(ctx context.Context, tx *sqlx.Tx, state *EscalationState) error {
	state.IncidentID = i.ID

	stmt, _ := i.DB.BuildUpsertStmt(state)
	_, err := tx.NamedExecContext(ctx, stmt, state)

	return err
}

// AddRecipient adds recipient from the given *rule.Entry to this incident.
// Syncs also all the recipients with the database and returns an error on db failure.
func (i *Incident) AddRecipient(ctx context.Context, tx *sqlx.Tx, escalation *rule.Entry, eventId int64) error {
	newRole := RoleRecipient
	if i.HasManager() {
		newRole = RoleSubscriber
	}

	for _, escalationRecipient := range escalation.Recipients {
		r := escalationRecipient.Recipient
		cr := &ContactRow{IncidentID: i.ID, Role: newRole}

		recipientKey := recipient.ToKey(r)
		cr.Key = recipientKey

		state, ok := i.Recipients[recipientKey]
		if !ok {
			i.Recipients[recipientKey] = &RecipientState{Role: newRole}
		} else {
			if state.Role < newRole {
				oldRole := state.Role
				state.Role = newRole

				i.Logger.Infof("Contact %q role changed from %s to %s", r, state.Role.String(), newRole.String())

				hr := &HistoryRow{
					IncidentID:       i.ID,
					EventID:          utils.ToDBInt(eventId),
					Key:              cr.Key,
					Time:             types.UnixMilli(time.Now()),
					Type:             RecipientRoleChanged,
					NewRecipientRole: newRole,
					OldRecipientRole: oldRole,
				}

				if err := hr.Sync(ctx, i.DB, tx); err != nil {
					i.Logger.Errorw(
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
			i.Logger.Errorw(
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
	rr := &RuleRow{IncidentID: i.ID, RuleID: r.ID}
	stmt, _ := i.DB.BuildUpsertStmt(rr)
	_, err := tx.NamedExecContext(ctx, stmt, rr)

	return err
}

// GenerateNotifications generates incident notification histories of the given recipients.
//
// This function will just insert notification.StateSuppressed incident histories and return an empty slice if
// the current Object is muted, otherwise a slice of pending *NotificationEntry(ies) that can be used to update
// the corresponding histories after the actual notifications have been sent out.
func (i *Incident) GenerateNotifications(
	ctx context.Context, tx *sqlx.Tx, ev *event.Event, contactChannels rule.ContactChannels,
) (notification.PendingNotifications, error) {
	notifications, err := notification.AddNotifications(ctx, i.DB, tx, contactChannels, func(n *notification.History) {
		n.IncidentID = utils.ToDBInt(i.ID)
		n.Message = utils.ToDBString(ev.Message)
		if i.isMuted && i.Object.IsMuted() {
			n.NotificationState = notification.StateSuppressed
		}
	})
	if err != nil {
		i.Logger.Errorw("Failed to add pending notification histories", zap.Error(err))
		return nil, err
	}

	for contact, entries := range notifications {
		for _, entry := range entries {
			hr := &HistoryRow{
				IncidentID:            i.ID,
				Key:                   recipient.ToKey(contact),
				EventID:               utils.ToDBInt(ev.ID),
				Time:                  types.UnixMilli(time.Now()),
				Type:                  Notified,
				NotificationHistoryID: utils.ToDBInt(entry.HistoryRowID),
			}

			if err := hr.Sync(ctx, i.DB, tx); err != nil {
				i.Logger.Errorw("Failed to insert incident notification history",
					zap.String("contact", contact.FullName), zap.Bool("incident_muted", i.Object.IsMuted()),
					zap.Error(err))
				return nil, err
			}
		}
	}

	if i.isMuted && i.Object.IsMuted() {
		notifications = nil
	}

	return notifications, nil
}
