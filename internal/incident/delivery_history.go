package incident

import (
	"context"

	"github.com/icinga/icinga-go-library/backoff"
	"github.com/icinga/icinga-go-library/database"
	"github.com/icinga/icinga-go-library/retry"
	"github.com/icinga/icinga-go-library/types"
)

// DeliveryEntry represents a single delivery_history database entry, recording the delivery
// attempt of a notification to a contact including the rule/escalation/recipient it originated from.
type DeliveryEntry struct {
	ID                int64             `db:"id"`
	IncidentID        int64             `db:"incident_id"`
	RuleID            types.Int         `db:"rule_id"`
	RuleEscalationID  types.Int         `db:"rule_escalation_id"`
	ContactID         int64             `db:"contact_id"`
	ContactgroupID    types.Int         `db:"contactgroup_id"`
	ChannelID         int64             `db:"channel_id"`
	ScheduleID        types.Int         `db:"schedule_id"`
	Message           types.String      `db:"message"`
	Reason            HistoryEventType  `db:"reason"`
	SentAt            types.UnixMilli   `db:"sent_at"`
	NotificationState NotificationState `db:"notification_state"`
}

// TableName implements the contracts.TableNamer interface.
func (d *DeliveryEntry) TableName() string {
	return "delivery_history"
}

// WriteToDatabase inserts this delivery entry into the database, retrying on transient errors.
func (d *DeliveryEntry) WriteToDatabase(ctx context.Context, db *database.DB) error {
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
