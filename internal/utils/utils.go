package utils

import (
	"database/sql"
	"fmt"
	"github.com/icinga/icingadb/pkg/driver"
	"github.com/icinga/icingadb/pkg/icingadb"
	"github.com/icinga/icingadb/pkg/types"
	"github.com/icinga/icingadb/pkg/utils"
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
func InsertAndFetchId(db *icingadb.DB, stmt string, args any) (int64, error) {
	var lastInsertId int64
	if db.DriverName() == driver.PostgreSQL {
		preparedStmt, err := db.PrepareNamed(stmt + " RETURNING id")
		if err != nil {
			return 0, err
		}
		defer preparedStmt.Close()

		err = preparedStmt.Get(&lastInsertId, args)
		if err != nil {
			return 0, fmt.Errorf("failed to insert entry for type %T: %s", args, err)
		}
	} else {
		result, err := db.NamedExec(stmt, args)
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
