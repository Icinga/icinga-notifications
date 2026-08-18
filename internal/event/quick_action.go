//go:generate go tool stringer -type Action -linecomment -output quick_action_string.go

package event

import (
	"encoding/json"
	"fmt"

	"go.uber.org/zap/zapcore"
)

// Action represents the kind of quick action that can be performed on an incident in Icinga Notifications Web.
type Action uint8

const (
	ActionInvalid Action = iota // invalid

	ActionManage      // manage
	ActionUnmanage    // unmanage
	ActionSubscribe   // subscribe
	ActionUnsubscribe // unsubscribe

	actionMax // internal
)

// UnmarshalJSON implements the [json.Unmarshaler] interface.
func (a *Action) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}

	for kind := range actionMax {
		if kind != ActionInvalid && kind.String() == s {
			*a = kind
			return nil
		}
	}
	return fmt.Errorf("invalid quick action: %q", s)
}

// QuickAction represents a quick action that can be performed on an incident in Icinga Notifications Web.
type QuickAction struct {
	Kind       Action            `json:"action"`
	ContactID  int64             `json:"contact_id"`
	SourceID   int64             `json:"source_id"`
	ObjectTags map[string]string `json:"object_tags"`
}

// MarshalLogObject implements the [zapcore.ObjectMarshaler] interface.
func (qa *QuickAction) MarshalLogObject(enc zapcore.ObjectEncoder) error {
	enc.AddString("action", qa.Kind.String())
	enc.AddInt64("contact_id", qa.ContactID)
	return enc.AddObject("object_tags", zapcore.ObjectMarshalerFunc(func(enc zapcore.ObjectEncoder) error {
		for k, v := range qa.ObjectTags {
			enc.AddString(k, v)
		}
		return nil
	}))
}
