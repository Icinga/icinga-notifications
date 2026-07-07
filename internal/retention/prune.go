package retention

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/icinga/icinga-go-library/backoff"
	"github.com/icinga/icinga-go-library/com"
	"github.com/icinga/icinga-go-library/database"
	"github.com/icinga/icinga-go-library/logging"
	"github.com/icinga/icinga-go-library/retry"
	"github.com/icinga/icinga-go-library/types"
	"github.com/jmoiron/sqlx"
	"go.uber.org/zap"
)

// Pruner defines the interface for pruning rows from a database table.
type Pruner interface {
	// TableName returns the name of the table to be pruned.
	TableName() string

	// IntervalAndPeriodOverrides returns the duration that overrides the global retention period and interval for this pruner.
	IntervalAndPeriodOverrides() time.Duration

	// Exec performs the pruning operation for the specific pruner.
	//
	// It takes a context, a database connection, a time threshold (olderThan), and a limit on the number of rows
	// to delete. It's up to the specific pruner whether the provided arguments are relevant for it or not.
	// The method returns the number of deleted rows and any error encountered during the operation.
	Exec(context.Context, *database.DB, *logging.Logger, types.UnixMilli, uint64) (uint64, error)
}

// prunerCommon provides the common fields and methods for pruning rows from a database table based on a PK or FK.
type prunerCommon struct {
	Table  string
	PKorFK string
}

// assembleDeleteByPK constructs a delete statement for the base pruner based on the primary key or foreign key.
func (pc prunerCommon) assembleDeleteByPK() string {
	return fmt.Sprintf(`DELETE FROM %s WHERE %s IN (?)`, pc.Table, pc.PKorFK)
}

// TimeBoundPruner defines the configuration for pruning rows from a table based on a time column.
//
// This struct is designed to be flexible and reusable for different tables with varying time columns and
// primary keys. It also supports maintaining referential integrity by allowing the definition of related
// tables that reference the primary keys of the main table, ensuring that any related rows are also pruned
// accordingly.
type TimeBoundPruner struct {
	prunerCommon
	TimeColumn string

	Referrers      []ReferencingRowPruner
	ExtraCondition string

	// OverridePeriodAndInterval, when non-zero, overrides the global retention period and interval to this value.
	OverridePeriodAndInterval time.Duration
}

func (tbp *TimeBoundPruner) TableName() string { return tbp.Table }
func (tbp *TimeBoundPruner) IntervalAndPeriodOverrides() time.Duration {
	return tbp.OverridePeriodAndInterval
}
func (tbp *TimeBoundPruner) referrers() []ReferencingRowPruner { return tbp.Referrers }

// Exec prunes rows from the specified table that are older than the given time threshold.
//
// If Referrers are defined, it first retrieves the primary keys of the rows to be deleted and then executes the
// corresponding DELETE statements for each [ReferencingRowPruner] before finally deleting the rows from the main
// table. If no Referrers are defined, it directly deletes rows based on the time column.
//
// The method returns only the total number of deleted rows from the main table.
func (tbp *TimeBoundPruner) Exec(ctx context.Context, db *database.DB, l *logging.Logger, olderThan types.UnixMilli, limit uint64) (uint64, error) {
	var deleted uint64
	err := retry.WithBackoff(
		ctx,
		func(ctx context.Context) (err error) {
			if len(tbp.Referrers) == 0 {
				deleted, err = exec(ctx, db, db, tbp.assembleDelete(db.DriverName(), limit), limit, olderThan)
				return
			}
			deleted, err = execCascade(ctx, db, db, tbp, limit, olderThan)
			return
		},
		retry.Retryable,
		backoff.DefaultBackoff,
		getPrunerRetrySettings(l),
	)
	return deleted, err
}

// whereClause returns the additional WHERE with an AND from WhereCondition, if set, or an empty string otherwise.
func (tbp *TimeBoundPruner) whereClause() string {
	if tbp.ExtraCondition != "" {
		return "AND " + tbp.ExtraCondition
	}

	return ""
}

