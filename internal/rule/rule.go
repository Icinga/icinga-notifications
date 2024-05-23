package rule

import (
	"github.com/icinga/icinga-go-library/types"
	"github.com/icinga/icinga-notifications/internal/filter"
	"github.com/icinga/icinga-notifications/internal/recipient"
	"github.com/icinga/icinga-notifications/internal/timeperiod"
	"go.uber.org/zap/zapcore"
	"time"
)

type Rule struct {
	ID               int64                  `db:"id"`
	IsActive         types.Bool             `db:"is_active"`
	Name             string                 `db:"name"`
	TimePeriod       *timeperiod.TimePeriod `db:"-"`
	TimePeriodID     types.Int              `db:"timeperiod_id"`
	ObjectFilter     filter.Filter          `db:"-"`
	ObjectFilterExpr types.String           `db:"object_filter"`
	Escalations      map[int64]*Escalation  `db:"-"`
}

// MarshalLogObject implements the zapcore.ObjectMarshaler interface.
func (r *Rule) MarshalLogObject(encoder zapcore.ObjectEncoder) error {
	encoder.AddInt64("id", r.ID)
	encoder.AddString("name", r.Name)

	if r.TimePeriodID.Valid && r.TimePeriodID.Int64 != 0 {
		encoder.AddInt64("timeperiod_id", r.TimePeriodID.Int64)
	}
	if r.ObjectFilterExpr.Valid && r.ObjectFilterExpr.String != "" {
		encoder.AddString("object_filter", r.ObjectFilterExpr.String)
	}

	return nil
}

// ContactChannels stores a set of channel IDs for each set of individual contacts.
type ContactChannels map[*recipient.Contact]map[int64]bool

// LoadFromEscalationRecipients loads recipients channel of the specified escalation to the current map.
// You can provide this method a callback to control whether the channel of a specific contact should
// be loaded, and it will skip those for whom the callback returns false. Pass AlwaysNotifiable for default actions.
func (ch ContactChannels) LoadFromEscalationRecipients(escalation *Escalation, t time.Time, isNotifiable func(recipient.Key) bool) {
	for _, escalationRecipient := range escalation.Recipients {
		ch.LoadRecipientChannel(escalationRecipient, t, isNotifiable)
	}
}

// LoadRecipientChannel loads recipient channel to the current map.
// You can provide this method a callback to control whether the channel of a specific contact should
// be loaded, and it will skip those for whom the callback returns false. Pass AlwaysNotifiable for default actions.
func (ch ContactChannels) LoadRecipientChannel(er *EscalationRecipient, t time.Time, isNotifiable func(recipient.Key) bool) {
	if isNotifiable(er.Key) {
		for _, c := range er.Recipient.GetContactsAt(t) {
			if ch[c] == nil {
				ch[c] = make(map[int64]bool)
			}
			if er.ChannelID.Valid {
				ch[c][er.ChannelID.Int64] = true
			} else {
				ch[c][c.DefaultChannelID] = true
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
