package rule

import (
	"fmt"
	"github.com/icinga/icinga-notifications/internal/event"
	"github.com/icinga/icinga-notifications/internal/filter"
	"math"
	"time"
)

// RetryNever indicates that an escalation condition should never be retried once it has been evaluated.
const RetryNever = time.Duration(math.MaxInt64)

type EscalationFilter struct {
	IncidentAge      time.Duration
	IncidentSeverity event.Severity
}

// ExtractRetryAfter extracts a time.Duration from the given filter conditions.
// The retry after duration is extracted from the specified filter conditions that evaluate the "incident_age"
// filter column. This function returns rule.RetryNever when all the incident age filter values are < the actual
// incident age or none of the filter conditions evaluates the "incident_age" column.
func (e *EscalationFilter) ExtractRetryAfter(conditions []filter.Condition, incidentAge time.Duration) time.Duration {
	retryAfter := RetryNever
	for _, condition := range conditions {
		if condition.Column == "incident_age" {
			age, err := time.ParseDuration(condition.Value)
			if err != nil {
				continue
			}

			if incidentAge < age {
				retryAfter = min(retryAfter, age)
			}
		}
	}

	return retryAfter
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
