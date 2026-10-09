package rule

import (
	"errors"
	"time"

	"github.com/icinga/icinga-go-library/types"
	"github.com/icinga/icinga-notifications/internal/config/baseconf"
	"github.com/icinga/icinga-notifications/internal/filter"
	"github.com/icinga/icinga-notifications/internal/recipient"
	"github.com/icinga/icinga-notifications/internal/timeperiod"
	"go.uber.org/zap/zapcore"
)

// Type represents the type of rule, either a notification or an escalation.
type Type string

// String returns the string representation of the rule type.
func (t Type) String() string {
	return string(t)
}

// Every rule must have exactly one type of the following values.
const (
	// TypeNotification is used for rules that are triggered immediately when an event occurs and without relation to an incident.
	TypeNotification Type = "notification"
	// TypeEscalation is used for rules that are triggered when an incident is opened or escalated.
	TypeEscalation Type = "escalation"
)

type Rule struct {
	baseconf.IncrementalPkDbEntry[int64] `db:",inline"`

	Name             string                 `db:"name"`
	TimePeriod       *timeperiod.TimePeriod `db:"-"`
	TimePeriodID     types.Int              `db:"timeperiod_id"`
	ObjectFilter     filter.Filter          `db:"-"`
	ObjectFilterExpr types.String           `db:"object_filter"`
	SourceType       string                 `db:"source_type"`
	Type             Type                   `db:"type"`
	Entries          map[int64]*Entry       `db:"-"`

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
	if r.Type != TypeNotification && r.Type != TypeEscalation {
		return errors.New("invalid rule type: " + r.Type.String())
	}

	if r.ObjectFilterExpr.Valid {
		f, err := filter.ParseASTExpr(r.ObjectFilterExpr.String)
		if err != nil {
			return err
		}

		r.ObjectFilter = f
		if f != nil {
			for _, condition := range f.ExtractConditions() {
				r.FilterColumns = append(r.FilterColumns, condition.Attributes())
			}
		}
	}
	return nil
}

// MarshalLogObject implements the zapcore.ObjectMarshaler interface.
func (r *Rule) MarshalLogObject(encoder zapcore.ObjectEncoder) error {
	encoder.AddInt64("id", r.ID)
	encoder.AddString("name", r.Name)
	encoder.AddString("source_type", r.SourceType)
	encoder.AddString("rule_type", r.Type.String())

	if r.TimePeriodID.Valid && r.TimePeriodID.Int64 != 0 {
		encoder.AddInt64("timeperiod_id", r.TimePeriodID.Int64)
	}
	if r.ObjectFilter != nil {
		encoder.AddString("object_filter", r.ObjectFilter.String())
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

// ChannelOrigin identifies the entry recipient through which a contact's channel was selected.
//
// A zero value denotes a contact that was added without any rule involvement,
// e.g. a recipient that subscribed to or manages an incident via the UI.
type ChannelOrigin struct {
	ChannelID      int64
	RuleID         int64 // Zero if the contact subscribed via web-gui
	RuleEntryID    int64 // Zero if the contact subscribed via web-gui
	ContactGroupID int64 // Non-zero if the contact was resolved from a contact group.
	ScheduleID     int64 // Non-zero if the contact was resolved from a schedule.
	Role           recipient.Role

	IncidentRelated bool // True if the contact was resolved from an incident-related recipient.
}

// ContactChannels stores, per contact and channel ID, the origins that selected this channel.
//
// When multiple entry recipients resolve to the same contact and channel, all their origins are
// recorded: the first origin is the one the notification is attributed to, any further ones denote
// duplicates that would have notified the same contact via the same channel.
type ContactChannels map[*recipient.Contact][]ChannelOrigin

// LoadFromEntryRecipients loads recipients channel of the specified Entry to the current map.
// isIncidentRelated is stored on each resulting ChannelOrigin.IncidentRelated, to mark whether these
// recipients were resolved via an incident-related path (e.g. escalations) rather than a plain
// notification rule.
// You can provide this method a callback to control whether the channel of a specific contact should
// be loaded, and it will skip those for whom the callback returns false. Pass AlwaysNotifiable for default actions.
func (ch ContactChannels) LoadFromEntryRecipients(entry *Entry, t time.Time, isIncidentRelated bool, isNotifiable func(recipient.Key) bool) {
	for _, er := range entry.Recipients {
		ch.LoadRecipientChannel(er, entry.RuleID, t, isIncidentRelated, isNotifiable)
	}
}

// LoadRecipientChannel loads recipient channel to the current map.
// You can provide this method a callback to control whether the channel of a specific contact should
// be loaded, and it will skip those for whom the callback returns false. Pass AlwaysNotifiable for default actions.
func (ch ContactChannels) LoadRecipientChannel(
	r *EntryRecipient,
	ruleID int64,
	t time.Time,
	isIncidentRelated bool,
	isNotifiable func(recipient.Key) bool,
) {
	if !isNotifiable(r.Key) {
		return
	}

	origin := ChannelOrigin{
		ChannelID:      r.ChannelID.Int64,
		RuleID:         ruleID,
		RuleEntryID:    r.EntryID,
		ContactGroupID: r.GroupID.Int64,
		ScheduleID:     r.ScheduleID.Int64,
		Role:           recipient.RoleRecipient,

		IncidentRelated: isIncidentRelated,
	}

	for _, c := range r.Recipient.GetContactsAt(t) {
		if r.ChannelID.Valid {
			origin.ChannelID = r.ChannelID.Int64
		} else {
			origin.ChannelID = c.DefaultChannelID
		}
		ch[c] = append(ch[c], origin)
	}
}

// AlwaysNotifiable (checks) whether the given recipient is notifiable and returns always true.
// This function is usually passed as an argument to ContactChannels.LoadFromEntryRecipients whenever you do
// not want to perform any custom actions.
func AlwaysNotifiable(_ recipient.Key) bool {
	return true
}
