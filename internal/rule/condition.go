package rule

import (
	"fmt"
	"math"
	"time"

	"github.com/icinga/icinga-go-library/notifications/event"
	"github.com/icinga/icinga-notifications/internal/filter"
)

// RetryNever indicates that an escalation condition should never be retried once it has been evaluated.
const RetryNever = time.Duration(math.MaxInt64)

type EscalationFilter struct {
	IncidentAge      time.Duration
	IncidentSeverity event.Severity
	IsManaged        bool
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
		if keyString(condition.Attributes()) == "incident_age" {
			v, err := time.ParseDuration(fmt.Sprint(condition.Value()))
			if err == nil && v > e.IncidentAge {
				// The incident age is compared with a value in the future. Once that age is
				// reached, the escalation could trigger, so consider that time for reevaluation.
				retryAfter = min(retryAfter, v-e.IncidentAge)
			}
		}
	}

	return retryAfter
}

func (e *EscalationFilter) EvalEqual(key []string, value any) (bool, error) {
	switch keyString(key) {
	case "incident_age":
		age, err := time.ParseDuration(fmt.Sprint(value))
		if err != nil {
			return false, err
		}

		return e.IncidentAge == age, nil
	case "incident_severity":
		severity, err := event.ParseSeverity(fmt.Sprint(value))
		if err != nil {
			return false, err
		}

		return e.IncidentSeverity == severity, nil
	case "is_managed":
		managed, err := parseManagedValue(value)
		if err != nil {
			return false, err
		}

		return e.IsManaged == managed, nil
	default:
		return false, nil
	}
}

func (e *EscalationFilter) EvalLess(key []string, value any) (bool, error) {
	switch keyString(key) {
	case "incident_age":
		age, err := time.ParseDuration(fmt.Sprint(value))
		if err != nil {
			return false, err
		}

		return e.IncidentAge < age, nil
	case "incident_severity":
		severity, err := event.ParseSeverity(fmt.Sprint(value))
		if err != nil {
			return false, err
		}

		return e.IncidentSeverity < severity, nil
	case "is_managed":
		return false, fmt.Errorf("is_managed only supports the equality operators (= and !=)")
	default:
		return false, nil
	}
}

func (e *EscalationFilter) EvalLike(_ []string, _ any) (bool, error) {
	return false, fmt.Errorf("escalation filter does not support wildcard matches")
}

func (e *EscalationFilter) EvalLessOrEqual(key []string, value any) (bool, error) {
	switch keyString(key) {
	case "incident_age":
		age, err := time.ParseDuration(fmt.Sprint(value))
		if err != nil {
			return false, err
		}

		return e.IncidentAge <= age, nil
	case "incident_severity":
		severity, err := event.ParseSeverity(fmt.Sprint(value))
		if err != nil {
			return false, err
		}

		return e.IncidentSeverity <= severity, nil
	case "is_managed":
		return false, fmt.Errorf("is_managed only supports the equality operators (= and !=)")
	default:
		return false, nil
	}
}

func (e *EscalationFilter) EvalExists(key []string) bool {
	switch keyString(key) {
	case "incident_age":
		fallthrough
	case "incident_severity":
		fallthrough
	case "is_managed":
		return true
	default:
		return false
	}
}

// parseManagedValue parses the value of the is_managed attribute, which is expected to be either "y" or "n".
func parseManagedValue(value any) (bool, error) {
	switch v := fmt.Sprint(value); v {
	case "y":
		return true, nil
	case "n":
		return false, nil
	default:
		return false, fmt.Errorf(`invalid is_managed value %q, expected either "y" or "n"`, v)
	}
}

// keyString returns the first element of the key slice, or an empty string if the slice is empty.
func keyString(key []string) string {
	if len(key) == 0 {
		return ""
	}
	return key[0]
}

type NotificationFilter struct {
	EventType string
}

func (n *NotificationFilter) EvalEqual(key []string, value any) (bool, error) {
	switch keyString(key) {
	case "event_type":
		if s, ok := value.(string); ok && n.EventType == s {
			return true, nil
		}
	}
	return false, nil
}

func (n *NotificationFilter) EvalExists(key []string) bool {
	return keyString(key) == "event_type"
}

func (n *NotificationFilter) EvalLike(key []string, value any) (bool, error) {
	return false, fmt.Errorf("notification rule filter does not support wildcard matches")
}
func (n *NotificationFilter) EvalLess(key []string, value any) (bool, error) {
	return false, fmt.Errorf("notification rule filter does not support 'less' matches")
}
func (n *NotificationFilter) EvalLessOrEqual(key []string, value any) (bool, error) {
	return false, fmt.Errorf("notification rule filter does not support 'less or equal' matches")
}
