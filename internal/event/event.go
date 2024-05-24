package event

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/icinga/icinga-go-library/database"
	"github.com/icinga/icinga-go-library/types"
	"github.com/icinga/icinga-notifications/internal/utils"
	"github.com/jmoiron/sqlx"
	"time"
)

// ErrSuperfluousStateChange indicates a superfluous state change being ignored and stopping further processing.
var ErrSuperfluousStateChange = errors.New("ignoring superfluous state change")

// Event received of a specified Type for internal processing.
//
// The JSON struct tags are being used to unmarshal a JSON representation received from the listener.Listener. Some
// fields are being omitted as they are only allowed to be populated from within icinga-notifications. Currently, there
// is no Event being marshalled into its JSON representation.
type Event struct {
	Time     time.Time `json:"-"`
	SourceId int64     `json:"-"`

	Name      string            `json:"name"`
	URL       string            `json:"url"`
	Tags      map[string]string `json:"tags"`
	ExtraTags map[string]string `json:"extra_tags"`

	Type     string   `json:"type"`
	Severity Severity `json:"severity"`
	Username string   `json:"username"`
	Message  string   `json:"message"`

	ID int64 `json:"-"`
}

const (
	TypeState                  = "state"
	TypeAcknowledgementSet     = "acknowledgement-set"
	TypeAcknowledgementCleared = "acknowledgement-cleared"
	TypeInternal               = "internal"
	TypeDowntimeRemoved        = "downtime-removed"
	TypeDowntimeStart          = "downtime-start"
	TypeDowntimeEnd            = "downtime-end"
	TypeCustom                 = "custom"
	TypeFlappingStart          = "flapping-start"
	TypeFlappingEnd            = "flapping-end"
)

// Validate validates the current event state.
// Returns an error if it detects a misconfigured field.
func (e *Event) Validate() error {
	if len(e.Tags) == 0 {
		return fmt.Errorf("invalid event: tags must not be empty")
	}

	if e.SourceId == 0 {
		return fmt.Errorf("invalid event: source ID must not be empty")
	}

	if e.Severity != SeverityNone && e.Type != TypeState {
		return fmt.Errorf("invalid event: if 'severity' is set, 'type' must be set to %q", TypeState)
	}

	switch e.Type {
	case "":
		return fmt.Errorf("invalid event: 'type' must not be empty")
	case TypeState,
		TypeAcknowledgementSet,
		TypeAcknowledgementCleared,
		TypeInternal,
		TypeDowntimeRemoved,
		TypeDowntimeStart,
		TypeDowntimeEnd,
		TypeCustom,
		TypeFlappingStart,
		TypeFlappingEnd:
		return nil
	default:
		return fmt.Errorf("invalid event: unsupported event type %q", e.Type)
	}
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

// Sync transforms this event to *event.EventRow and synchronises with the database.
func (e *Event) Sync(ctx context.Context, tx *sqlx.Tx, db *database.DB, objectId types.Binary) error {
	if e.ID != 0 {
		return nil
	}

	eventRow := NewEventRow(e, objectId)
	eventID, err := utils.InsertAndFetchId(ctx, tx, utils.BuildInsertStmtWithout(db, eventRow, "id"), eventRow)
	if err == nil {
		e.ID = eventID
	}

	return err
}

// EventRow represents a single event database row and isn't an in-memory representation of an event.
type EventRow struct {
	ID       int64           `db:"id"`
	Time     types.UnixMilli `db:"time"`
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

func NewEventRow(e *Event, objectId types.Binary) *EventRow {
	return &EventRow{
		Time:     types.UnixMilli(e.Time),
		ObjectID: objectId,
		Type:     utils.ToDBString(e.Type),
		Severity: e.Severity,
		Username: utils.ToDBString(e.Username),
		Message:  utils.ToDBString(e.Message),
	}
}
