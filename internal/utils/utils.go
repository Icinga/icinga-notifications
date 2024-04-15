package utils

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/icinga/icingadb/pkg/driver"
	"github.com/icinga/icingadb/pkg/icingadb"
	"github.com/icinga/icingadb/pkg/types"
	"github.com/icinga/icingadb/pkg/utils"
	"github.com/jmoiron/sqlx"
	"github.com/pkg/errors"
	"strings"
)

// BuildInsertStmtWithout builds an insert stmt without the provided column.
func BuildInsertStmtWithout(db *icingadb.DB, into interface{}, withoutColumn string) string {
	columns := db.BuildColumns(into)
	for i, column := range columns {
		if column == withoutColumn {
			// Event id is auto incremented, so just erase it from our insert columns
			columns = append(columns[:i], columns[i+1:]...)
			break
		}
	}

	return fmt.Sprintf(
		`INSERT INTO "%s" ("%s") VALUES (%s)`,
		utils.TableName(into), strings.Join(columns, `", "`),
		fmt.Sprintf(":%s", strings.Join(columns, ", :")),
	)
}

// RunInTx allows running a function in a database transaction without requiring manual transaction handling.
//
// A new transaction is started on db which is then passed to fn. After fn returns, the transaction is
// committed unless an error was returned. If fn returns an error, that error is returned, otherwise an
// error is returned if a database operation fails.
func RunInTx(ctx context.Context, db *icingadb.DB, fn func(tx *sqlx.Tx) error) error {
	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	err = fn(tx)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// InsertAndFetchId executes the given query and fetches the last inserted ID.
func InsertAndFetchId(ctx context.Context, tx *sqlx.Tx, stmt string, args any) (int64, error) {
	var lastInsertId int64
	if tx.DriverName() == driver.PostgreSQL {
		preparedStmt, err := tx.PrepareNamedContext(ctx, stmt+" RETURNING id")
		if err != nil {
			return 0, err
		}
		defer func() { _ = preparedStmt.Close() }()

		err = preparedStmt.Get(&lastInsertId, args)
		if err != nil {
			return 0, fmt.Errorf("failed to insert entry for type %T: %s", args, err)
		}
	} else {
		result, err := tx.NamedExecContext(ctx, stmt, args)
		if err != nil {
			return 0, fmt.Errorf("failed to insert entry for type %T: %s", args, err)
		}

		lastInsertId, err = result.LastInsertId()
		if err != nil {
			return 0, fmt.Errorf("failed to fetch last insert id for type %T: %s", args, err)
		}
	}

	return lastInsertId, nil
}

// ForEachRow applies the provided restoreFunc callback for each successfully retrieved row of the specified type.
// It will bulk SELECT the data from the database scoped to the specified ids and scans into the provided Row type.
// Returns error on any database failure or fails to acquire the table semaphore.
func ForEachRow[Row, Id any](ctx context.Context, db *icingadb.DB, idColumn string, ids []Id, restoreFunc func(*Row)) error {
	subject := new(Row)
	table := utils.TableName(subject)
	sem := db.GetSemaphoreForTable(table)
	if err := sem.Acquire(ctx, 1); err != nil {
		return errors.Wrapf(err, "cannot acquire semaphore for table %q", table)
	}

	query := fmt.Sprintf("%s WHERE %q IN (?)", db.BuildSelectStmt(subject, subject), idColumn)
	stmt, args, err := sqlx.In(query, ids)
	if err != nil {
		return errors.Wrapf(err, "cannot build placeholders for %q", query)
	}

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

// ToDBString transforms the given string to types.String.
func ToDBString(value string) types.String {
	str := types.String{NullString: sql.NullString{String: value}}
	if value != "" {
		str.Valid = true
	}

	return str
}

// ToDBInt transforms the given value to types.Int.
func ToDBInt(value int64) types.Int {
	val := types.Int{NullInt64: sql.NullInt64{Int64: value}}
	if value != 0 {
		val.Valid = true
	}

	return val
}

func RemoveIf[T any](slice []T, pred func(T) bool) []T {
	n := len(slice)

	for i := 0; i < n; i++ {
		for i < n && pred(slice[i]) {
			n--
			slice[i], slice[n] = slice[n], slice[i]
		}
	}

	return slice[:n]
}

func RemoveNils[T any](slice []*T) []*T {
	return RemoveIf(slice, func(ptr *T) bool {
		return ptr == nil
	})
}
