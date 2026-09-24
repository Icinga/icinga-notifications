package incident

import (
	"context"
	"fmt"

	"github.com/icinga/icinga-go-library/database"
	"github.com/icinga/icinga-go-library/notifications/source"
	"github.com/icinga/icinga-go-library/types"
	"github.com/icinga/icinga-notifications/internal/object"
)

// NotificationHistory represents a single notification_history database entry,
// recording the fact that a notification was triggered for a given incident history entry.
type NotificationHistory struct {
	ID             int64                    `db:"id"`
	ObjectID       types.Binary             `db:"object_id"`
	EventID        types.UUID               `db:"event_id"`
	ContactID      int64                    `db:"contact_id"`
	ContactgroupID types.Int                `db:"contactgroup_id"`
	ScheduleID     types.Int                `db:"schedule_id"`
	ChannelID      int64                    `db:"channel_id"`
	IncidentID     types.Int                `db:"incident_id"`
	EventMessage   string                   `db:"event_message"`
	State          source.NotificationState `db:"state"`
	TriggeredAt    types.UnixMilli          `db:"triggered_at"`
}

// Sync persists the current state of this notification history to the database and retrieves the just inserted
// history ID. Returns error when failed to execute the query.
func (n *NotificationHistory) Sync(ctx context.Context, db *database.DB) error {
	historyId, err := database.InsertObtainID(ctx, db, database.BuildInsertStmtWithout(db, n, "id"), n)
	if err != nil {
		return err
	}

	n.ID = historyId

	return nil
}

// YieldNotificationHistory yields all notification_history entries since the given timestamp as a channel of
// NotificationHistoryPair. The second channel is used to report errors that occur during the query or iteration.
// The function returns immediately, and the caller should read from the channels until they are closed.
func YieldNotificationHistory(
	ctx context.Context,
	db *database.DB,
	since int64,
) (<-chan NotificationHistoryPair, <-chan error) {
	query := `
	SELECT 
		nh.event_id,
		nh.triggered_at,
		c.full_name AS contact_name,
		cg.name AS contactgroup_name,
		s.name AS schedule_name,
		ch.name AS channel_name,
		nh.event_message,
		nh.state,
		nh.object_id
    FROM notification_history nh
    LEFT JOIN contact c ON nh.contact_id = c.id AND c.deleted = 'n'
    LEFT JOIN channel ch ON nh.channel_id = ch.id
	LEFT JOIN contactgroup cg ON nh.contactgroup_id = cg.id
	LEFT JOIN schedule s ON nh.schedule_id = s.id
    WHERE nh.triggered_at >= ?`

	valueCh := make(chan NotificationHistoryPair)
	errCh := make(chan error, 1) // buffered to avoid goroutine leak if the receiver is not ready to receive errors.

	go func() {
		defer close(valueCh)
		defer close(errCh)

		rows, err := db.QueryxContext(ctx, db.Rebind(query), since)
		if err != nil {
			errCh <- fmt.Errorf("cannot query notification_history entries: %w", err)
			return
		}
		defer func() { _ = rows.Close() }()

		objects := make(map[string]*object.Object)
		for rows.Next() {
			nh := new(NotificationHistoryPair)
			if err := rows.StructScan(nh); err != nil {
				errCh <- fmt.Errorf("cannot scan notification_history entries from row: %w", err)
				return
			}

			obj, ok := objects[nh.ObjectID.String()]
			if !ok {
				obj, err = object.Get(ctx, db, nh.ObjectID)
				if err != nil {
					errCh <- fmt.Errorf("cannot get object for notification_history entry: %w", err)
					return
				}
			}
			nh.Object = obj

			select {
			case valueCh <- *nh:
			case <-ctx.Done():
				errCh <- ctx.Err()
				return
			}
		}

		if err := rows.Err(); err != nil {
			errCh <- fmt.Errorf("notification_history cursor error: %w", err)
		}
	}()

	return valueCh, errCh
}

// SkippedNotificationHistory represents a single skipped_notification_history database entry, recording the path
// a notification was skipped for a given incident history entry.
type SkippedNotificationHistory struct {
	NotificationID   int64     `db:"notification_history_id"`
	RuleID           int64     `db:"rule_id"`
	RuleEscalationID int64     `db:"rule_escalation_id"`
	ContactgroupID   types.Int `db:"contactgroup_id"`
	ScheduleID       types.Int `db:"schedule_id"`
}

type NotificationHistoryPair struct {
	source.NotificationHistory
	ObjectID types.Binary `db:"object_id"`
	Object   *object.Object
}
