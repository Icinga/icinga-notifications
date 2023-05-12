package rule

import (
	"fmt"
	"github.com/icinga/icinga-notifications/internal/event"
	"time"
)

type EscalationFilter struct {
	IncidentAge      time.Duration
	IncidentSeverity event.Severity
}

func (e *EscalationFilter) EvalEqual(key string, value string) (bool, error) {
	switch key {
	case "incident_age":
		age, err := time.ParseDuration(value)
		if err != nil {
			return false, err
		}

		return e.IncidentAge == age, nil
	case "incident_severity":
		severity, err := event.GetSeverityByName(value)
		if err != nil {
			return false, err
		}

		return e.IncidentSeverity == severity, nil
	default:
		return false, nil
	}
}

func (e *EscalationFilter) EvalLess(key string, value string) (bool, error) {
	switch key {
	case "incident_age":
		age, err := time.ParseDuration(value)
		if err != nil {
			return false, err
		}

		return e.IncidentAge < age, nil
	case "incident_severity":
		severity, err := event.GetSeverityByName(value)
		if err != nil {
			return false, err
		}

		return e.IncidentSeverity < severity, nil
	default:
		return false, nil
	}
}

func (e *EscalationFilter) EvalLike(key string, value string) (bool, error) {
	return false, fmt.Errorf("escalation filter doesn't support wildcard matches")
}

func (e *EscalationFilter) EvalLessOrEqual(key string, value string) (bool, error) {
	switch key {
	case "incident_age":
		age, err := time.ParseDuration(value)
		if err != nil {
			return false, err
		}

		return e.IncidentAge <= age, nil
	case "incident_severity":
		severity, err := event.GetSeverityByName(value)
		if err != nil {
			return false, err
		}

		return e.IncidentSeverity <= severity, nil
	default:
		return false, nil
	}
}

func (e *EscalationFilter) EvalExists(key string) bool {
	switch key {
	case "incident_age":
		fallthrough
	case "incident_severity":
		return true
	default:
		return false
	}
}
