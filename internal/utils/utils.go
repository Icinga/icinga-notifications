package utils

import (
	"cmp"
	"context"
	"fmt"
	"github.com/icinga/icinga-go-library/database"
	"github.com/jmoiron/sqlx"
	"github.com/pkg/errors"
	"reflect"
	"strings"
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

	//nolint:sqlclosecheck // False positive, does not detect deferred close: https://github.com/ryanrolds/sqlclosecheck/issues/43
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

// PrefixWithJSONPathRootSelector ensures that the provided JSONPath expression starts with the root selector "$.".
//
// If the provided path already starts with "$.", it is returned unchanged.
// Otherwise, the root selector is prefixed to the path.
func PrefixWithJSONPathRootSelector(path string) string {
	if !strings.HasPrefix(path, "$.") {
		return "$." + path
	}
	return path
}

// CompareAny compares two values of any type and returns an integer indicating their order (1 if a > b, -1 if a < b, 0 if equal).
func CompareAny(a, b any) (int, error) {
	atype := reflect.TypeOf(a)
	btype := reflect.TypeOf(b)

	switch atype.Kind() {
	case reflect.String:
		if btype.ConvertibleTo(atype) {
			av := fmt.Sprint(a)
			bv := fmt.Sprint(b)
			if len(av) > len(bv) {
				return 1, nil // a is greater than b
			}
			if len(av) < len(bv) {
				return -1, nil // a is less than b
			}
			// Both strings have the same length, compare them lexicographically.
			return strings.Compare(av, bv), nil
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if btype.ConvertibleTo(atype) {
			return cmp.Compare(reflect.ValueOf(a).Int(), reflect.ValueOf(b).Convert(atype).Int()), nil
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if atype.ConvertibleTo(btype) {
			return cmp.Compare(reflect.ValueOf(a).Uint(), reflect.ValueOf(b).Convert(atype).Uint()), nil
		}
	case reflect.Float32, reflect.Float64:
		if atype.ConvertibleTo(btype) {
			return cmp.Compare(reflect.ValueOf(a).Float(), reflect.ValueOf(b).Convert(atype).Float()), nil
		}
	}
	return 0, errors.Errorf("cannot compare types %s and %s", atype.Kind(), btype.Kind())
}
