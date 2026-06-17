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
	"github.com/jmoiron/sqlx"
)

type Object struct {
	ID       types.Binary `db:"id"`
	SourceID int64        `db:"source_id"`
	Name     string       `db:"name"`
	URL      types.String `db:"url"`

	Tags map[string]string `db:"-"`

	db *database.DB
}

// New creates a new object from the given event.
func New(db *database.DB, ev *event.Event) *Object {
	return &Object{
		SourceID: ev.SourceId,
		Name:     ev.Name,
		db:       db,
		URL:      types.MakeString(ev.URL, types.TransformEmptyStringToNull),
		Tags:     ev.Tags,
	}
}

// GetFromCache fetches an object from the global object cache store matching the given ID.
// Returns nil if it's not in the cache.
func GetFromCache(id types.Binary) *Object {
	cacheMu.Lock()
	defer cacheMu.Unlock()

	return cache[id.String()]
}

// ClearCache clears the global object cache store.
// Note, this is only used for unit tests not to run into "can't cache already cached object" error.
func ClearCache() {
	cacheMu.Lock()
	defer cacheMu.Unlock()

	cache = make(map[string]*Object)
}

// SyncFromEvent syncs the current object with the given event.
//
// It updates all relevant fields and tags of the object and saves it to the database within the given transaction.
// The current object is updated with the new values only if the database update is successful, otherwise it remains
// unchanged.
//
// Returns error on any database failure.
func (o *Object) SyncFromEvent(ctx context.Context, tx *sqlx.Tx, ev *event.Event) error {
	newObject := Object{ID: o.ID, SourceID: o.SourceID, Name: ev.Name}
	newObject.URL = types.MakeString(ev.URL, types.TransformEmptyStringToNull)

	stmt, _ := o.db.BuildUpsertStmt(o)
	if _, err := tx.NamedExecContext(ctx, stmt, &newObject); err != nil {
		return fmt.Errorf("failed to insert object: %w", err)
	}

	stmt, _ = o.db.BuildUpsertStmt(new(IdTagRow))
	if _, err := tx.NamedExecContext(ctx, stmt, mapToIdTagRows(newObject.ID, ev.Tags)); err != nil {
		return fmt.Errorf("failed to upsert object id tags: %w", err)
	}

	o.Name = newObject.Name
	o.URL = newObject.URL
	return nil
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
