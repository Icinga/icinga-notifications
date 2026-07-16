package object

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/icinga/icinga-go-library/database"
	"github.com/icinga/icinga-go-library/types"
	"github.com/icinga/icinga-notifications/internal/event"
	"github.com/icinga/icinga-notifications/internal/utils"
	"github.com/jmoiron/sqlx"
)

type Object struct {
	ID       types.Binary `db:"id"`
	SourceID int64        `db:"source_id"`
	Name     string       `db:"name"`
	URL      types.String `db:"url"`

	Tags map[string]string `db:"-"`
}

// New creates a new object from the given event.
func New(ev *event.Event) *Object {
	return &Object{
		ID:       ID(ev.SourceId, ev.Tags),
		SourceID: ev.SourceId,
		Name:     ev.Name,
		URL:      types.MakeString(ev.URL, types.TransformEmptyStringToNull),
		Tags:     ev.Tags,
	}
}

// Get the Object for the requested ID from the database.
func Get(ctx context.Context, db *database.DB, id types.Binary) (*Object, error) {
	o := new(Object)
	stmt := db.Rebind(db.BuildSelectStmt(o, o) + ` WHERE "id" = ?`)
	err := db.GetContext(ctx, o, stmt, id)
	if err != nil {
		return nil, fmt.Errorf("cannot get object %q from database: %w", id, err)
	}

	o.Tags = make(map[string]string)
	err = utils.ForEachRow(ctx, db, db, "object_id", []types.Binary{id}, func(ir *IdTagRow) {
		o.Tags[ir.Tag] = ir.Value
	})
	if err != nil {
		return nil, fmt.Errorf("cannot fetch object id tags from database: %w", err)
	}

	return o, nil
}

// GetAll fetches the Objects for the requested IDs from the database, keyed by their string ID.
//
// IDs without a matching object are omitted from the result.
func GetAll(ctx context.Context, db *database.DB, ids []types.Binary) (map[string]*Object, error) {
	if len(ids) == 0 {
		return make(map[string]*Object), nil
	}

	objects := make(map[string]*Object, len(ids))
	err := utils.ForEachRow(ctx, db, db, "id", ids, func(o *Object) {
		o.Tags = make(map[string]string)
		objects[o.ID.String()] = o
	})
	if err != nil {
		return nil, fmt.Errorf("cannot fetch objects from database: %w", err)
	}

	err = utils.ForEachRow(ctx, db, db, "object_id", ids, func(ir *IdTagRow) {
		if o, ok := objects[ir.ObjectId.String()]; ok {
			o.Tags[ir.Tag] = ir.Value
		}
	})
	if err != nil {
		return nil, fmt.Errorf("cannot fetch object id tags from database: %w", err)
	}

	return objects, nil
}

// SyncFromEvent syncs the object in the database with the given event.
//
// It inserts or updates the object and its tags in the database based on the provided event.
// The function uses the specified tx to ensure that both the object and its tags are updated atomically.
//
// Returns error on any database failure, otherwise returns the updated object.
func SyncFromEvent(ctx context.Context, db *database.DB, tx *sqlx.Tx, ev *event.Event) (*Object, error) {
	o := New(ev)
	stmt, _ := db.BuildUpsertStmt(o)
	if _, err := tx.NamedExecContext(ctx, stmt, o); err != nil {
		return nil, fmt.Errorf("failed to insert object: %w", err)
	}

	stmt, _ = db.BuildUpsertStmt(new(IdTagRow))
	if _, err := tx.NamedExecContext(ctx, stmt, mapToIdTagRows(o.ID, ev.Tags)); err != nil {
		return nil, fmt.Errorf("failed to upsert object id tags: %w", err)
	}

	return o, nil
}

func (o *Object) DisplayName() string {
	if o.Name != "" {
		return o.Name
	}

	j, err := json.Marshal(o.Tags)
	if err != nil {
		panic(err)
	}
	return string(j)
}

func (o *Object) String() string {
	var b bytes.Buffer
	_, _ = fmt.Fprintf(&b, "Object:\n")
	_, _ = fmt.Fprintf(&b, "  ID: %s\n", hex.EncodeToString(o.ID[:]))
	for tag, value := range o.Tags {
		_, _ = fmt.Fprintf(&b, "    %q", tag)
		if value != "" {
			_, _ = fmt.Fprintf(&b, " = %q", value)
		}
		_, _ = fmt.Fprintf(&b, "\n")
	}

	_, _ = fmt.Fprintf(&b, "    Source %d:\n", o.SourceID)
	_, _ = fmt.Fprintf(&b, "    Name: %q\n", o.Name)
	_, _ = fmt.Fprintf(&b, "    URL: %q\n", o.URL.String)

	return b.String()
}

// ID generates a stable identifier based on a source ID and tags.
//
// TODO: the return value of this function must be stable like forever
func ID(source int64, tags map[string]string) types.Binary {
	h := sha256.New()

	if source < 0 {
		panic(fmt.Sprintf("source value %d is negative", source))
	}

	sourceBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(sourceBytes, uint64(source))
	h.Write(sourceBytes)

	type KV struct {
		K, V string
	}

	sortedTags := make([]KV, len(tags))
	for k, v := range tags {
		sortedTags = append(sortedTags, KV{k, v})
	}
	sort.Slice(sortedTags, func(i, j int) bool { return sortedTags[i].K < sortedTags[j].K })

	for _, kv := range sortedTags {
		h.Write([]byte(kv.K))
		h.Write([]byte{0})
		h.Write([]byte(kv.V))
		h.Write([]byte{0})
	}

	return h.Sum(nil)
}

// mapToIdTagRows transforms the object tags map to a slice of TagRow struct.
func mapToIdTagRows(objectId types.Binary, tags map[string]string) []*IdTagRow {
	var tagRows []*IdTagRow
	for key, val := range tags {
		tagRows = append(tagRows, &IdTagRow{
			ObjectId: objectId,
			Tag:      key,
			Value:    val,
		})
	}

	return tagRows
}
