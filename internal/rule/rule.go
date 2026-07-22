package rule

import (
	"encoding/json"
	"errors"

	"github.com/icinga/icinga-go-library/types"
	"github.com/icinga/icinga-notifications/internal/config/baseconf"
	"github.com/icinga/icinga-notifications/internal/filter"
	"github.com/icinga/icinga-notifications/internal/recipient"
	"github.com/icinga/icinga-notifications/internal/timeperiod"
	"go.uber.org/zap/zapcore"
	"time"
)

type Rule struct {
	baseconf.IncrementalPkDbEntry[int64] `db:",inline"`

	Name             string                 `db:"name"`
	TimePeriod       *timeperiod.TimePeriod `db:"-"`
	TimePeriodID     types.Int              `db:"timeperiod_id"`
	SourceID         int64                  `db:"source_id"`
	ObjectFilter     filter.Filter          `db:"-"`
	ObjectFilterExpr types.String           `db:"object_filter"`
	Escalations      map[int64]*Escalation  `db:"-"`

	// FilterColumns is a set of all filter columns used in the rule's ObjectFilter.
	//
	// This is computed from the ObjectFilter once and can be used by sources to determine which
	// columns they need to provide for the events to be able to evaluate the rule.
	FilterColumns FilterAttrsType `db:"-"`
}

// FilterAttrsType represents a list of filter attributes for a given list of filter conditions.
type FilterAttrsType [][]string

// IncrementalInitAndValidate implements the config.IncrementalConfigurableInitAndValidatable interface.
func (r *Rule) IncrementalInitAndValidate() error {
	if r.ObjectFilterExpr.Valid {
		data := map[string]json.RawMessage{}
		if err := json.Unmarshal([]byte(r.ObjectFilterExpr.String), &data); err != nil {
			return err
		}
		filterBytes, exists := data["ast"]
		if !exists {
			return errors.New("missing 'ast' field in object filter expression")
		}

		f, err := filter.UnmarshalJSON(filterBytes)
		if err != nil {
			return err
		}

		r.ObjectFilter = f
		if f != nil {
			for _, condition := range f.ExtractConditions() {
				if attrs, ok := condition.Attributes().([]string); ok {
					r.FilterColumns = append(r.FilterColumns, attrs)
				}
			}
		}
	}
	return nil
}

// MarshalLogObject implements the zapcore.ObjectMarshaler interface.
func (r *Rule) MarshalLogObject(encoder zapcore.ObjectEncoder) error {
	encoder.AddInt64("id", r.ID)
	encoder.AddString("name", r.Name)
	encoder.AddInt64("source_id", r.SourceID)

	if r.TimePeriodID.Valid && r.TimePeriodID.Int64 != 0 {
		encoder.AddInt64("timeperiod_id", r.TimePeriodID.Int64)
	}

	return nil
}

// Eval evaluates the configured object filter for the provided filterable.
//
// Returns always true if the current rule doesn't have a configured object filter.
func (r *Rule) Eval(filterable filter.Filterable) (bool, error) {
	if r.ObjectFilter == nil {
		return true, nil
	}
	return r.ObjectFilter.Eval(filterable)
}

// ChannelOrigin identifies the escalation recipient through which a contact's channel was selected.
//
// A zero value denotes a contact that was added without any rule involvement,
// e.g. a recipient that subscribed to or manages an incident via the UI.
type ChannelOrigin struct {
	RuleID           int64
	RuleEscalationID int64
	ContactGroupID   int64 // Non-zero if the contact was resolved from a contact group.
	ScheduleID       int64 // Non-zero if the contact was resolved from a schedule.
}

// ContactChannels stores, per contact and channel ID, the origins that selected this channel.
//
// When multiple escalation recipients resolve to the same contact and channel, all their origins are
// recorded: the first origin is the one the notification is attributed to, any further ones denote
// duplicates that would have notified the same contact via the same channel.
type ContactChannels map[*recipient.Contact]map[int64][]ChannelOrigin

// LoadFromEscalationRecipients loads recipients channel of the specified escalation to the current map.
// You can provide this method a callback to control whether the channel of a specific contact should
// be loaded, and it will skip those for whom the callback returns false. Pass AlwaysNotifiable for default actions.
func (ch ContactChannels) LoadFromEscalationRecipients(escalation *Escalation, t time.Time, isNotifiable func(recipient.Key) bool) {
	for _, escalationRecipient := range escalation.Recipients {
		ch.LoadRecipientChannel(escalationRecipient, escalation.RuleID, t, isNotifiable)
	}
}

// LoadRecipientChannel loads recipient channel to the current map.
// You can provide this method a callback to control whether the channel of a specific contact should
// be loaded, and it will skip those for whom the callback returns false. Pass AlwaysNotifiable for default actions.
func (ch ContactChannels) LoadRecipientChannel(er *EscalationRecipient, ruleID int64, t time.Time, isNotifiable func(recipient.Key) bool) {
	if isNotifiable(er.Key) {
		origin := ChannelOrigin{
			RuleID:           ruleID,
			RuleEscalationID: er.EscalationID,
			ContactGroupID:   er.GroupID.Int64,
			ScheduleID:       er.ScheduleID.Int64,
		}
		for _, c := range er.Recipient.GetContactsAt(t) {
			if ch[c] == nil {
				ch[c] = make(map[int64][]ChannelOrigin)
			}
			if er.ChannelID.Valid {
				ch[c][er.ChannelID.Int64] = append(ch[c][er.ChannelID.Int64], origin)
			} else {
				ch[c][c.DefaultChannelID] = append(ch[c][c.DefaultChannelID], origin)
			}
		}
	}
}

// AlwaysNotifiable (checks) whether the given recipient is notifiable and returns always true.
// This function is usually passed as an argument to ContactChannels.LoadFromEscalationRecipients whenever you do
// not want to perform any custom actions.
func AlwaysNotifiable(_ recipient.Key) bool {
	return true
}
