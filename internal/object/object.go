package object

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/icinga/icinga-notifications/internal/event"
	"github.com/icinga/icinga-notifications/internal/utils"
	"github.com/icinga/icingadb/pkg/icingadb"
	"github.com/icinga/icingadb/pkg/types"
	"regexp"
	"sort"
	"strings"
	"sync"
)

var (
	cache   = make(map[string]*Object)
	cacheMu sync.Mutex
)

type Object struct {
	ID       types.Binary
	SourceId int64
	Name     string
	Tags     map[string]string
	URL      string

	ExtraTags map[string]string

	db *icingadb.DB
}

func NewObject(db *icingadb.DB, ev *event.Event) *Object {
	return &Object{
		SourceId:  ev.SourceId,
		Name:      ev.Name,
		db:        db,
		URL:       ev.URL,
		Tags:      ev.Tags,
		ExtraTags: ev.ExtraTags,
	}
}

// FromEvent creates an object from the provided event tags if it's not in the cache
// and syncs all object related types with the database.
// Returns error on any database failure
func FromEvent(ctx context.Context, db *icingadb.DB, ev *event.Event) (*Object, error) {
	id := ID(ev.SourceId, ev.Tags)

	cacheMu.Lock()
	defer cacheMu.Unlock()

	object, ok := cache[id.String()]
	if !ok {
		object = NewObject(db, ev)
		object.ID = id
		cache[id.String()] = object
	}

	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to start object database transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	dbObj := &ObjectRow{
		ID:       object.ID,
		SourceID: ev.SourceId,
		Name:     ev.Name,
		Host:     ev.Tags["host"],
		URL:      utils.ToDBString(ev.URL),
	}

	if service, ok := ev.Tags["service"]; ok {
		dbObj.Service = utils.ToDBString(service)
	}

	stmt, _ := object.db.BuildUpsertStmt(&ObjectRow{})
	_, err = tx.NamedExecContext(ctx, stmt, dbObj)
	if err != nil {
		return nil, fmt.Errorf("failed to insert object: %w", err)
	}

	extraTag := &ExtraTagRow{ObjectId: object.ID}
	_, err = tx.NamedExecContext(ctx, `DELETE FROM "object_extra_tag" WHERE "object_id" = :object_id`, extraTag)
	if err != nil {
		return nil, err
	}

	if len(ev.ExtraTags) > 0 {
		stmt, _ := object.db.BuildInsertStmt(extraTag)
		_, err = tx.NamedExecContext(ctx, stmt, mapToExtraTagRows(object.ID, ev.ExtraTags))
		if err != nil {
			return nil, err
		}
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("can't commit object database transaction: %w", err)
	}

	object.ExtraTags = ev.ExtraTags
	object.Tags = ev.Tags
	object.Name = ev.Name
	object.URL = ev.URL

	return object, nil
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

	_, _ = fmt.Fprintf(&b, "    Source %d:\n", o.SourceId)
	_, _ = fmt.Fprintf(&b, "    Name: %q\n", o.Name)
	_, _ = fmt.Fprintf(&b, "    URL: %q\n", o.URL)
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

// mapToExtraTags transforms the object extra tags map to a slice of ExtraTagRow struct.
func mapToExtraTagRows(objectId types.Binary, extraTags map[string]string) []*ExtraTagRow {
	var extraTagRows []*ExtraTagRow
	for key, val := range extraTags {
		extraTagRows = append(extraTagRows, &ExtraTagRow{
			ObjectId: objectId,
			Tag:      key,
			Value:    val,
		})
	}

	return extraTagRows
}
