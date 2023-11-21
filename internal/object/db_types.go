package object

import (
	"github.com/icinga/icingadb/pkg/types"
)

// TagRow is a base type for IdTagRow and ExtraTagRow
type TagRow struct {
	ObjectId types.Binary `db:"object_id"`
	Tag      string       `db:"tag"`
	Value    string       `db:"value"`
}

// ExtraTagRow represents a single database object extra tag like `hostgroup/foo: null`.
type ExtraTagRow TagRow

// TableName implements the contracts.TableNamer interface.
func (e *ExtraTagRow) TableName() string {
	return "object_extra_tag"
}

// IdTagRow represents a single database object id tag.
type IdTagRow TagRow

// TableName implements the contracts.TableNamer interface.
func (e *IdTagRow) TableName() string {
	return "object_id_tag"
}

// Upsert implements the contracts.Upserter interface.
func (o *Object) Upsert() interface{} {
	return struct {
		Name string       `db:"name"`
		URL  types.String `db:"url"`
	}{}
}
