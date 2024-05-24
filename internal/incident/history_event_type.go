package incident

import (
	"database/sql/driver"
	"fmt"
)

type HistoryEventType int

const (
	HistoryEventTypeNull HistoryEventType = iota

	Opened
	Muted
	Unmuted
	IncidentSeverityChanged
	RuleMatched
	EscalationTriggered
	RecipientRoleChanged
	Closed
	Notified
)

var historyTypeByName = map[string]HistoryEventType{
	"opened":                    Opened,
	"muted":                     Muted,
	"unmuted":                   Unmuted,
	"incident_severity_changed": IncidentSeverityChanged,
	"rule_matched":              RuleMatched,
	"escalation_triggered":      EscalationTriggered,
	"recipient_role_changed":    RecipientRoleChanged,
	"closed":                    Closed,
	"notified":                  Notified,
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