// assembleSelect constructs a select statement to retrieve primary keys of the TimeBoundPruner
// based on the time column and any extra conditions.
func (tbp *TimeBoundPruner) assembleSelect(limit uint64) string {
	return fmt.Sprintf(
		`SELECT %[1]s FROM %[2]s WHERE %[3]s IS NOT NULL AND %[3]s < ? %[4]s LIMIT %[5]d`,
		tbp.PKorFK, tbp.Table, tbp.TimeColumn, tbp.whereClause(), limit)
}

// assembleDelete constructs a delete statement for the TimeBoundPruner based on the database driver, time column,
// and any extra conditions.
func (tbp *TimeBoundPruner) assembleDelete(driverName string, limit uint64) string {
	switch driverName {
	case database.MySQL:
		return fmt.Sprintf(
			`DELETE FROM %[1]s WHERE %[2]s IS NOT NULL AND %[2]s < ? %[3]s LIMIT %[4]d`,
			tbp.Table, tbp.TimeColumn, tbp.whereClause(), limit)
	case database.PostgreSQL:
		return fmt.Sprintf(`
			WITH rows AS (SELECT %[1]s FROM %[2]s WHERE %[3]s IS NOT NULL AND %[3]s < ? %[4]s LIMIT %[5]d)
			DELETE FROM %[2]s WHERE %[1]s IN (SELECT * FROM rows)`,
			tbp.PKorFK, tbp.Table, tbp.TimeColumn, tbp.whereClause(), limit)
	default:
		panic(fmt.Sprintf("invalid database type %s", driverName))
	}
}

// OrphanRowPruner defines the configuration for pruning rows from a table that are not referenced by any other table.
//
// This struct is used to identify and delete orphaned rows based on the primary key of the main table and the
// foreign key of the referencing table. Currently, it only supports a single referencing table, but it can be
// extended to support multiple referencing tables in the future if needed.
type OrphanRowPruner struct {
	prunerCommon
	ReferencingTable string // The name of the table that references this table's primary key.
	ReferencingFK    string // The name of the foreign key column in the referencing table that points to this table's primary key.
	Interval         time.Duration

	// Referrers is a list of tables that reference this table's primary key, allowing for cascading deletes to
	// maintain referential integrity.
	//
	// This must not be confused with the ReferencingTable and ReferencingFK fields, which are used to identify
	// orphaned rows based on a single referencing table. This field allows for additional tables to be pruned
	// in a cascading manner, ensuring that any related rows are also deleted.
	Referrers []ReferencingRowPruner
}

func (orp *OrphanRowPruner) TableName() string                         { return orp.Table }
func (orp *OrphanRowPruner) IntervalAndPeriodOverrides() time.Duration { return orp.Interval }
func (orp *OrphanRowPruner) referrers() []ReferencingRowPruner         { return orp.Referrers }

// Exec prunes rows from the specified table that are not referenced by any other table.
//
// It constructs a delete statement that identifies orphaned rows based on the primary key and the referencing
// table's foreign key. The method executes the delete statement within a transaction, ensuring that the operation
// is performed atomically and with the appropriate isolation level.
func (orp *OrphanRowPruner) Exec(ctx context.Context, db *database.DB, l *logging.Logger, _ types.UnixMilli, limit uint64) (uint64, error) {
	var total uint64
	for {
		var deleted uint64
		err := retry.WithBackoff(
			ctx,
			func(ctx context.Context) error {
				// A SERIALIZABLE transaction is required here to prevent a subtle race condition.
				//
				// The pruner operates on the `object` table while another component (e.g. the incident ProcessEvent tx)
				// performs upserts using the default isolation level of the respective DBMs. Without serializability our
				// transaction could observe a state in which a concurrent upsert targeting the exact same row commits
				// while we still execute the DELETE stmt but haven't committed yet. Then, when our transaction commits,
				// it would fail disastrously due to a foreign key violation, if the upsert tx managed to insert a row in
				// the `incident` table referencing the row we just deleted. Using SERIALIZABLE here causes conflicting
				// executions to abort with a serialization error, ensuring the operation is rolled back and retried so
				// the deletion predicate is re-evaluated against a consistent snapshot.
				//
				// Committing this transaction first, on the other hand, is always safe even without SERIALIZABLE
				// as the concurrent upsert would subsequently succeed and insert/update the row as intended.
				return db.ExecTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable}, func(ctx context.Context, tx *sqlx.Tx) (err error) {
					if len(orp.Referrers) == 0 {
						deleted, err = exec(ctx, db, tx, orp.assembleDelete(db.DriverName(), limit), limit)
						return
					}
					deleted, err = execCascade(ctx, db, tx, orp, limit)
					return
				})
			},
			retry.Retryable,
			backoff.DefaultBackoff,
			getPrunerRetrySettings(l),
		)
		if err != nil {
			return 0, err
		}

		total += deleted
		if deleted < limit {
			return total, nil
		}
	}
}

