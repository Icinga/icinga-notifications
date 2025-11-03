package event

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/icinga/icinga-go-library/database"
	baseEv "github.com/icinga/icinga-go-library/notifications/event"
	"github.com/icinga/icinga-go-library/types"
	"github.com/jmoiron/sqlx"
	"net/url"
	"strings"
	"time"
)

// ErrSuperfluousStateChange indicates a superfluous state change being ignored and stopping further processing.
var ErrSuperfluousStateChange = errors.New("ignoring superfluous state change")

// ErrSuperfluousMuteUnmuteEvent indicates that a superfluous mute or unmute event is being ignored and is
// triggered when trying to mute/unmute an already muted/unmuted incident.
var ErrSuperfluousMuteUnmuteEvent = errors.New("ignoring superfluous (un)mute event")

// Event received of a specified Type for internal processing.
//
// This is a representation of an event received from an external source with additional metadata with sole
// purpose of being used for internal processing. All the JSON serializable fields are these inherited from
// the base event type, and are used to decode the request body. Currently, there is no Event being marshalled
// into its JSON representation.
type Event struct {
	Time     time.Time `json:"-"`
	SourceId int64     `json:"-"`
	ID       int64     `json:"-"`

	baseEv.Event `json:",inline"`
}

// CompleteURL prefixes the URL with the given Icinga Web 2 base URL unless it already carries a URL or is empty.
func (e *Event) CompleteURL(icingaWebBaseUrl string) {
	if e.URL == "" {
		return
	}

	if !strings.HasSuffix(icingaWebBaseUrl, "/") {
		icingaWebBaseUrl += "/"
	}

	u, err := url.Parse(e.URL)
	if err != nil || u.Scheme == "" {
		e.URL = icingaWebBaseUrl + e.URL
	}
}

// Validate validates the current event state.
// Returns an error if it detects a misconfigured field.
func (e *Event) Validate() error {
	if len(e.Tags) == 0 {
		return fmt.Errorf("invalid event: tags must not be empty")
	}

	for tag := range e.Tags {
		if len(tag) > 255 {
			return fmt.Errorf("invalid event: tag %q is too long, at most 255 chars allowed, %d given", tag, len(tag))
		}
	}

	for tag := range e.ExtraTags {
		if len(tag) > 255 {
			return fmt.Errorf(
				"invalid event: extra tag %q is too long, at most 255 chars allowed, %d given", tag, len(tag),
			)
		}
	}

	if e.SourceId == 0 {
		return fmt.Errorf("invalid event: source ID must not be empty")
	}

	if e.Severity != baseEv.SeverityNone && e.Type != baseEv.TypeState {
		return fmt.Errorf("invalid event: if 'severity' is set, 'type' must be set to %q", baseEv.TypeState)
	}
	if e.Type == baseEv.TypeMute && (!e.Mute.Valid || !e.Mute.Bool) {
		return fmt.Errorf("invalid event: 'mute' must be true if 'type' is set to %q", baseEv.TypeMute)
	}
	if e.Type == baseEv.TypeUnmute && (!e.Mute.Valid || e.Mute.Bool) {
		return fmt.Errorf("invalid event: 'mute' must be false if 'type' is set to %q", baseEv.TypeUnmute)
	}
	if e.Mute.Valid && e.Mute.Bool && e.MuteReason == "" {
		return fmt.Errorf("invalid event: 'mute_reason' must not be empty if 'mute' is set")
	}

	if e.Type == baseEv.TypeUnknown {
		return fmt.Errorf("invalid event: missing type %q", e.Type)
	}

	return nil
}

// SetMute alters the event mute and mute reason.
func (e *Event) SetMute(muted bool, reason string) {
	e.Mute = types.Bool{Valid: true, Bool: muted}
	e.MuteReason = reason
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
	if e.Type != baseEv.TypeUnknown {
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
	eventID, err := database.InsertObtainID(ctx, tx, database.BuildInsertStmtWithout(db, eventRow, "id"), eventRow)
	if err == nil {
		e.ID = eventID
	}

	return err
}

// EventRow represents a single event database row and isn't an in-memory representation of an event.
type EventRow struct {
	ID         int64           `db:"id"`
	Time       types.UnixMilli `db:"time"`
	ObjectID   types.Binary    `db:"object_id"`
	Type       types.String    `db:"type"`
	Severity   baseEv.Severity `db:"severity"`
	Username   types.String    `db:"username"`
	Message    types.String    `db:"message"`
	Mute       types.Bool      `db:"mute"`
	MuteReason types.String    `db:"mute_reason"`
}

// TableName implements the contracts.TableNamer interface.
func (er *EventRow) TableName() string {
	return "event"
}

func NewEventRow(e *Event, objectId types.Binary) *EventRow {
	return &EventRow{
		Time:       types.UnixMilli(e.Time),
		ObjectID:   objectId,
		Type:       types.MakeString(e.Type.String(), types.TransformEmptyStringToNull),
		Severity:   e.Severity,
		Username:   types.MakeString(e.Username, types.TransformEmptyStringToNull),
		Message:    types.MakeString(e.Message, types.TransformEmptyStringToNull),
		Mute:       e.Mute,
		MuteReason: types.MakeString(e.MuteReason, types.TransformEmptyStringToNull),
	}
}
