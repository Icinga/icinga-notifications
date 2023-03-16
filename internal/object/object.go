package object

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
)

type Object struct {
	ID       [sha256.Size]byte
	Tags     map[string]string
	Metadata map[int64]*SourceMetadata

	mu sync.Mutex
}

var (
	cache   = make(map[[sha256.Size]byte]*Object)
	cacheMu sync.Mutex
)

func FromTags(tags map[string]string) *Object {
	id := ID(tags)

	cacheMu.Lock()
	defer cacheMu.Unlock()

	object, ok := cache[id]
	if ok {
		return object
	}

	object = &Object{ID: id, Tags: tags}
	cache[id] = object
	return object
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
		_, _ = fmt.Fprintf(&b, "      URL: %q\n", metadata.URL)
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

func (o *Object) UpdateMetadata(source int64, name string, url string, extraTags map[string]string) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.Metadata == nil {
		o.Metadata = make(map[int64]*SourceMetadata)
	}

	if m := o.Metadata[source]; m != nil {
		m.Name = name
		m.URL = url
		m.ExtraTags = extraTags
		return
	}

	o.Metadata[source] = &SourceMetadata{
		Name:      name,
		URL:       url,
		ExtraTags: extraTags,
	}
}

// TODO: the return value of this function must be stable like forever
func ID(tags map[string]string) [sha256.Size]byte {
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

	return [32]byte(h.Sum(nil))
}

type SourceMetadata struct {
	Name      string
	URL       string
	ExtraTags map[string]string
}
