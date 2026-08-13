package incident

import (
	"context"
	"fmt"

	"github.com/icinga/icinga-go-library/database"
	"github.com/icinga/icinga-go-library/notifications/source"
	"github.com/icinga/icinga-go-library/types"
	"github.com/jmoiron/sqlx"
)

// NotificationHistory represents a single notification_history database entry,
// recording the fact that a notification was triggered for a given incident history entry.
type NotificationHistory struct {
	ID                int64                    `db:"id"`
	IncidentHistoryID int64                    `db:"incident_history_id"`
	SourceID          int64                    `db:"source_id"`
	EventID           types.UUID               `db:"event_id"`
	TriggeredAt       types.UnixMilli          `db:"triggered_at"`
	ContactID         int64                    `db:"contact_id"`
	ContactgroupID    types.Int                `db:"contactgroup_id"`
	ScheduleID        types.Int                `db:"schedule_id"`
	ChannelID         int64                    `db:"channel_id"`
	IncidentID        types.Int                `db:"incident_id"`
	Message           types.String             `db:"message"`
	State             source.NotificationState `db:"state"`
}

// TableName implements the contracts.TableNamer interface.
func (n *NotificationHistory) TableName() string {
	return "notification_history"
}

// Sync persists the current state of this notification history to the database and retrieves the just inserted
// history ID. Returns error when failed to execute the query.
func (n *NotificationHistory) Sync(ctx context.Context, db *database.DB, tx *sqlx.Tx) error {
	historyId, err := database.InsertObtainID(ctx, tx, database.BuildInsertStmtWithout(db, n, "id"), n)
	if err != nil {
		return err
	}

	n.ID = historyId

	return nil
}

// NotificationHistoryUpdate represents a single notification_history database entry update.
type NotificationHistoryUpdate struct {
	IncidentHistoryID int64                    `db:"incident_history_id"`
	State             source.NotificationState `db:"state"`
	TriggeredAt       types.UnixMilli          `db:"triggered_at"`
}

// TableName implements the contracts.TableNamer interface.
func (n *NotificationHistoryUpdate) TableName() string {
	return "notification_history"
}

// Scope returns a struct with the fields that uniquely identify this notification history update in the database.
func (n *NotificationHistoryUpdate) Scope() any {
	return struct {
		IncidentHistoryID int64 `db:"incident_history_id"`
	}{}
}

// YieldNotificationHistoryForSource returns a channel of [Pair] for all active incidents of the given source (see yield docstring).
//
// Each yielded entry has its SkippedHistory field populated with the skipped_notification_history rows recorded
// for it, if any.
func YieldNotificationHistoryForSource(
	ctx context.Context,
	db *database.DB,
	since string,
	srcID int64,
) (<-chan source.NotificationHistory, <-chan error) {
	// TODO: which sources should be included?
	query := `SELECT 
    nh.event_id,
    nh.triggered_at,
    c.full_name AS contact_name,
    COALESCE(cg.name, '') AS contactgroup_name,
    COALESCE(s.name, '') AS schedule_name,
    ch.name AS channel_name,
    nh.incident_id,
    nh.message,
    nh.state
    FROM notification_history nh
    JOIN "contact" c ON nh.contact_id = c.id
    JOIN "channel" ch ON nh.channel_id = ch.id
	LEFT JOIN "contactgroup" cg ON nh.contactgroup_id = cg.id
	LEFT JOIN "schedule" s ON nh.schedule_id = s.id
    WHERE nh.triggered_at >= ? AND nh.source_id = ?`

	entryCh, entryErrCh := yieldQuery[source.NotificationHistory](ctx, db, query, since, srcID)

	return entryCh, entryErrCh
}

// NotificationSkippedHistoryEntry represents a single skipped_notification_history database entry, recording the path
// a notification was skipped for a given incident history entry.
type NotificationSkippedHistoryEntry struct {
	NotificationID   int64     `db:"notification_history_id"`
	RuleID           int64     `db:"rule_id"`
	RuleEscalationID int64     `db:"rule_escalation_id"`
	ContactgroupID   types.Int `db:"contactgroup_id"`
	ScheduleID       types.Int `db:"schedule_id"`
}

// TableName implements the contracts.TableNamer interface.
func (ns *NotificationSkippedHistoryEntry) TableName() string {
	return "skipped_notification_history"
}

// Sync persists the current state of this skipped notification history to the database and retrieves the just inserted
// history ID. Returns error when failed to execute the query.
func (ns *NotificationSkippedHistoryEntry) Sync(ctx context.Context, db *database.DB, tx *sqlx.Tx) error {
	_, err := database.InsertObtainID(ctx, tx, database.BuildInsertStmtWithout(db, ns, "id"), ns)
	if err != nil {
		return err
	}

	//ns.ID = skippedHistoryId

	return nil
}

// yieldQuery runs the given query in a separate goroutine and sends each result to the returned channel.
//
// The query must not be a named query, and must ensure to provide the correct arguments (if any) for the query.
func yieldQuery[T any, PT interface {
	*T
	database.TableNamer
}](
	ctx context.Context,
	db *database.DB,
	query string,
	args ...any,
) (<-chan T, <-chan error) {
	valueCh := make(chan T)
	errCh := make(chan error, 1) // buffered to avoid goroutine leak if the receiver is not ready to receive errors.

	go func() {
		defer close(valueCh)
		defer close(errCh)

		var zero T
		tableName := PT(&zero).TableName()

		rows, err := db.QueryxContext(ctx, db.Rebind(query), args...)
		if err != nil {
			errCh <- fmt.Errorf("cannot query %s entries: %w", tableName, err)
			return
		}
		defer func() { _ = rows.Close() }()

		for rows.Next() {
			nh := new(T)
			if err := rows.StructScan(nh); err != nil {
				errCh <- fmt.Errorf("cannot scan %s entries from row: %w", tableName, err)
				return
			}

			select {
			case valueCh <- *nh:
			case <-ctx.Done():
				errCh <- ctx.Err()
				return
			}
		}

		if err := rows.Err(); err != nil {
			errCh <- fmt.Errorf("%s cursor error: %w", tableName, err)
		}
	}()

	return valueCh, errCh
}
