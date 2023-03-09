package event

import (
	"bytes"
	"fmt"
	"time"
)

type Event struct {
	Time   time.Time
	Source int64

	Name      string             `json:"name"`
	URL       string             `json:"url"`
	IdTags    map[string]*string `json:"tags"`
	ExtraTags map[string]*string `json:"extra_tags"`

	Type     string   `json:"type"`
	Severity Severity `json:"severity"`
	Username string   `json:"username"`
	Message  string   `json:"message"`
}

func (e *Event) String() string {
	var b bytes.Buffer
	_, _ = fmt.Fprintf(&b, "Event:\n")
	_, _ = fmt.Fprintf(&b, "  Name: %q\n", e.Name)
	_, _ = fmt.Fprintf(&b, "  URL: %q\n", e.URL)
	_, _ = fmt.Fprintf(&b, "  ID Tags:\n")
	for tag, value := range e.IdTags {
		_, _ = fmt.Fprintf(&b, "    %q", tag)
		if value != nil {
			_, _ = fmt.Fprintf(&b, " = %q", *value)
		}
		_, _ = fmt.Fprintf(&b, "\n")
	}
	_, _ = fmt.Fprintf(&b, "  Extra Tags:\n")
	for tag, value := range e.ExtraTags {
		_, _ = fmt.Fprintf(&b, "    %q", tag)
		if value != nil {
			_, _ = fmt.Fprintf(&b, " = %q", *value)
		}
		_, _ = fmt.Fprintf(&b, "\n")
	}
	_, _ = fmt.Fprintf(&b, "  Time: %s\n", e.Time)
	_, _ = fmt.Fprintf(&b, "  Source: %d\n", e.Source)
	if e.Type != "" {
		_, _ = fmt.Fprintf(&b, "  Type: %q\n", e.Type)
	}
	if e.Severity != 0 {
		_, _ = fmt.Fprintf(&b, "  Severity: %s\n", e.Severity.String())
	}
	if e.Username != "" {
		_, _ = fmt.Fprintf(&b, "  Username: %q\n", e.Username)
	}
	if e.Message != "" {
		_, _ = fmt.Fprintf(&b, "  Message: %q\n", e.Message)
	}
	return b.String()
}
