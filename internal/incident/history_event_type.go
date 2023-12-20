package incident

import (
	"database/sql/driver"
	"fmt"
	"github.com/icinga/icinga-notifications/internal/event"
)

type HistoryEventType int

const (
	HistoryEventTypeNull HistoryEventType = iota
	SeverityChanged
	RecipientRoleChanged
	EscalationTriggered
	RuleMatched
	Opened
	Closed
	Notified
	DowntimeStarted
	DowntimeEnded
	DowntimeCancelled
	Custom
	FlappingStarted
	FlappingEnded
	CommentAdded
	CommentRemoved
)

var historyTypeByName = map[string]HistoryEventType{
	"incident_severity_changed": SeverityChanged,
	"recipient_role_changed":    RecipientRoleChanged,
	"escalation_triggered":      EscalationTriggered,
	"rule_matched":              RuleMatched,
	"opened":                    Opened,
	"closed":                    Closed,
	"notified":                  Notified,
	"downtime_started":          DowntimeStarted,
	"downtime_ended":            DowntimeEnded,
	"downtime_cancelled":        DowntimeCancelled,
	"custom":                    Custom,
	"flapping_started":          FlappingStarted,
	"flapping_ended":            FlappingEnded,
	"comment_added":             CommentAdded,
	"comment_removed":           CommentRemoved,
}

var historyEventTypeToName = func() map[HistoryEventType]string {
	eventTypes := make(map[HistoryEventType]string)
	for name, eventType := range historyTypeByName {
		eventTypes[eventType] = name
	}
	return eventTypes
}()

// Scan implements the sql.Scanner interface.
// Supports SQL NULL.
func (h *HistoryEventType) Scan(src any) error {
	if src == nil {
		*h = HistoryEventTypeNull
		return nil
	}

	var name string
	switch val := src.(type) {
	case string:
		name = val
	case []byte:
		name = string(val)
	default:
		return fmt.Errorf("unable to scan type %T into HistoryEventType", src)
	}

	historyType, ok := historyTypeByName[name]
	if !ok {
		return fmt.Errorf("unknown history event type %q", name)
	}

	*h = historyType

	return nil
}

func (h HistoryEventType) Value() (driver.Value, error) {
	if h == HistoryEventTypeNull {
		return nil, nil
	}

	return h.String(), nil
}

func (h *HistoryEventType) String() string {
	return historyEventTypeToName[*h]
}

func GetHistoryEventType(eventType string) (HistoryEventType, error) {
	var historyEvType HistoryEventType
	switch eventType {
	case event.TypeDowntimeStart:
		historyEvType = DowntimeStarted
	case event.TypeDowntimeEnd:
		historyEvType = DowntimeEnded
	case event.TypeDowntimeCancelled:
		historyEvType = DowntimeCancelled
	case event.TypeFlappingStart:
		historyEvType = FlappingStarted
	case event.TypeFlappingEnd:
		historyEvType = FlappingEnded
	case event.TypeCustom:
		historyEvType = Custom
	case event.TypeCommentAdded:
		historyEvType = CommentAdded
	default:
		//TODO: other events
		return historyEvType, fmt.Errorf("type %s not implemented yet", eventType)
	}

	return historyEvType, nil
}
