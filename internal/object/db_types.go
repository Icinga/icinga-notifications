package object

import "github.com/icinga/icinga-go-library/types"

// IdTagRow represents a single database object id tag.
type IdTagRow struct {
	ObjectId types.Binary `db:"object_id"`
	Tag      string       `db:"tag"`
	Value    string       `db:"value"`
}

// TableName implements the contracts.TableNamer interface.
func (e *IdTagRow) TableName() string {
	return "object_id_tag"
}

// Upsert implements the contracts.Upserter interface.
func (o *Object) Upsert() interface{} {
	return struct {
		Name       string       `db:"name"`
		URL        types.String `db:"url"`
		MuteReason types.String `db:"mute_reason"`
	}{}
}
