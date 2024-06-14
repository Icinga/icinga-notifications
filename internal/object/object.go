package object

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/icinga/icinga-go-library/database"
	"github.com/icinga/icinga-go-library/types"
	"github.com/icinga/icinga-notifications/internal/event"
	"github.com/icinga/icinga-notifications/internal/utils"
	"github.com/pkg/errors"
	"regexp"
	"sort"
	"strings"
)

type Object struct {
	ID         types.Binary `db:"id"`
	SourceID   int64        `db:"source_id"`
	Name       string       `db:"name"`
	URL        types.String `db:"url"`
	MuteReason types.String `db:"mute_reason"`

	Tags      map[string]string `db:"-"`
	ExtraTags map[string]string `db:"-"`

	db *database.DB
}

// New creates a new object from the given event.
func New(db *database.DB, ev *event.Event) *Object {
	obj := &Object{
		SourceID:  ev.SourceId,
		Name:      ev.Name,
		db:        db,
		URL:       utils.ToDBString(ev.URL),
		Tags:      ev.Tags,
		ExtraTags: ev.ExtraTags,
	}
	if ev.Mute.Valid && ev.Mute.Bool {
		obj.MuteReason = types.String{NullString: sql.NullString{String: ev.MuteReason, Valid: true}}
	}

	return obj
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

// RestoreObjects restores all objects and their (extra)tags matching the given IDs from the database.
// Returns error on any database failures and panics when trying to cache an object that's already in the cache store.
func RestoreObjects(ctx context.Context, db *database.DB, ids []types.Binary) error {
	objects := map[string]*Object{}
	err := utils.ForEachRow[Object](ctx, db, "id", ids, func(o *Object) {
		o.db = db
		o.Tags = map[string]string{}
		o.ExtraTags = map[string]string{}

		objects[o.ID.String()] = o
	})
	if err != nil {
		return errors.Wrap(err, "cannot restore objects")
	}

	// Restore object ID tags matching the given object ids
	err = utils.ForEachRow[IdTagRow](ctx, db, "object_id", ids, func(ir *IdTagRow) {
		objects[ir.ObjectId.String()].Tags[ir.Tag] = ir.Value
	})
	if err != nil {
		return errors.Wrap(err, "cannot restore objects ID tags")
	}

	// Restore object extra tags matching the given object ids
	err = utils.ForEachRow[ExtraTagRow](ctx, db, "object_id", ids, func(et *ExtraTagRow) {
		objects[et.ObjectId.String()].ExtraTags[et.Tag] = et.Value
	})
	if err != nil {
		return errors.Wrap(err, "cannot restore objects extra tags")
	}

	addObjectsToCache(objects)

	return nil
}

// FromEvent creates an object from the provided event tags if it's not in the cache
// and syncs all object related types with the database.
// Returns error on any database failure
func FromEvent(ctx context.Context, db *database.DB, ev *event.Event) (*Object, error) {
	id := ID(ev.SourceId, ev.Tags)

	cacheMu.Lock()
	defer cacheMu.Unlock()

	newObject := new(Object)
	object, objectExists := cache[id.String()]
	if !objectExists {
		newObject = New(db, ev)
		newObject.ID = id
	} else {
		*newObject = *object

		newObject.ExtraTags = ev.ExtraTags
		newObject.Name = ev.Name
		newObject.URL = utils.ToDBString(ev.URL)
		if ev.Mute.Valid {
			if ev.Mute.Bool {
				newObject.MuteReason = utils.ToDBString(ev.MuteReason)
			} else {
				// The ongoing event unmutes the object, so reset the mute reason to null.
				newObject.MuteReason = types.String{}
			}
		}
	}

	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to start object database transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, _ := db.BuildUpsertStmt(&Object{})
	_, err = tx.NamedExecContext(ctx, stmt, newObject)
	if err != nil {
		return nil, fmt.Errorf("failed to insert object: %w", err)
	}

	stmt, _ = db.BuildUpsertStmt(&IdTagRow{})
	_, err = tx.NamedExecContext(ctx, stmt, mapToTagRows(newObject.ID, ev.Tags))
	if err != nil {
		return nil, fmt.Errorf("failed to upsert object id tags: %w", err)
	}

	extraTag := &ExtraTagRow{ObjectId: newObject.ID}
	_, err = tx.NamedExecContext(ctx, `DELETE FROM "object_extra_tag" WHERE "object_id" = :object_id`, extraTag)
	if err != nil {
		return nil, fmt.Errorf("failed to delete object extra tags: %w", err)
	}

	if len(ev.ExtraTags) > 0 {
		stmt, _ := db.BuildInsertStmt(extraTag)
		_, err = tx.NamedExecContext(ctx, stmt, mapToTagRows(newObject.ID, ev.ExtraTags))
		if err != nil {
			return nil, fmt.Errorf("failed to insert object extra tags: %w", err)
		}
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("can't commit object database transaction: %w", err)
	}

	if !objectExists {
		cache[id.String()] = newObject
		return newObject, nil
	}

	*object = *newObject

	return object, nil
}

// IsMuted returns whether the current object is muted by its source.
func (o *Object) IsMuted() bool {
	return o.MuteReason.Valid
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
	_, _ = fmt.Fprintf(&b, "    Extra Tags:\n")

	for tag, value := range o.ExtraTags {
		_, _ = fmt.Fprintf(&b, "        %q", tag)
		if value != "" {
			_, _ = fmt.Fprintf(&b, " = %q", value)
		}
		_, _ = fmt.Fprintf(&b, "\n")
	}

	return b.String()
}

func (o *Object) EvalEqual(key string, value string) (bool, error) {
	tagVal, ok := o.Tags[key]
	if ok && tagVal == value {
		return true, nil
	}

	tagVal, ok = o.ExtraTags[key]
	if ok && tagVal == value {
		return true, nil
	}

	return false, nil
}

// EvalLike returns true when the objects tag/value matches the filter.Conditional value.
func (o *Object) EvalLike(key string, value string) (bool, error) {
	segments := strings.Split(value, "*")
	builder := &strings.Builder{}
	for _, segment := range segments {
		if segment == "" {
			builder.WriteString(".*")
		}

		builder.WriteString(regexp.QuoteMeta(segment))
	}

	regex := regexp.MustCompile("^" + builder.String() + "$")
	tagVal, ok := o.Tags[key]
	if ok && regex.MatchString(tagVal) {
		return true, nil
	}

	tagVal, ok = o.ExtraTags[key]
	if ok && regex.MatchString(tagVal) {
		return true, nil
	}

	return false, nil
}

func (o *Object) EvalLess(key string, value string) (bool, error) {
	tagVal, ok := o.Tags[key]
	if ok && tagVal < value {
		return true, nil
	}

	tagVal, ok = o.ExtraTags[key]
	if ok && tagVal < value {
		return true, nil
	}

	return false, nil
}

func (o *Object) EvalLessOrEqual(key string, value string) (bool, error) {
	tagVal, ok := o.Tags[key]
	if ok && tagVal <= value {
		return true, nil
	}

	tagVal, ok = o.ExtraTags[key]
	if ok && tagVal <= value {
		return true, nil
	}

	return false, nil
}

func (o *Object) EvalExists(key string) bool {
	_, ok := o.Tags[key]
	if ok {
		return true
	}

	_, ok = o.ExtraTags[key]
	return ok
}

// TODO: the return value of this function must be stable like forever
func ID(source int64, tags map[string]string) types.Binary {
	h := sha256.New()

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

// mapToTagRows transforms the object (extra) tags map to a slice of TagRow struct.
func mapToTagRows(objectId types.Binary, extraTags map[string]string) []*TagRow {
	var tagRows []*TagRow
	for key, val := range extraTags {
		tagRows = append(tagRows, &TagRow{
			ObjectId: objectId,
			Tag:      key,
			Value:    val,
		})
	}

	return tagRows
}
