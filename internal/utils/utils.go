package utils

import (
	"context"
	"fmt"
	"github.com/icinga/icinga-go-library/database"
	"github.com/jmoiron/sqlx"
	"github.com/pkg/errors"
)

// ExecAndApply applies the provided restoreFunc callback for each successfully retrieved row of the specified type.
// Returns error on any database failure or fails to acquire the table semaphore.
func ExecAndApply[Row any](ctx context.Context, db *database.DB, stmt string, args []interface{}, restoreFunc func(*Row)) error {
	table := database.TableName(new(Row))
	sem := db.GetSemaphoreForTable(table)
	if err := sem.Acquire(ctx, 1); err != nil {
		return errors.Wrapf(err, "cannot acquire semaphore for table %q", table)
	}
	defer sem.Release(1)

	rows, err := db.QueryxContext(ctx, db.Rebind(stmt), args...)
	if err != nil {
		return err
	}
	// In case the records in the loop below are successfully traversed, rows is automatically closed and an
	// error is returned (if any), making this rows#Close() call a no-op. Escaping from this function unexpectedly
	// means we have a more serious problem, so in either case just discard the error here.
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		row := new(Row)
		if err = rows.StructScan(row); err != nil {
			return err
		}

		restoreFunc(row)
	}

	return rows.Err()
}

// ForEachRow applies the provided restoreFunc callback for each successfully retrieved row of the specified type.
// It will bulk SELECT the data from the database scoped to the specified ids and scans into the provided Row type.
// Returns error on any database failure or fails to acquire the table semaphore.
func ForEachRow[Row, Id any](ctx context.Context, db *database.DB, idColumn string, ids []Id, restoreFunc func(*Row)) error {
	subject := new(Row)
	query := fmt.Sprintf("%s WHERE %q IN (?)", db.BuildSelectStmt(subject, subject), idColumn)
	stmt, args, err := sqlx.In(query, ids)
	if err != nil {
		return errors.Wrapf(err, "cannot build placeholders for %q", query)
	}

	return ExecAndApply(ctx, db, stmt, args, restoreFunc)
}
