package incident

import (
	"database/sql/driver"
	"fmt"
)

type HistoryEventType int

const (
	HistoryEventTypeNull HistoryEventType = iota
	SourceSeverityChanged
	SeverityChanged
	RecipientRoleChanged
	EscalationTriggered
	RuleMatched
	Opened
	Closed
	Notified
)

var historyTypeByName = map[string]HistoryEventType{
	"source_severity_changed":   SourceSeverityChanged,
	"incident_severity_changed": SeverityChanged,
	"recipient_role_changed":    RecipientRoleChanged,
	"escalation_triggered":      EscalationTriggered,
	"rule_matched":              RuleMatched,
	"opened":                    Opened,
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