// assembleSelect constructs a select statement to retrieve primary keys of the OrphanRowPruner.
func (orp *OrphanRowPruner) assembleSelect(limit uint64) string {
	return fmt.Sprintf(`
		SELECT main.%[2]s
		FROM %[1]s main
			LEFT JOIN %[3]s ref ON ref.%[4]s = main.%[2]s
		WHERE ref.%[4]s IS NULL
		LIMIT %[5]d`,
		orp.Table, orp.PKorFK, orp.ReferencingTable, orp.ReferencingFK, limit)
}

// assembleDelete constructs a delete statement for the OrphanRowPruner based on the database driver and limit.
func (orp *OrphanRowPruner) assembleDelete(driverName string, limit uint64) string {
	switch driverName {
	case database.MySQL:
		// MariaDB does support JOIN based ANTI-JOINs but doesn't support LIMIT in DELETE statements with JOINs
		// on older versions, so we have to use a subquery instead. However, this might yield unexpected results
		// if the configured foreign key is nullable on the referencing table.
		return fmt.Sprintf(
			`DELETE FROM %[1]s WHERE %[2]s NOT IN (SELECT %[4]s FROM %[3]s) LIMIT %[5]d`,
			orp.Table, orp.PKorFK, orp.ReferencingTable, orp.ReferencingFK, limit)
	case database.PostgreSQL:
		return fmt.Sprintf(`
 			WITH rows AS (
				SELECT main.%[2]s
				FROM %[1]s main
					LEFT JOIN %[3]s ref ON ref.%[4]s = main.%[2]s
				WHERE ref.%[4]s IS NULL
				LIMIT %[5]d
			)
			DELETE FROM %[1]s WHERE %[2]s IN (SELECT * FROM rows)`,
			orp.Table, orp.PKorFK, orp.ReferencingTable, orp.ReferencingFK, limit)
	default:
		panic(fmt.Sprintf("invalid database type %s", driverName))
	}
}

// ReferencingRowPruner defines the configuration for pruning rows that reference the primary keys of another table.
//
// This struct is used to maintain referential integrity when pruning rows from a main table by ensuring that
// any related rows in other tables are also pruned accordingly. In other words, it allows to explicitly define
// cascading deletes for related tables without relying on database-level foreign key constraints.
//
// Usually, this is used in conjunction with [TimeBoundPruner] or [OrphanRowPruner] but never on its own,
// as it relies on the primary keys of another table to identify which rows to delete.
type ReferencingRowPruner prunerCommon

// Exec performs the pruning operation by deleting rows from the specified table that reference the given primary keys.
func (rrp ReferencingRowPruner) Exec(ctx context.Context, db *database.DB, executer database.TxOrDB, pks ...any) (uint64, error) {
	return exec(ctx, db, executer, (prunerCommon)(rrp).assembleDeleteByPK(), 0, pks...)
}

