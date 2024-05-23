package rule

import (
	"database/sql"
	"github.com/icinga/icinga-notifications/internal/filter"
	"github.com/icinga/icinga-notifications/internal/recipient"
	"go.uber.org/zap/zapcore"
	"strings"
	"time"
)

type Escalation struct {
	ID            int64          `db:"id"`
	RuleID        int64          `db:"rule_id"`
	NameRaw       sql.NullString `db:"name"`
	Condition     filter.Filter  `db:"-"`
	ConditionExpr sql.NullString `db:"condition"`
	FallbackForID sql.NullInt64  `db:"fallback_for"`
	Fallbacks     []*Escalation  `db:"-"`

	Recipients []*EscalationRecipient `db:"-"`
}

// MarshalLogObject implements the zapcore.ObjectMarshaler interface.
//
// This allows us to use `zap.Inline(escalation)` or `zap.Object("rule_escalation", escalation)` wherever
// fine-grained logging context is needed, without having to add all the individual fields ourselves each time.
// https://pkg.go.dev/go.uber.org/zap/zapcore#ObjectMarshaler
func (e *Escalation) MarshalLogObject(encoder zapcore.ObjectEncoder) error {
	encoder.AddInt64("id", e.ID)
	encoder.AddInt64("rule_id", e.RuleID)
	encoder.AddString("name", e.DisplayName())

	if e.ConditionExpr.Valid && e.ConditionExpr.String != "" {
		encoder.AddString("condition", e.ConditionExpr.String)
	}
	if e.FallbackForID.Valid && e.FallbackForID.Int64 != 0 {
		encoder.AddInt64("fallback_for", e.FallbackForID.Int64)
	}

	return nil
}

// Eval evaluates the configured escalation filter for the provided filter.
// Returns always true if there are no configured escalation conditions.
func (e *Escalation) Eval(filterable *EscalationFilter) (bool, error) {
	if e.Condition == nil {
		return true, nil
	}

	return e.Condition.Eval(filterable)
}

func (e *Escalation) DisplayName() string {
	if e.NameRaw.Valid && e.NameRaw.String != "" {
		return e.NameRaw.String
	}

	var recipients []string

	for _, r := range e.Recipients {
		switch v := r.Recipient.(type) {
		case *recipient.Contact:
			recipients = append(recipients, "[C] "+v.FullName)
		case *recipient.Group:
			recipients = append(recipients, "[G] "+v.Name)
		case *recipient.Schedule:
			recipients = append(recipients, "[S] "+v.Name)
		}
	}

	if len(recipients) == 0 {
		return "(no recipients)"
	}

	return strings.Join(recipients, ", ")
}

func (e *Escalation) GetContactsAt(t time.Time) []ContactChannelPair {
	var pairs []ContactChannelPair

	for _, r := range e.Recipients {
		for _, c := range r.Recipient.GetContactsAt(t) {
			pairs = append(pairs, ContactChannelPair{c, r.ChannelID})
		}
	}

	return pairs
}

func (e *Escalation) TableName() string {
	return "rule_escalation"
}

type EscalationRecipient struct {
	ID            int64         `db:"id"`
	EscalationID  int64         `db:"rule_escalation_id"`
	ChannelID     sql.NullInt64 `db:"channel_id"`
	recipient.Key `db:",inline"`
	Recipient     recipient.Recipient `db:"-"`
}

func (r *EscalationRecipient) TableName() string {
	return "rule_escalation_recipient"
}

type ContactChannelPair struct {
	Contact   *recipient.Contact
	ChannelID sql.NullInt64
}
