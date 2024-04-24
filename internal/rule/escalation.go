package rule

import (
	"database/sql"
	"github.com/icinga/icinga-notifications/internal/recipient"
	"go.uber.org/zap/zapcore"
	"strings"
	"time"
)

type Escalation struct {
	Meta          `db:",inline"`
	FallbackForID sql.NullInt64 `db:"fallback_for"`
	Fallbacks     []*Escalation

	Recipients []*EscalationRecipient
}

func (e *Escalation) MarshalLogObject(encoder zapcore.ObjectEncoder) error {
	if err := e.Meta.MarshalLogObject(encoder); err != nil {
		return err
	}

	encoder.AddInt64("fallback_for", e.FallbackForID.Int64)
	return nil
}

func (e *Escalation) DisplayName() string {
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
	RecipientMeta `db:",inline"`
	EscalationID  int64 `db:"rule_escalation_id"`
}

func (r *EscalationRecipient) TableName() string {
	return "rule_escalation_recipient"
}

type ContactChannelPair struct {
	Contact   *recipient.Contact
	ChannelID sql.NullInt64
}
