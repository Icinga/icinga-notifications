package history

import (
	"context"
	"github.com/icinga/icinga-notifications/internal/recipient"
	"github.com/icinga/icinga-notifications/internal/rule"
	"github.com/icinga/icinga-notifications/internal/utils"
	"github.com/icinga/icingadb/pkg/icingadb"
	"github.com/icinga/icingadb/pkg/types"
	"github.com/jmoiron/sqlx"
	"github.com/pkg/errors"
	"time"
)

// NotificationHistory represents a single history database entry.
type NotificationHistory struct {
	ID                int64     `db:"id"`
	IncidentID        types.Int `db:"incident_id"`     // Is only set for incident related notifications
	RuleRoutingID     types.Int `db:"rule_routing_id"` // Is only set for non-incident related notifications
	recipient.Key     `db:",inline"`
	Time              types.UnixMilli   `db:"time"`
	ChannelID         int64             `db:"channel_id"`
	Message           types.String      `db:"message"`
	NotificationState NotificationState `db:"notification_state"`
	SentAt            types.UnixMilli   `db:"sent_at"`
}

// Persist adds the current state of this history to the database.
// You can set the last argument to true if you want to retrieve the just inserted history ID.
// Returns error when failed to execute the query.
func (h *NotificationHistory) Persist(ctx context.Context, db *icingadb.DB, tx *sqlx.Tx, fetchID bool) error {
	stmt := utils.BuildInsertStmtWithout(db, h, "id")
	if fetchID {
		historyId, err := utils.InsertAndFetchId(ctx, tx, stmt, h)
		if err != nil {
			return err
		}

		h.ID = historyId

		return nil
	}

	if _, err := tx.NamedExecContext(ctx, stmt, h); err != nil {
		return err
	}

	return nil
}

// NotificationEntry is used to cache a set of incident history fields of type Notified.
//
// The event processing workflow is performed in a separate transaction before trying to send the actual
// notifications. Thus, all resulting notification entries are marked as pending, and it creates a reference
// to them of this type. The cached entries are then used to actually notify the contacts and mark the pending
// notification entries as either NotificationStateSent or NotificationStateFailed.
type NotificationEntry struct {
	HistoryRowID int64 `db:"id"`
	ContactID    int64
	ChannelID    int64
	Message      types.String      `db:"message"`
	State        NotificationState `db:"notification_state"`
	SentAt       types.UnixMilli   `db:"sent_at"`
}

// TableName implements the contracts.TableNamer interface.
func (h *NotificationEntry) TableName() string {
	return "notification_history"
}

// PendingNotifications is a map of per recipient.Contact pending notifications.
// Is just a short/readable form of the actual map.
type PendingNotifications map[*recipient.Contact][]*NotificationEntry

// AddPendingNotifications inserts pending notifications into the global notification history table.
// If you need to set some additional fields of the NotificationHistory type, you can specify a callback
// as an argument that is called prior to persisting the history entry to the database.
//
// Returns on success PendingNotifications referencing the just inserted entries and error on any database failure.
func AddPendingNotifications(
	ctx context.Context, db *icingadb.DB, tx *sqlx.Tx, contactChannels rule.ContactChannels, initEntry func(*NotificationHistory),
) (PendingNotifications, error) {
	notifications := make(PendingNotifications)
	for contact, channels := range contactChannels {
		for chID := range channels {
			nh := &NotificationHistory{
				Key:               recipient.ToKey(contact),
				Time:              types.UnixMilli(time.Now()),
				ChannelID:         chID,
				NotificationState: NotificationStatePending,
			}
			if initEntry != nil {
				// Might be used to initialise some context specific fields like "incident_id", "rule_routing_id" etc.
				initEntry(nh)
			}

			if err := nh.Persist(ctx, db, tx, true); err != nil {
				return nil, errors.Wrapf(err, "cannot insert pending notification history for %q", contact.String())
			}

			notifications[contact] = append(notifications[contact], &NotificationEntry{
				HistoryRowID: nh.ID,
				ContactID:    contact.ID,
				State:        NotificationStatePending,
				ChannelID:    chID,
			})
		}
	}

	return notifications, nil
}
