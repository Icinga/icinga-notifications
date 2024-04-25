package rule

import (
	"database/sql"
	"fmt"
	"github.com/icinga/icinga-notifications/internal/config/baseconf"
	"github.com/icinga/icinga-notifications/internal/filter"
	"github.com/icinga/icinga-notifications/internal/recipient"
	"go.uber.org/zap/zapcore"
	"strings"
	"time"
)

type Entry struct {
	baseconf.IncrementalPkDbEntry[int64] `db:",inline"`
	RuleID                               int64          `db:"rule_id"`
	NameRaw                              sql.NullString `db:"name"`
	Condition                            filter.Filter  `db:"-"`
	ConditionExpr                        sql.NullString `db:"condition"`
	FallbackForID                        sql.NullInt64  `db:"fallback_for"`

	Fallbacks  []*Entry          `db:"-"`
	Recipients []*EntryRecipient `db:"-"`
}

// IncrementalInitAndValidate implements the config.IncrementalConfigurableInitAndValidatable interface.
func (e *Entry) IncrementalInitAndValidate() error {
	if e.ConditionExpr.Valid {
		cond, err := filter.Parse(e.ConditionExpr.String)
		if err != nil {
			return err
		}

		e.Condition = cond
	}

	if e.FallbackForID.Valid {
		// TODO: implement fallbacks (needs extra validation: mismatching rule_id, cycles)
		return fmt.Errorf("ignoring fallback escalation (not yet implemented)")
	}

	return nil
}

// MarshalLogObject implements the zapcore.ObjectMarshaler interface.
//
// This allows us to use `zap.Inline(escalation)` or `zap.Object("rule_escalation", escalation)` wherever
// fine-grained logging context is needed, without having to add all the individual fields ourselves each time.
// https://pkg.go.dev/go.uber.org/zap/zapcore#ObjectMarshaler
func (e *Entry) MarshalLogObject(encoder zapcore.ObjectEncoder) error {
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

// Eval evaluates the configured escalation/routing filter for the provided filter.
// Returns always true if there are no configured escalation/routing conditions.
func (e *Entry) Eval(filterable filter.Filterable) (bool, error) {
	if e.Condition == nil {
		return true, nil
	}

	return e.Condition.Eval(filterable)
}

func (e *Entry) DisplayName() string {
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

func (e *Entry) GetContactsAt(t time.Time) []ContactChannelPair {
	var pairs []ContactChannelPair

	for _, r := range e.Recipients {
		for _, c := range r.Recipient.GetContactsAt(t) {
			pairs = append(pairs, ContactChannelPair{c, r.ChannelID})
		}
	}

	return pairs
}

func (e *Entry) TableName() string {
	return "rule_entry"
}

// EntryRecipient links a recipient to a rule entry, optionally with a specific channel.
//
// This allows to override the recipient's default channel for any given rule entry this recipient is linked to.
// Otherwise, the recipient's default channel is used to deliver notifications.
type EntryRecipient struct {
	baseconf.IncrementalPkDbEntry[int64] `db:",inline"`

	EntryID       int64         `db:"rule_entry_id"`
	ChannelID     sql.NullInt64 `db:"channel_id"`
	recipient.Key `db:",inline"`
	Recipient     recipient.Recipient `db:"-"`
}

// MarshalLogObject implements the zapcore.ObjectMarshaler interface.
func (r *EntryRecipient) MarshalLogObject(encoder zapcore.ObjectEncoder) error {
	encoder.AddInt64("id", r.ID)
	encoder.AddInt64("rule_escalation_id", r.EntryID)
	if r.ChannelID.Valid {
		encoder.AddInt64("channel_id", r.ChannelID.Int64)
	}
	return r.Key.MarshalLogObject(encoder)
}

func (r *EntryRecipient) TableName() string {
	return "rule_entry_recipient"
}

type ContactChannelPair struct {
	Contact   *recipient.Contact
	ChannelID sql.NullInt64
}
