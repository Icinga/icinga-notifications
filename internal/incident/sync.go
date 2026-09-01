package incident

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/icinga/icinga-go-library/database"
	baseEv "github.com/icinga/icinga-go-library/notifications/event"
	"github.com/icinga/icinga-go-library/notifications/source"
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

// AddRecipient adds recipient from the given *rule.Escalation to this incident.
// Syncs also all the recipients with the database and returns an error on db failure.
func (i *Incident) AddRecipient(ctx context.Context, tx *sqlx.Tx, escalation *rule.Escalation) error {
	newRole := recipient.RoleRecipient
	if i.HasManager() {
		newRole = recipient.RoleSubscriber
	}

	for _, escalationRecipient := range escalation.Recipients {
		r := escalationRecipient.Recipient
		cr := &ContactRow{IncidentID: i.Id, Role: newRole, ChangedAt: types.UnixMilli(time.Now())}

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
					Key:              cr.Key,
					Time:             types.UnixMilli(time.Now()),
					Type:             RecipientRoleChanged,
					NewRecipientRole: newRole,
					OldRecipientRole: oldRole,
				}

				if err := hr.Sync(ctx, i.db, tx); err != nil {
					i.logger.Errorw(
						"Failed to insert recipient role changed incident history", zap.Object("escalation", escalation),
						zap.String("recipients", r.String()), zap.Error(err),
					)
					return err
				}
			}
			cr.Role = state.Role
		}

		stmt, _ := i.db.BuildUpsertStmt(cr, "id")
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
	stmt, _ := i.db.BuildUpsertStmt(rr)
	_, err := tx.NamedExecContext(ctx, stmt, rr)

	return err
}

// generateNotifications generates incident notification histories of the given recipients.
//
// This function will just insert NotificationStateSuppressed incident histories and return an empty slice if
// the incident is muted, otherwise a slice of pending *NotificationEntry(ies) that can be used to update
// the corresponding histories after the actual notifications have been sent out. The given eventID correlates
// with the generated notification histories so that they can be traced back to the event that triggered them.
//
// A contact+channel pair selected by multiple origins yields a single pending entry for the first origin,
// plus one NotificationStateSuperfluous entry per additional origin. Superfluous entries are only recorded
// in the notification history, they do not get an incident_history row and are never delivered.
//
// Note: handleUnmute clears i.MuteReason before this function runs, and handleMute sets it after, so a single
// i.IsMuted() check captures the correct transitional state for mute/unmute and steady-state events alike.
func (i *Incident) generateNotifications(
	ctx context.Context, tx *sqlx.Tx, ev *event.Event, contactChannels rule.ContactChannels,
) ([]*NotificationEntry, error) {
	var notificationState source.NotificationState
	suppress := i.IsMuted()
	if suppress {
		notificationState = source.NotificationStateSuppressed
	} else {
		notificationState = source.NotificationStatePending
	}

	var notifications []*NotificationEntry
	for contact, channelOrigins := range contactChannels {
		slices.SortStableFunc(channelOrigins, func(a, b rule.ChannelOrigin) int {
			return cmp.Compare(a.ChannelID, b.ChannelID)
		})

		var lastChannelID int64
		var notificationOfCurrentChannel *NotificationEntry
		for _, origin := range channelOrigins {
			if lastChannelID != origin.ChannelID {
				lastChannelID = origin.ChannelID
				hr := &HistoryRow{
					IncidentID:        i.Id,
					Key:               recipient.ToKey(contact),
					Time:              types.UnixMilli(time.Now()),
					Type:              Notified,
					ChannelID:         types.MakeInt(origin.ChannelID, types.TransformZeroIntToNull),
					NotificationState: notificationState,
					Message:           types.MakeString(ev.Message, types.TransformEmptyStringToNull),
				}

				if err := hr.Sync(ctx, i.db, tx); err != nil {
					i.logger.Errorw("Failed to insert incident notification history",
						zap.String("contact", contact.FullName),
						zap.Bool("incident_muted", i.IsMuted()),
						zap.Error(err))
					return nil, err
				}

				if suppress {
					// If the incident is muted, we don't need to create a pending notification entry,
					// so we can skip to the next origin.
					continue
				}

				notificationHistory := NotificationHistory{
					ObjectID:       i.ObjectID,
					EventID:        ev.ID,
					ContactID:      contact.ID,
					ContactgroupID: types.MakeInt(origin.ContactGroupID, types.TransformZeroIntToNull),
					ScheduleID:     types.MakeInt(origin.ScheduleID, types.TransformZeroIntToNull),
					ChannelID:      origin.ChannelID,
					IncidentID:     types.MakeInt(i.Id),
					EventMessage:   ev.Message,
				}

				notificationOfCurrentChannel = &NotificationEntry{
					HistoryRowID: hr.ID,
					ContactID:    contact.ID,
					ChannelID:    origin.ChannelID,
					State:        source.NotificationStatePending,
					HistoryEntry: notificationHistory,
				}

				notifications = append(notifications, notificationOfCurrentChannel)
			} else if notificationOfCurrentChannel != nil {
				notificationOfCurrentChannel.SkippedHistoryEntries = append(
					notificationOfCurrentChannel.SkippedHistoryEntries,
					SkippedNotificationHistory{
						RuleID:           origin.RuleID,
						RuleEscalationID: origin.RuleEscalationID,
						ContactgroupID:   types.MakeInt(origin.ContactGroupID, types.TransformZeroIntToNull),
						ScheduleID:       types.MakeInt(origin.ScheduleID, types.TransformZeroIntToNull),
					},
				)
			}
		}
	}

	return notifications, nil
}
