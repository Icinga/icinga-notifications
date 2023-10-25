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

// ReevaluateAfter returns the duration after which escalationCond should be reevaluated the
// next time on the incident represented by e.
//
// escalationCond must correspond to an escalation that did not trigger on the incident
// represented by e before. If nothing in the incident changes apart from time passing by,
// the escalation is guaranteed to not trigger within the returned duration. After that
// duration, the escalation should be reevaluated, and it may or may not trigger. If anything
// else changes, for example due to an external event, the escalation must be reevaluated as
// well.
func (e *EscalationFilter) ReevaluateAfter(escalationCond filter.Filter) time.Duration {
	retryAfter := RetryNever
	for _, condition := range escalationCond.ExtractConditions() {
		if condition.Column() == "incident_age" {
			v, err := time.ParseDuration(condition.Value())
			if err == nil && v > e.IncidentAge {
				// The incident age is compared with a value in the future. Once that age is
				// reached, the escalation could trigger, so consider that time for reevaluation.
				retryAfter = min(retryAfter, v-e.IncidentAge)
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
