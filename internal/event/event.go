package event

import (
	"errors"
	"fmt"
	baseEv "github.com/icinga/icinga-go-library/notifications/event"
	"github.com/icinga/icinga-go-library/types"
	"go.uber.org/zap/zapcore"
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
		return errors.New("invalid event: missing type")
	}

	return nil
}

// SetMute alters the event mute and mute reason.
func (e *Event) SetMute(muted bool, reason string) {
	e.Mute = types.Bool{Valid: true, Bool: muted}
	e.MuteReason = reason
}

func (e *Event) String() string { return e.Name }

// MarshalLogObject implements the [zapcore.ObjectMarshaler] interface to allow logging the event as a structured object.
func (e *Event) MarshalLogObject(encoder zapcore.ObjectEncoder) error {
	encoder.AddString("name", e.Name)
	encoder.AddTime("time", e.Time)
	encoder.AddInt64("source_id", e.SourceId)
	encoder.AddString("type", e.Type.String())
	encoder.AddString("severity", e.Severity.String())
	encoder.AddString("username", e.Username)
	_ = encoder.AddObject("tags", zapcore.ObjectMarshalerFunc(func(objectEncoder zapcore.ObjectEncoder) error {
		for key, value := range e.Tags {
			objectEncoder.AddString(key, value)
		}
		return nil
	}))
	return nil
}
