package utils

import (
	"database/sql"
	"github.com/icinga/icinga-go-library/types"
)

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
