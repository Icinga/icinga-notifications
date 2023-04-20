package object

import (
	"github.com/icinga/icingadb/pkg/types"
)

// ExtraTagRow represents a single database object extra tag like `hostgroup/foo: null`.
type ExtraTagRow struct {
	ObjectId types.Binary `db:"object_id"`
	SourceId int64        `db:"source_id"`
	Tag      string       `db:"tag"`
	Value    string       `db:"value"`
}

// TableName implements the contracts.TableNamer interface.
func (e *ExtraTagRow) TableName() string {
	return "object_extra_tag"
}

type ObjectRow struct {
	ID      types.Binary `db:"id"`
	Host    string       `db:"host"`
	Service types.String `db:"service"`
}

// TableName implements the contracts.TableNamer interface.
func (d *ObjectRow) TableName() string {
	return "object"
}
