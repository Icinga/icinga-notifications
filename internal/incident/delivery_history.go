package incident

import (
	"context"

	"github.com/icinga/icinga-go-library/backoff"
	"github.com/icinga/icinga-go-library/database"
	"github.com/icinga/icinga-go-library/retry"
	"github.com/icinga/icinga-go-library/types"
)

// NotificationHistoryEntry represents a single notification_history database entry, recording the notification sent
// attempt of a notification to a contact including the rule/escalation/recipient it originated from.
type NotificationHistoryEntry struct {
	ID               int64             `db:"id"`
	IncidentID       int64             `db:"incident_id"`
	RuleID           int64             `db:"rule_id"`
	RuleEscalationID int64             `db:"rule_escalation_id"`
	ContactID        int64             `db:"contact_id"`
	ContactgroupID   types.Int         `db:"contactgroup_id"`
	ChannelID        types.Int         `db:"channel_id"`
	ScheduleID       types.Int         `db:"schedule_id"`
	Message          types.String      `db:"message"`
	Reason           HistoryEventType  `db:"reason"`
	State            NotificationState `db:"state"`
	SentAt           types.UnixMilli   `db:"sent_at"`
}

// TableName implements the contracts.TableNamer interface.
func (d *NotificationHistoryEntry) TableName() string {
	return "notification_history"
}

// WriteToDatabase inserts this notification history entry into the database, retrying on transient errors.
func (d *NotificationHistoryEntry) WriteToDatabase(ctx context.Context, db *database.DB) error {
	stmt := database.BuildInsertStmtWithout(db, d, "id")

	return retry.WithBackoff(
		ctx,
		func(ctx context.Context) error {
			_, err := db.NamedExecContext(ctx, stmt, d)
			if err != nil {
				return database.CantPerformQuery(err, stmt)
			}

			return nil
		},
		retry.Retryable,
		backoff.DefaultBackoff,
		db.GetDefaultRetrySettings())
}
