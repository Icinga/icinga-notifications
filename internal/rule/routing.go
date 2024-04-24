package rule

import (
	"fmt"
)

// Routing represents a single rule routing database entry and defines how non-state events should be handled.
// This is used to determine the appropriate recipients for non-state event notifications when there is no
// active event for a particular object.
type Routing struct {
	Meta `db:",inline"`

	Recipients []*RoutingRecipient
}

func (r *Routing) TableName() string {
	return "rule_routing"
}

// RoutingRecipient represents a single rule routing recipient.
type RoutingRecipient struct {
	RecipientMeta `db:",inline"`
	RoutingID     int64 `db:"rule_routing_id"`
}

func (r *RoutingRecipient) TableName() string {
	return "rule_routing_recipient"
}

// RoutingFilter is used to evaluate rule Routing conditions.
// Currently, it only implements the equal operator for the "event_type" filter key.
type RoutingFilter struct {
	EventType string
}

func (rf *RoutingFilter) EvalEqual(key string, value string) (bool, error) {
	switch key {
	case "event_type":
		return rf.EventType == value, nil
	default:
		return false, fmt.Errorf("unsupported rule routing filter option %q", key)
	}
}

func (rf *RoutingFilter) EvalLess(key string, value string) (bool, error) {
	return false, fmt.Errorf("rule routing filter doesn't support '<' operator")
}

func (rf *RoutingFilter) EvalLike(key string, value string) (bool, error) {
	return false, fmt.Errorf("rule routing filter doesn't support wildcard matches")
}

func (rf *RoutingFilter) EvalLessOrEqual(key string, value string) (bool, error) {
	return rf.EvalEqual(key, value)
}

func (rf *RoutingFilter) EvalExists(key string) bool {
	return key == "event_type"
}
