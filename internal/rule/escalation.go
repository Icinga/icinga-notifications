package rule

import (
	"database/sql"
	"github.com/icinga/icinga-notifications/internal/filter"
	"github.com/icinga/icinga-notifications/internal/recipient"
	"strings"
	"time"
)

type EscalationTemplate struct {
	ID            int64          `db:"id"`
	RuleID        int64          `db:"rule_id"`
	Name          string         `db:"-"`
	NameRaw       sql.NullString `db:"name"`
	Condition     filter.Filter  `db:"-"`
	ConditionExpr sql.NullString `db:"condition"`
	FallbackForID sql.NullInt64  `db:"fallback_for"`
	Fallbacks     []*Escalation

	Recipients []*EscalationRecipient
}

func (e *EscalationTemplate) DisplayName() string {
	if e.Name != "" {
		return e.Name
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

func (e *EscalationTemplate) GetContactsAt(t time.Time) []ContactChannelPair {
	var pairs []ContactChannelPair

	for _, r := range e.Recipients {
		for _, c := range r.Recipient.GetContactsAt(t) {
			pairs = append(pairs, ContactChannelPair{c, r.ChannelID})
		}
	}

	return pairs
}

const (
	TypeEscalation         = "Escalation"
	TypeNonStateEscalation = "NonStateEscalation"
)

type Escalation struct {
	*EscalationTemplate
}

func (e *Escalation) TableName() string {
	return "rule_escalation"
}

type NonStateEscalation struct {
	*EscalationTemplate
}

func (e *NonStateEscalation) TableName() string {
	return "rule_non_state_escalation"
}

type EscalationRecipient struct {
	ID                   int64         `db:"id"`
	EscalationID         sql.NullInt64 `db:"rule_escalation_id"`
	NonStateEscalationID sql.NullInt64 `db:"rule_non_state_escalation_id"`
	ChannelID            sql.NullInt64 `db:"channel_id"`
	recipient.Key        `db:",inline"`
	Recipient            recipient.Recipient
}

func (r *EscalationRecipient) TableName() string {
	return "rule_escalation_recipient"
}

type ContactChannelPair struct {
	Contact   *recipient.Contact
	ChannelID sql.NullInt64
}
