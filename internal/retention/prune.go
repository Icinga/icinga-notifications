package retention

import (
	"context"
	"fmt"

	"github.com/icinga/icinga-go-library/backoff"
	"github.com/icinga/icinga-go-library/com"
	"github.com/icinga/icinga-go-library/database"
	"github.com/icinga/icinga-go-library/retry"
	"github.com/icinga/icinga-go-library/types"
	"github.com/jmoiron/sqlx"
)

// TimeBoundPruner defines the configuration for pruning rows from a table based on a time column.
//
// This struct is designed to be flexible and reusable for different tables with varying time columns and
// primary keys. It also supports maintaining referential integrity by allowing the definition of related
// tables that reference the primary keys of the main table, ensuring that any related rows are also pruned
// accordingly.
type TimeBoundPruner struct {
	Table      string
	PK         string
	TimeColumn string

	Referrers []ReferencingRowPruner
}

// Exec prunes rows from the specified table that are older than the given time threshold.
//
// If Referrers are defined, it first retrieves the primary keys of the rows to be deleted and then executes the
// corresponding DELETE statements for each [ReferencingRowPruner] before finally deleting the rows from the main
// table. If no Referrers are defined, it directly deletes rows based on the time column.
//
// The method returns only the total number of deleted rows from the main table.
func (tbp *TimeBoundPruner) Exec(ctx context.Context, db *database.DB, olderThan types.UnixMilli, limit uint64) (uint64, error) {
	deleteStmt := tbp.assembleDelete(db.DriverName(), limit, len(tbp.Referrers) > 0)
	if len(tbp.Referrers) == 0 {
		return exec(ctx, db, deleteStmt, limit, olderThan)
	}

	selectStmt := tbp.assembleSelect(limit)
	var total uint64
	for {
		var ids []any
		err := retry.WithBackoff(
			ctx,
			func(ctx context.Context) error {
				if err := db.SelectContext(ctx, &ids, db.Rebind(selectStmt), olderThan); err != nil {
					return database.CantPerformQuery(err, selectStmt)
				}
				return nil
			},
			retry.Retryable,
			backoff.DefaultBackoff,
			db.GetDefaultRetrySettings(),
		)
		if err != nil {
			return 0, err
		}

		if len(ids) == 0 {
			// No rows to delete, so we can skip executing the referrers and the final delete statement.
			return total, nil
		}

		for _, referrer := range tbp.Referrers {
			if _, err := referrer.Exec(ctx, db, ids...); err != nil {
				return 0, err
			}
		}

		if affected, err := exec(ctx, db, deleteStmt, limit, ids...); err != nil {
			return 0, err
		} else {
			total += affected
		}
	}
}

// assembleSelect constructs a select stmt to retrieve primary keys of this pruner based on the time column and limit.
func (tbp *TimeBoundPruner) assembleSelect(limit uint64) string {
	return fmt.Sprintf(`SELECT %s FROM %s WHERE %[3]s IS NOT NULL AND %[3]s < ? LIMIT %d`, tbp.PK, tbp.Table, tbp.TimeColumn, limit)
}

// assembleDelete constructs a delete stmt for this pruner based on the database driver and whether we are
// deleting by primary keys or by time column.
func (tbp *TimeBoundPruner) assembleDelete(driverName string, limit uint64, byPKs bool) string {
	if byPKs {
		// The limit doesn't apply when deleting by PKs, as the number of PKs is already limited by the provided arguments.
		return fmt.Sprintf(`DELETE FROM %s WHERE %s IN (?)`, tbp.Table, tbp.PK)
	}

	switch driverName {
	case database.MySQL:
		return fmt.Sprintf(`DELETE FROM %s WHERE %[2]s IS NOT NULL AND %[2]s < ? LIMIT %d`, tbp.Table, tbp.TimeColumn, limit)
	case database.PostgreSQL:
		return fmt.Sprintf(`
WITH rows AS (SELECT %[1]s FROM %[2]s WHERE %[3]s IS NOT NULL AND %[3]s < ? LIMIT %[4]d)
DELETE FROM %[2]s WHERE %[1]s IN (SELECT * FROM rows)`,
			tbp.PK, tbp.Table, tbp.TimeColumn, limit)
	default:
		panic(fmt.Sprintf("invalid database type %s", driverName))
	}
}

// ReferencingRowPruner defines the configuration for pruning rows that reference the primary keys of another table.
//
// This struct is used to maintain referential integrity when pruning rows from a main table by ensuring that
// any related rows in other tables are also pruned accordingly. In other words, it allows to explicitly define
// cascading deletes for related tables without relying on database-level foreign key constraints.
type ReferencingRowPruner struct {
	Table string
	FK    string
}

// Exec performs the pruning operation by deleting rows from the specified table that reference the given primary keys.
func (rrp *ReferencingRowPruner) Exec(ctx context.Context, db *database.DB, pks ...any) (uint64, error) {
	return exec(ctx, db, rrp.assembleDelete(), 0, pks...)
}

// assembleDelete constructs the delete statement for the ReferencingRowPruner based on the foreign key.
func (rrp *ReferencingRowPruner) assembleDelete() string {
	return fmt.Sprintf(`DELETE FROM %s WHERE %s IN (?)`, rrp.Table, rrp.FK)
}

// exec executes the given delete statement with the provided arguments and limit, handling retries and counting affected rows.
func exec(ctx context.Context, db *database.DB, query string, limit uint64, args ...any) (uint64, error) {
	stmt, expandedArgs, err := sqlx.In(query, args)
	if err != nil {
		return 0, err
	}

	var counter com.Counter
	defer db.Log(ctx, query, &counter).Stop()

	for {
		var affected uint64
		err = retry.WithBackoff(
			ctx,
			func(ctx context.Context) error {
				res, err := db.ExecContext(ctx, db.Rebind(stmt), expandedArgs...)
				if err != nil {
					return database.CantPerformQuery(err, query)
				}

				n, err := res.RowsAffected()
				if err == nil && n > 0 {
					affected = uint64(n)
				}
				return err
			},
			retry.Retryable,
			backoff.DefaultBackoff,
			db.GetDefaultRetrySettings(),
		)
		if err != nil {
			return 0, err
		}

		counter.Add(affected)
		// If limit is set to 0, it means we are deleting matching rows by primary keys, so the limit check can be skipped.
		if limit == 0 || affected < limit {
			break
		}
	}

	return counter.Total(), nil
}
