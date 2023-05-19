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
