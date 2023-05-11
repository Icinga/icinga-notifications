package object

import (
	"context"
	"fmt"
	"github.com/icinga/icingadb/pkg/icingadb"
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

// LoadFromDB loads objects from the database matching the given id
// Returns error on database failure.
func LoadFromDB(ctx context.Context, db *icingadb.DB, id types.Binary) (*Object, error) {
	objectRow := &ObjectRow{ID: id}
	err := db.QueryRowxContext(ctx, db.Rebind(db.BuildSelectStmt(objectRow, objectRow)+` WHERE "id" = ?`), objectRow.ID).StructScan(objectRow)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch object: %w", err)
	}

	tags := map[string]string{"host": objectRow.Host}
	if objectRow.Service.Valid {
		tags["service"] = objectRow.Service.String
	}

	var extraTagRows []*ExtraTagRow
	err = db.SelectContext(
		ctx, &extraTagRows,
		db.Rebind(db.BuildSelectStmt(&ExtraTagRow{}, &ExtraTagRow{})+` WHERE "object_id" = ?`), id,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch object extra tags: %w", err)
	}

	extraTags := map[string]string{}
	for _, extraTag := range extraTagRows {
		extraTags[extraTag.Tag] = extraTag.Value
	}

	obj := &Object{db: db, ID: id, Tags: tags, ExtraTags: extraTags}

	cacheMu.Lock()
	defer cacheMu.Unlock()

	cache[id.String()] = obj

	return obj, nil
}
