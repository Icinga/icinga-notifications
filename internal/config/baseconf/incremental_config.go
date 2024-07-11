package baseconf

import (
	"github.com/icinga/icinga-go-library/types"
)

// IncrementalDbEntry contains the changed_at and deleted columns as struct fields.
//
// This type partially implements config.IncrementalConfigurable with GetChangedAt and IsDeleted. Thus, it can be
// embedded in other types with the _`db:",inline"`_ struct tag. However, most structs might want to embed the
// IncrementalPkDbEntry struct instead.
type IncrementalDbEntry struct {
	ChangedAt types.UnixMilli `db:"changed_at"`
	Deleted   types.Bool      `db:"deleted"`
}

// GetChangedAt returns the changed_at value of this entry from the database.
//
// It is required by the config.IncrementalConfigurable interface.
func (i IncrementalDbEntry) GetChangedAt() types.UnixMilli {
	return i.ChangedAt
}

// IsDeleted indicates if this entry is marked as deleted in the database.
//
// It is required by the config.IncrementalConfigurable interface.
func (i IncrementalDbEntry) IsDeleted() bool {
	return i.Deleted.Valid && i.Deleted.Bool
}

// IncrementalPkDbEntry implements a single primary key named id of a generic type next to IncrementalDbEntry.
//
// This type embeds IncrementalDbEntry and adds a single column/value id field, getting one step closer to implementing
// the config.IncrementalConfigurable interface. Thus, it needs to be embedded with the _`db:",inline"`_ struct tag.
type IncrementalPkDbEntry[PK comparable] struct {
	IncrementalDbEntry `db:",inline"`
	ID                 PK `db:"id"`
}

// GetPrimaryKey returns the id of this entry from the database.
//
// It is required by the config.IncrementalConfigurable interface.
func (i IncrementalPkDbEntry[PK]) GetPrimaryKey() PK {
	return i.ID
}
