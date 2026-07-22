package incident

import (
	"context"
	"fmt"

	"github.com/icinga/icinga-go-library/backoff"
	"github.com/icinga/icinga-go-library/database"
	"github.com/icinga/icinga-go-library/retry"
	"github.com/icinga/icinga-go-library/types"
	"github.com/jmoiron/sqlx"
)

// NotificationHistoryEntry represents a single notification_history database entry, recording the notification sent
// attempt of a notification to a contact including the rule/escalation/recipient it originated from.
type NotificationHistoryEntry struct {
	ID               int64             `db:"id"`
	RuleID           int64             `db:"rule_id"`
	RuleEscalationID int64             `db:"rule_escalation_id"`
	ContactID        int64             `db:"contact_id"`
	ChannelID        int64             `db:"channel_id"`
	IncidentID       types.Int         `db:"incident_id"`
	ContactgroupID   types.Int         `db:"contactgroup_id"`
	ScheduleID       types.Int         `db:"schedule_id"`
	Message          types.String      `db:"message"`
	Reason           HistoryEventType  `db:"reason"`
	State            NotificationState `db:"state"`
	TriggeredAt      types.UnixMilli   `db:"triggered_at"`
}

// TableName implements the contracts.TableNamer interface.
func (n *NotificationHistoryEntry) TableName() string {
	return "notification_history"
}

// Sync persists the current state of this history to the database and retrieves the just inserted history ID.
// Returns error when failed to execute the query.
func (n *NotificationHistoryEntry) Sync(ctx context.Context, db *database.DB, tx *sqlx.Tx) error {
	historyId, err := database.InsertObtainID(ctx, tx, database.BuildInsertStmtWithout(db, n, "id"), n)
	if err != nil {
		return err
	}

	n.ID = historyId

	return nil
}

// WriteToDatabase inserts this notification history entry into the database, retrying on transient errors.
func (n *NotificationHistoryEntry) WriteToDatabase(ctx context.Context, db *database.DB) error {
	stmt := database.BuildInsertStmtWithout(db, n, "id")

	return retry.WithBackoff(
		ctx,
		func(ctx context.Context) error {
			_, err := db.NamedExecContext(ctx, stmt, n)
			if err != nil {
				return database.CantPerformQuery(err, stmt)
			}

			return nil
		},
		retry.Retryable,
		backoff.DefaultBackoff,
		db.GetDefaultRetrySettings())
}

// YieldNotificationHistoryForSource returns a channel of [Pair] for all active incidents of the given source (see yield docstring).
func YieldNotificationHistoryForSource(
	ctx context.Context,
	db *database.DB,
	since string,
	sourceID int64,
) (<-chan NotificationHistoryEntry, <-chan error) {
	// TODO: which sources should be included and how to handle the relation if the hardlink between rule and source is removed?
	query := `SELECT nh.* FROM notification_history nh
    JOIN rule r ON nh.rule_id = r.id
	WHERE nh.triggered_at >= ? AND r.source_id = ?`

	return yieldQuery[NotificationHistoryEntry](ctx, db, query, since, sourceID)
}

// yieldQuery runs the given query in a separate goroutine and sends each result to the returned channel.
//
// The query must not be a named query, and must ensure to provide the correct arguments (if any) for the query.
func yieldQuery[T any](
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

		rows, err := db.QueryxContext(ctx, db.Rebind(query), args...)
		if err != nil {
			errCh <- fmt.Errorf("cannot query notification history entries: %w", err)
			return
		}
		defer func() { _ = rows.Close() }()

		for rows.Next() {
			nh := new(T)
			if err := rows.StructScan(nh); err != nil {
				errCh <- fmt.Errorf("cannot scan notification history entries from row: %w", err)
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
			errCh <- fmt.Errorf("incidents cursor error: %w", err)
		}
	}()

	return valueCh, errCh
}
