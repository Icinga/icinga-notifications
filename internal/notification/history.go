package notification

import (
	"context"
	"time"

	"github.com/icinga/icinga-go-library/database"
	"github.com/icinga/icinga-go-library/types"
	"github.com/icinga/icinga-notifications/internal/recipient"
	"github.com/icinga/icinga-notifications/internal/rule"
	"github.com/jmoiron/sqlx"
	"github.com/pkg/errors"
)

// History represents a single history database entry.
type History struct {
	ID                int64     `db:"id"`
	IncidentID        types.Int `db:"incident_id"` // Is only set for incident related notifications
	RuleEntryID       types.Int `db:"rule_entry_id"`
	recipient.Key     `db:",inline"`
	Time              types.UnixMilli `db:"time"`
	ChannelID         int64           `db:"channel_id"`
	Message           types.String    `db:"message"`
	NotificationState State           `db:"notification_state"`
	SentAt            types.UnixMilli `db:"sent_at"`
}

// Sync persists the current state of this history to the database and retrieves the just inserted history ID.
// Returns error when failed to execute the query.
func (h *History) Sync(ctx context.Context, db *database.DB, tx *sqlx.Tx) error {
	historyId, err := database.InsertObtainID(ctx, tx, database.BuildInsertStmtWithout(db, h, "id"), h)
	if err != nil {
		return err
	}

	h.ID = historyId

	return nil
}

// TableName implements the contracts.TableNamer interface.
func (h *History) TableName() string {
	return "notification_history"
}

// Entry is used to cache a set of incident history fields of type Notified.
//
// The event processing workflow is performed in a separate transaction before trying to send the actual
// notifications. Thus, all resulting notification entries are marked as pending, and it creates a reference
// to them of this type. The cached entries are then used to actually notify the contacts and mark the pending
// notification entries as either StateSent or StateFailed.
type Entry struct {
	HistoryRowID int64 `db:"id"`
	ContactID    int64
	ChannelID    int64
	State        State           `db:"notification_state"`
	SentAt       types.UnixMilli `db:"sent_at"`
}

// TableName implements the contracts.TableNamer interface.
func (h *Entry) TableName() string {
	return "notification_history"
}

// InitHistoryFunc is used to additionally initialise a History entry before persisting it to the database.
type InitHistoryFunc func(*History)

// PendingNotifications is a map of per recipient.Contact pending notifications.
// Is just a short/readable form of the actual map.
type PendingNotifications map[*recipient.Contact][]*Entry

// AddNotifications inserts by default pending notification histories into the global notification History table.
// If you need to set/override some additional fields of the History type, you can specify an InitHistoryFunc
// as an argument that is called prior to persisting the history entry to the database.
//
// Returns on success PendingNotifications referencing the just inserted entries and error on any database failure.
func AddNotifications(
	ctx context.Context,
	db *database.DB,
	tx *sqlx.Tx,
	contactChannels rule.ContactChannels,
	initializer InitHistoryFunc,
) (PendingNotifications, error) {
	notifications := make(PendingNotifications)
	for contact, channels := range contactChannels {
		for chID := range channels {
			nh := &History{Key: recipient.ToKey(contact), Time: types.UnixMilli(time.Now()), ChannelID: chID}
			nh.NotificationState = StatePending
			if initializer != nil {
				// Might be used to initialise some context specific fields like "incident_id", "rule_entry_id" etc.
				initializer(nh)
			}

			if err := nh.Sync(ctx, db, tx); err != nil {
				return nil, errors.Wrapf(err, "cannot insert pending notification history for %q", contact.String())
			}

			notifications[contact] = append(notifications[contact], &Entry{
				HistoryRowID: nh.ID,
				ContactID:    contact.ID,
				ChannelID:    chID,
				State:        nh.NotificationState,
			})
		}
	}
	return notifications, nil
}
