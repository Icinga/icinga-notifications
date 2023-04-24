package object

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/icinga/icingadb/pkg/icingadb"
	"github.com/icinga/icingadb/pkg/types"
	"github.com/icinga/noma/internal/utils"
	"regexp"
	"sort"
	"strings"
	"sync"
)

type Object struct {
	ID       types.Binary
	Tags     map[string]string
	Metadata map[int64]*SourceMetadata

	db *icingadb.DB

	mu sync.Mutex
}

var (
	cache   = make(map[string]*Object)
	cacheMu sync.Mutex
)

func FromTags(db *icingadb.DB, tags map[string]string) (*Object, error) {
	id := ID(tags)

	cacheMu.Lock()
	defer cacheMu.Unlock()

	object, ok := cache[id.String()]
	if ok {
		return object, nil
	}

	object = &Object{ID: id, Tags: tags, db: db}
	cache[id.String()] = object

	stmt, _ := object.db.BuildInsertIgnoreStmt(&ObjectRow{})
	dbObj := &ObjectRow{
		ID:   object.ID,
		Host: object.Tags["host"],
	}

	if service, ok := object.Tags["service"]; ok {
		dbObj.Service = utils.ToDBString(service)
	}

	_, err := object.db.NamedExec(stmt, dbObj)
	if err != nil {
		return nil, fmt.Errorf("failed to insert object: %s", err)
	}

	return object, nil
}

func (o *Object) DisplayName() string {
	for _, metadata := range o.Metadata {
		if metadata.Name != "" {
			return metadata.Name
		}
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
	for source, metadata := range o.Metadata {
		_, _ = fmt.Fprintf(&b, "    Source %d:\n", source)
		_, _ = fmt.Fprintf(&b, "      Name: %q\n", metadata.Name)
		_, _ = fmt.Fprintf(&b, "      URL: %q\n", metadata.URL.String)
		_, _ = fmt.Fprintf(&b, "      Extra Tags:\n")
		for tag, value := range metadata.ExtraTags {
			_, _ = fmt.Fprintf(&b, "        %q", tag)
			if value != "" {
				_, _ = fmt.Fprintf(&b, " = %q", value)
			}
			_, _ = fmt.Fprintf(&b, "\n")
		}
	}
	return b.String()
}

func (o *Object) UpdateMetadata(source int64, name string, url types.String, extraTags map[string]string) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	sourceMetadata := &SourceMetadata{
		ObjectId:  o.ID,
		SourceId:  source,
		Name:      name,
		URL:       url,
		ExtraTags: extraTags,
	}

	stmt, _ := o.db.BuildUpsertStmt(&SourceMetadata{})
	_, err := o.db.NamedExec(stmt, sourceMetadata)
	if err != nil {
		return fmt.Errorf("failed to upsert object metadata: %s", err)
	}

	tx, err := o.db.BeginTxx(context.TODO(), nil)
	if err != nil {
		return fmt.Errorf("failed to start transaction for object extra tags: %s", err)
	}
	defer tx.Rollback()

	extraTag := &ExtraTagRow{ObjectId: o.ID, SourceId: source}
	_, err = tx.NamedExec(`DELETE FROM "object_extra_tag" WHERE "object_id" = :object_id AND "source_id" = :source_id`, extraTag)
	if err != nil {
		return fmt.Errorf("failed to delete object extra tags: %s", err)
	}

	if len(extraTags) > 0 {
		stmt, _ = o.db.BuildInsertStmt(extraTag)
		_, err = tx.NamedExec(stmt, sourceMetadata.mapToExtraTags())
		if err != nil {
			return fmt.Errorf("failed to insert object extra tags: %s", err)
		}
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit object extrag tags transaction: %s", err)
	}

	if o.Metadata == nil {
		o.Metadata = make(map[int64]*SourceMetadata)
	}

	if m := o.Metadata[source]; m != nil {
		m.Name = name
		m.URL = url
		m.ExtraTags = extraTags
	} else {
		o.Metadata[source] = sourceMetadata
	}

	return nil
}

func (o *Object) EvalEqual(key string, value string) bool {
	tagVal, ok := o.Tags[key]
	if ok && tagVal == value {
		return true
	}

	for _, m := range o.Metadata {
		tagVal, ok = m.ExtraTags[key]
		if ok && tagVal == value {
			return true
		}
	}

	return false
}

// EvalLike returns true when the objects tag/value matches the filter.Conditional value.
func (o *Object) EvalLike(key string, value string) bool {
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
		return true
	}

	for _, m := range o.Metadata {
		tagVal, ok = m.ExtraTags[key]
		if ok && regex.MatchString(tagVal) {
			return true
		}
	}

	return false
}

func (o *Object) EvalLess(key string, value string) bool {
	tagVal, ok := o.Tags[key]
	if ok && tagVal < value {
		return true
	}

	for _, m := range o.Metadata {
		tagVal, ok = m.ExtraTags[key]
		if ok && tagVal < value {
			return true
		}
	}

	return false
}

func (o *Object) EvalLessOrEqual(key string, value string) bool {
	tagVal, ok := o.Tags[key]
	if ok && tagVal <= value {
		return true
	}

	for _, m := range o.Metadata {
		tagVal, ok = m.ExtraTags[key]
		if ok && tagVal <= value {
			return true
		}
	}

	return false
}

func (o *Object) EvalExists(key string) bool {
	_, ok := o.Tags[key]
	if ok {
		return true
	}

	for _, m := range o.Metadata {
		_, ok = m.ExtraTags[key]
		if ok {
			return true
		}
	}

	return false
}

// TODO: the return value of this function must be stable like forever
func ID(tags map[string]string) types.Binary {
	h := sha256.New()

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

type SourceMetadata struct {
	ObjectId  types.Binary      `db:"object_id"`
	SourceId  int64             `db:"source_id"`
	Name      string            `db:"name"`
	URL       types.String      `db:"url"`
	ExtraTags map[string]string `db:"-"`
}

// TableName implements the contracts.TableNamer interface.
func (s *SourceMetadata) TableName() string {
	return "source_object"
}

// mapToExtraTags transforms the source metadata extra tags map to a slice of ExtraTagRow struct.
func (s *SourceMetadata) mapToExtraTags() []*ExtraTagRow {
	var extraTags []*ExtraTagRow
	for key, val := range s.ExtraTags {
		extraTags = append(extraTags, &ExtraTagRow{
			ObjectId: s.ObjectId,
			SourceId: s.SourceId,
			Tag:      key,
			Value:    val,
		})
	}

	return extraTags
}
