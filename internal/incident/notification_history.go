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

	// SkippedHistory holds all notification_skipped_history rows recorded for this notification, if any.
	// It is not part of the notification_history table itself and must not be scanned or written from/to it.
	SkippedHistory []NotificationSkippedHistoryEntry `db:"-"`
}

// TableName implements the contracts.TableNamer interface.
func (n *NotificationHistoryEntry) TableName() string {
	return "notification_history"
}

// NotificationSkippedHistoryEntry represents a single notification_skipped_history database entry, recording why a
// notification for a given rule/escalation/recipient was skipped instead of being sent.
type NotificationSkippedHistoryEntry struct {
	ID               int64     `db:"id"`
	NotificationID   int64     `db:"notification_id"`
	RuleID           int64     `db:"rule_id"`
	RuleEscalationID int64     `db:"rule_escalation_id"`
	IncidentID       types.Int `db:"incident_id"`
	ContactgroupID   types.Int `db:"contactgroup_id"`
	ScheduleID       types.Int `db:"schedule_id"`
}

// TableName implements the contracts.TableNamer interface.
func (n *NotificationSkippedHistoryEntry) TableName() string {
	return "notification_skipped_history"
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
//
// Each yielded entry has its SkippedHistory field populated with the notification_skipped_history rows recorded
// for it, if any.
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

	entryCh, entryErrCh := yieldQuery[NotificationHistoryEntry](ctx, db, query, since, sourceID)

	return attachSkippedHistory(ctx, db, entryCh, entryErrCh)
}

// skippedHistoryBatchSize is the number of NotificationHistoryEntry values buffered before their
// notification_skipped_history rows are looked up in a single batched query.
const skippedHistoryBatchSize = 100

// attachSkippedHistory consumes entries from entryCh, populates each entry's SkippedHistory field with its
// associated notification_skipped_history rows, and forwards the enriched entries on the returned channel.
//
// notification_history has a one-to-many relation to notification_skipped_history, which can't be flattened into a
// single struct via a JOIN and StructScan (one SQL row always scans into exactly one Go value). Since the caller may
// be streaming an unbounded, timestamp-selected range rather than a small fixed set, entries are buffered in bounded
// batches and their skipped-history rows fetched with one IN(...) query per batch, instead of either buffering the
// whole result set or issuing one lookup query per entry.
func attachSkippedHistory(
	ctx context.Context,
	db *database.DB,
	entryCh <-chan NotificationHistoryEntry,
	entryErrCh <-chan error,
) (<-chan NotificationHistoryEntry, <-chan error) {
	outCh := make(chan NotificationHistoryEntry)
	errCh := make(chan error, 1)

	go func() {
		defer close(outCh)
		defer close(errCh)

		batch := make([]NotificationHistoryEntry, 0, skippedHistoryBatchSize)

		// flush looks up and attaches SkippedHistory for the buffered batch, sends each entry on outCh, and
		// resets the batch. It reports whether the caller should keep consuming entryCh.
		flush := func() bool {
			if len(batch) == 0 {
				return true
			}

			skipped, err := loadSkippedHistoryBatch(ctx, db, batch)
			if err != nil {
				errCh <- err
				return false
			}

			for _, entry := range batch {
				entry.SkippedHistory = skipped[entry.ID]

				select {
				case outCh <- entry:
				case <-ctx.Done():
					errCh <- ctx.Err()
					return false
				}
			}

			batch = batch[:0]
			return true
		}

		for entry := range entryCh {
			batch = append(batch, entry)
			if len(batch) >= skippedHistoryBatchSize {
				if !flush() {
					return
				}
			}
		}

		if !flush() {
			return
		}

		if err, ok := <-entryErrCh; ok {
			errCh <- err
		}
	}()

	return outCh, errCh
}

// loadSkippedHistoryBatch returns the notification_skipped_history rows for all given entries, grouped by
// notification_id.
func loadSkippedHistoryBatch(
	ctx context.Context,
	db *database.DB,
	batch []NotificationHistoryEntry,
) (map[int64][]NotificationSkippedHistoryEntry, error) {
	ids := make([]int64, len(batch))
	for i, entry := range batch {
		ids[i] = entry.ID
	}

	query, args, err := sqlx.In(`SELECT * FROM notification_skipped_history WHERE notification_id IN (?)`, ids)
	if err != nil {
		return nil, fmt.Errorf("cannot build notification_skipped_history query: %w", err)
	}

	var rows []NotificationSkippedHistoryEntry
	if err := db.SelectContext(ctx, &rows, db.Rebind(query), args...); err != nil {
		return nil, fmt.Errorf("cannot query notification_skipped_history entries: %w", err)
	}

	grouped := make(map[int64][]NotificationSkippedHistoryEntry, len(batch))
	for _, row := range rows {
		grouped[row.NotificationID] = append(grouped[row.NotificationID], row)
	}

	return grouped, nil
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
