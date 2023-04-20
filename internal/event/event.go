package event

import (
	"bytes"
	"fmt"
	"github.com/icinga/icingadb/pkg/icingadb"
	"github.com/icinga/icingadb/pkg/types"
	"github.com/icinga/noma/internal/utils"
	"time"
)

type Event struct {
	Time     time.Time
	SourceId int64 `json:"source_id"`

	Name      string            `json:"name"`
	URL       string            `json:"url"`
	Tags      map[string]string `json:"tags"`
	ExtraTags map[string]string `json:"extra_tags"`

	Type     string   `json:"type"`
	Severity Severity `json:"severity"`
	Username string   `json:"username"`
	Message  string   `json:"message"`

	ID int64
}

func (e *Event) String() string {
	return fmt.Sprintf("[time=%s type=%q severity=%s]", e.Time, e.Type, e.Severity.String())
}

func (e *Event) FullString() string {
	var b bytes.Buffer
	_, _ = fmt.Fprintf(&b, "Event:\n")
	_, _ = fmt.Fprintf(&b, "  Name: %q\n", e.Name)
	_, _ = fmt.Fprintf(&b, "  URL: %q\n", e.URL)
	_, _ = fmt.Fprintf(&b, "  ID Tags:\n")
	for tag, value := range e.Tags {
		_, _ = fmt.Fprintf(&b, "    %q", tag)
		if value != "" {
			_, _ = fmt.Fprintf(&b, " = %q", value)
		}
		_, _ = fmt.Fprintf(&b, "\n")
	}
	_, _ = fmt.Fprintf(&b, "  Extra Tags:\n")
	for tag, value := range e.ExtraTags {
		_, _ = fmt.Fprintf(&b, "    %q", tag)
		if value != "" {
			_, _ = fmt.Fprintf(&b, " = %q", value)
		}
		_, _ = fmt.Fprintf(&b, "\n")
	}
	_, _ = fmt.Fprintf(&b, "  Time: %s\n", e.Time)
	_, _ = fmt.Fprintf(&b, "  SourceId: %d\n", e.SourceId)
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

// Sync transforms this event to *event.EventRow and calls its sync method.
func (e *Event) Sync(db *icingadb.DB, objectId types.Binary) error {
	if e.ID != 0 {
		return nil
	}

	eventRow := &EventRow{
		Time:     types.UnixMilli(e.Time),
		SourceID: e.SourceId,
		ObjectID: objectId,
		Type:     utils.ToDBString(e.Type),
		Severity: e.Severity,
		Username: utils.ToDBString(e.Username),
		Message:  utils.ToDBString(e.Message),
	}

	err := eventRow.Sync(db)
	if err == nil {
		e.ID = eventRow.ID
	}

	return err
}

// EventRow represents a single event database row and isn't an in-memory representation of an event.
type EventRow struct {
	ID       int64           `db:"id"`
	Time     types.UnixMilli `db:"time"`
	SourceID int64           `db:"source_id"`
	ObjectID types.Binary    `db:"object_id"`
	Type     types.String    `db:"type"`
	Severity Severity        `db:"severity"`
	Username types.String    `db:"username"`
	Message  types.String    `db:"message"`
}

// TableName implements the contracts.TableNamer interface.
func (er *EventRow) TableName() string {
	return "event"
}

// Sync synchronizes this types data to the database.
// Returns an error when any of the database operation fails.
func (er *EventRow) Sync(db *icingadb.DB) error {
	eventId, err := utils.InsertAndFetchId(db, utils.BuildInsertStmtWithout(db, er, "id"), er)
	if err != nil {
		return err
	}

	er.ID = eventId

	return nil
}