// exec executes the provided SQL query with the given arguments and returns the number of affected rows.
//
// It uses the provided database connection or transaction to execute the query, and it logs the query execution
// details using the provided database instance. This method doesn't retry the query execution, so any retry logic
// must be handled by the caller if needed.
func exec(ctx context.Context, db *database.DB, executer database.TxOrDB, query string, limit uint64, args ...any) (uint64, error) {
	stmt := query
	if len(args) > 0 {
		var err error
		stmt, args, err = sqlx.In(query, args)
		if err != nil {
			return 0, err
		}
	}

	var counter com.Counter
	defer db.Log(ctx, query, &counter).Stop()

	for {
		res, err := executer.ExecContext(ctx, db.Rebind(stmt), args...)
		if err != nil {
			return 0, database.CantPerformQuery(err, query)
		}

		n, err := res.RowsAffected()
		if err != nil {
			return 0, err
		}

		var affected uint64
		if n > 0 {
			affected = uint64(n)
			counter.Add(affected)
		}

		// If limit is set to 0, it means we are deleting matching rows by primary keys, so the limit check can be skipped.
		if limit == 0 || affected < limit {
			break
		}
	}

	return counter.Total(), nil
}

// execCascade executes the pruning operation for a pruner that has referrers, ensuring that all related rows
// are deleted in a cascading manner.
//
// It repeatedly selects up to `limit` primary keys via pruner.assembleSelect(limit), deletes matching rows from
// each [ReferencingRowPruner], and then deletes the rows from the main table by primary key. If the SELECT statement
// requires any bind arguments (e.g. an "olderThan" threshold), they must be provided via args.
//
// The method returns the total number of deleted rows from the main table.
func execCascade[
	P interface {
		referrers() []ReferencingRowPruner
		assembleSelect(uint64) string
		assembleDeleteByPK() string
	},
](
	ctx context.Context,
	db *database.DB,
	executer database.TxOrDB,
	pruner P,
	limit uint64,
	args ...any,
) (uint64, error) {
	selectStmt := pruner.assembleSelect(limit)
	deleteStmt := pruner.assembleDeleteByPK()

	var total uint64
	for {
		var rows []any
		if err := sqlx.SelectContext(ctx, executer, &rows, db.Rebind(selectStmt), args...); err != nil {
			return 0, database.CantPerformQuery(err, selectStmt)
		}

		if len(rows) == 0 {
			return total, nil
		}

		for _, referrer := range pruner.referrers() {
			if _, err := referrer.Exec(ctx, db, executer, rows...); err != nil {
				return 0, err
			}
		}

		if affected, err := exec(ctx, db, executer, deleteStmt, 0, rows...); err != nil {
			return 0, err
		} else {
			total += affected
		}

		// If the executer is a transaction, we can return early after one iteration to avoid holding
		// the transaction open for too long. The caller can then decide to commit and restart the pruning
		// process in a new transaction if needed.
		if _, ok := executer.(*sqlx.Tx); ok {
			return total, nil
		}
	}
}

// getPrunerRetrySettings returns the retry settings for a pruner, including the timeout and logging
// behavior for retryable errors and successful retries.
func getPrunerRetrySettings(l *logging.Logger) retry.Settings {
	return retry.Settings{
		Timeout: retry.DefaultTimeout,
		OnRetryableError: func(elapsed time.Duration, attempt uint64, err, lastErr error) {
			// The transaction above is expected to fail with a serialization error occasionally, so we don't
			// want to spam the logs with the same error message over and over again.
			if lastErr == nil || err.Error() != lastErr.Error() {
				l.Infow("Cannot execute query. Retrying",
					zap.Uint64("attempt", attempt),
					zap.Duration("elapsed", elapsed),
					zap.String("error", err.Error()))

				// The stack traces are only logged at debug level to avoid cluttering the logs with expected errors.
				l.Debugw("Query failed", zap.Error(err))
			}
		},
		OnSuccess: func(elapsed time.Duration, attempt uint64, lastErr error) {
			if attempt > 1 {
				l.Infow("Query retried successfully after error",
					zap.Uint64("attempt", attempt),
					zap.Duration("elapsed", elapsed),
					zap.String("last_error", lastErr.Error()))

				l.Debugw("Query retried successfully", zap.Error(lastErr)) // Same as above!
			}
		},
	}
}
