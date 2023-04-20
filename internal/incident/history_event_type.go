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

// Scan implements the sql.Scanner interface.
// Supports SQL NULL.
func (h *HistoryEventType) Scan(src any) error {
	if src == nil {
		*h = HistoryEventTypeNull
		return nil
	}

	name, ok := src.(string)
	if !ok {
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
	val := h.String()
	if val == "" {
		return nil, nil
	}

	return val, nil
}

func (h *HistoryEventType) String() string {
	for name, historyType := range historyTypeByName {
		if historyType == *h {
			return name
		}
	}

	return ""
}
