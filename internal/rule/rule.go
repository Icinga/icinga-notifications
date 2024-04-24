package rule

import (
	"database/sql"
	"github.com/icinga/icinga-notifications/internal/filter"
	"github.com/icinga/icinga-notifications/internal/recipient"
	"github.com/icinga/icinga-notifications/internal/timeperiod"
	"github.com/icinga/icingadb/pkg/types"
	"go.uber.org/zap/zapcore"
	"time"
)

type Rule struct {
	ID               int64      `db:"id"`
	IsActive         types.Bool `db:"is_active"`
	Name             string     `db:"name"`
	TimePeriod       *timeperiod.TimePeriod
	TimePeriodID     types.Int     `db:"timeperiod_id"`
	ObjectFilter     filter.Filter `db:"-"`
	ObjectFilterExpr types.String  `db:"object_filter"`
	Escalations      map[int64]*Escalation
	Routes           map[int64]*Routing
}

// Meta provides a set of common metadata for the rule Routing and Escalation types.
type Meta struct {
	ID            int64          `db:"id"`
	RuleID        int64          `db:"rule_id"`
	Name          string         `db:"-"`
	NameRaw       sql.NullString `db:"name"`
	Condition     filter.Filter  `db:"-"`
	ConditionExpr sql.NullString `db:"condition"`
}

// MarshalLogObject implements zapcore.ObjectMarshaler interface.
//
// This allows us to use `zap.Inline(meta)` or `zap.Object("rule_escalation", meta)` wherever fine-grained
// logging context is needed, without having to add all the individual fields ourselves each time.
func (m *Meta) MarshalLogObject(encoder zapcore.ObjectEncoder) error {
	encoder.AddInt64("id", m.ID)
	encoder.AddInt64("rule_id", m.RuleID)
	encoder.AddString("condition", m.ConditionExpr.String)
	encoder.AddString("name", m.NameRaw.String)

	return nil
}

// RecipientMeta provides a set of common metadata for the rule EscalationRecipient or RoutingRecipient.
type RecipientMeta struct {
	ID            int64         `db:"id"`
	ChannelID     sql.NullInt64 `db:"channel_id"`
	recipient.Key `db:",inline"`
	Recipient     recipient.Recipient `db:"-"`
}

// ContactChannels stores a set of channel IDs for each set of individual contacts.
type ContactChannels map[*recipient.Contact]map[int64]bool

// LoadFromEscalationRecipients loads recipients channel of the specified escalation to the current map.
// You can provide this method a callback to control whether the channel of a specific contact should
// be loaded, and it will skip those for whom the callback returns false. Pass IsNotifiable for default actions.
func (ch ContactChannels) LoadFromEscalationRecipients(escalation *Escalation, t time.Time, isNotifiable func(recipient.Key) bool) {
	for _, escalationRecipient := range escalation.Recipients {
		ch.LoadRecipientChannel(escalationRecipient.RecipientMeta, t, isNotifiable)
	}
}

// LoadFromRoutingRecipients loads recipients channel of the specified rule routing to the current map.
// You can provide this method a callback to control whether the channel of a specific contact should
// be loaded, and it will skip those for whom the callback returns false. Pass IsNotifiable for default actions.
func (ch ContactChannels) LoadFromRoutingRecipients(routing *Routing, t time.Time, isNotifiable func(recipient.Key) bool) {
	for _, routingRecipient := range routing.Recipients {
		ch.LoadRecipientChannel(routingRecipient.RecipientMeta, t, isNotifiable)
	}
}

// LoadRecipientChannel loads recipient channel to the current map.
// You can provide this method a callback to control whether the channel of a specific contact should
// be loaded, and it will skip those for whom the callback returns false. Pass IsNotifiable for default actions.
func (ch ContactChannels) LoadRecipientChannel(recipients RecipientMeta, t time.Time, isNotifiable func(recipient.Key) bool) {
	if isNotifiable(recipients.Key) {
		for _, c := range recipients.Recipient.GetContactsAt(t) {
			if ch[c] == nil {
				ch[c] = make(map[int64]bool)
			}
			if recipients.ChannelID.Valid {
				ch[c][recipients.ChannelID.Int64] = true
			} else {
				ch[c][c.DefaultChannelID] = true
			}
		}
	}
}

// IsNotifiable checks whether the given recipient is notifiable and returns always true.
// This function is usually passed as an argument to ContactChannels.LoadFromEscalationRecipients whenever you do
// not want to perform any custom actions.
func IsNotifiable(_ recipient.Key) bool {
	return true
}
