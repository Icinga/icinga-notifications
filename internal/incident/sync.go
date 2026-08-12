package incident

import (
	"context"
	"fmt"
	"time"

	"github.com/icinga/icinga-go-library/database"
	baseEv "github.com/icinga/icinga-go-library/notifications/event"
	"github.com/icinga/icinga-go-library/types"
	"github.com/icinga/icinga-notifications/internal/event"
	"github.com/icinga/icinga-notifications/internal/recipient"
	"github.com/icinga/icinga-notifications/internal/rule"
	"github.com/jmoiron/sqlx"
	"go.uber.org/zap"
)

// Upsert implements the contracts.Upserter interface.
func (i *Incident) Upsert() any {
	return &struct {
		Severity              baseEv.Severity `db:"severity"`
		RecoveredAt           types.UnixMilli `db:"recovered_at"`
		MuteReason            types.String    `db:"mute_reason"`
		Message               types.String    `db:"message"`
		NextEscalationCheckAt types.UnixMilli `db:"next_escalation_check_at"`
	}{}
}

// Sync initiates an *incident.IncidentRow from the current incident state and syncs it with the database.
// Before syncing any incident related database entries, this method should be called at least once.
// Returns an error on db failure.
func (i *Incident) Sync(ctx context.Context, tx *sqlx.Tx) error {
	if i.Id != 0 {
		stmt, _ := i.db.BuildUpsertStmt(i)
		_, err := tx.NamedExecContext(ctx, stmt, i)
		if err != nil {
			return fmt.Errorf("failed to upsert incident: %w", err)
		}
	} else {
		stmt := database.BuildInsertStmtWithout(i.db, i, "id")
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

	stmt, _ := i.db.BuildUpsertStmt(state)
	_, err := tx.NamedExecContext(ctx, stmt, state)

	return err
}

// AddEscalationRecipients adds the recipients of the given *rule.Escalation to the incident's recipients list.
//
// Each recipient is added to the incident's recipients list with the role RoleRecipient, and a new ContactRow
// is inserted into the incident_contact table. If a recipient already exists in the incident's recipients list,
// it is skipped and no new ContactRow is inserted for that recipient.
func (i *Incident) AddEscalationRecipients(ctx context.Context, tx *sqlx.Tx, escalation *rule.Escalation) error {
	for _, escalationRecipient := range escalation.Recipients {
		r := escalationRecipient.Recipient
		recipientKey := recipient.ToKey(r)
		if _, exists := i.Recipients[recipientKey]; exists {
			continue
		}
		i.Recipients[recipientKey] = RecipientState{Role: RoleRecipient, IsNew: true}
		cr := &ContactRow{IncidentID: i.Id, Key: recipientKey, Role: RoleRecipient, ChangedAt: types.UnixMilli(time.Now())}

		stmt, _ := i.db.BuildUpsertStmt(cr, "id")
		_, err := tx.NamedExecContext(ctx, stmt, cr)
		if err != nil {
			i.logger.Errorw(
				"Failed to add escalation recipient to incident",
				zap.Object("escalation", escalation),
				zap.String("recipient", r.String()),
				zap.Error(err))
			return err
		}
	}

	return nil
}

// AddRuleMatched syncs the given *rule.Rule to the database.
// Returns an error on database failure.
func (i *Incident) AddRuleMatched(ctx context.Context, tx *sqlx.Tx, r *rule.Rule) error {
	rr := &RuleRow{IncidentID: i.Id, RuleID: r.ID}
	stmt, _ := i.db.BuildUpsertStmt(rr)
	_, err := tx.NamedExecContext(ctx, stmt, rr)

	return err
}

// generateNotifications generates incident notification histories of the given recipients.
//
// This function will just insert NotificationStateSuppressed incident histories and return an empty slice if
// the incident is muted, otherwise a slice of pending *NotificationEntry(ies) that can be used to update
// the corresponding histories after the actual notifications have been sent out.
//
// Note: handleUnmute clears i.MuteReason before this function runs, and handleMute sets it after, so a single
// i.IsMuted() check captures the correct transitional state for mute/unmute and steady-state events alike.
func (i *Incident) generateNotifications(
	ctx context.Context, tx *sqlx.Tx, ev *event.Event, contactChannels rule.ContactChannels,
) ([]*NotificationEntry, error) {
	var notifications []*NotificationEntry
	suppress := i.IsMuted()
	for contact, channels := range contactChannels {
		for chID := range channels {
			hr := &HistoryRow{
				IncidentID:        i.Id,
				Key:               recipient.ToKey(contact),
				Time:              types.UnixMilli(time.Now()),
				Type:              Notified,
				ChannelID:         types.MakeInt(chID, types.TransformZeroIntToNull),
				NotificationState: NotificationStatePending,
				Message:           types.MakeString(ev.Message, types.TransformEmptyStringToNull),
			}
			if suppress {
				hr.NotificationState = NotificationStateSuppressed
			}

			if err := hr.Sync(ctx, i.db, tx); err != nil {
				i.logger.Errorw("Failed to insert incident notification history",
					zap.String("contact", contact.FullName),
					zap.Bool("incident_muted", i.IsMuted()),
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
