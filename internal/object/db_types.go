package object

import (
	"github.com/icinga/icingadb/pkg/types"
)

// ExtraTagRow represents a single database object extra tag like `hostgroup/foo: null`.
type ExtraTagRow struct {
	ObjectId types.Binary `db:"object_id"`
	Tag      string       `db:"tag"`
	Value    string       `db:"value"`
}

// TableName implements the contracts.TableNamer interface.
func (e *ExtraTagRow) TableName() string {
	return "object_extra_tag"
}

type ObjectRow struct {
	ID       types.Binary `db:"id"`
	SourceID int64        `db:"source_id"`
	Name     string       `db:"name"`
	Host     string       `db:"host"`
	Service  types.String `db:"service"`
	URL      types.String `db:"url"`
}

// TableName implements the contracts.TableNamer interface.
func (or *ObjectRow) TableName() string {
	return "object"
}

// Upsert implements the contracts.Upserter interface.
func (or *ObjectRow) Upsert() interface{} {
	return struct {
		Name string       `db:"name"`
		URL  types.String `db:"url"`
	}{}
}
