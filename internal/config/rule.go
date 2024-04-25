package config

import (
	"fmt"
	"github.com/icinga/icinga-notifications/internal/rule"
	"slices"
)

// applyPendingRules synchronizes changed rules.
func (r *RuntimeConfig) applyPendingRules() {
	incrementalApplyPending(
		r,
		&r.Rules, &r.configChange.Rules,
		func(newElement *rule.Rule) error {
			if newElement.TimePeriodID.Valid {
				tp, ok := r.TimePeriods[newElement.TimePeriodID.Int64]
				if !ok {
					return fmt.Errorf("rule refers unknown time period %d", newElement.TimePeriodID.Int64)
				}
				newElement.TimePeriod = tp
			}

			newElement.Entries = make(map[int64]*rule.Entry)
			return nil
		},
		func(curElement, update *rule.Rule) error {
			curElement.ChangedAt = update.ChangedAt
			curElement.Name = update.Name

			curElement.TimePeriodID = update.TimePeriodID
			if curElement.TimePeriodID.Valid {
				tp, ok := r.TimePeriods[curElement.TimePeriodID.Int64]
				if !ok {
					return fmt.Errorf("rule refers unknown time period %d", curElement.TimePeriodID.Int64)
				}
				curElement.TimePeriod = tp
			} else {
				curElement.TimePeriod = nil
			}

			// ObjectFilter{,Expr} are being initialized by config.IncrementalConfigurableInitAndValidatable.
			curElement.ObjectFilter = update.ObjectFilter
			curElement.ObjectFilterExpr = update.ObjectFilterExpr

			return nil
		},
		nil)

	incrementalApplyPending(
		r,
		&r.ruleEntries, &r.configChange.ruleEntries,
		func(newElement *rule.Entry) error {
			elementRule, ok := r.Rules[newElement.RuleID]
			if !ok {
				return fmt.Errorf("rule escalation refers unknown rule %d", newElement.RuleID)
			}

			elementRule.Entries[newElement.ID] = newElement
			return nil
		},
		func(curElement, update *rule.Entry) error {
			if curElement.RuleID != update.RuleID {
				return errRemoveAndAddInstead
			}

			curElement.ChangedAt = update.ChangedAt
			curElement.NameRaw = update.NameRaw
			// Condition{,Expr} are being initialized by config.IncrementalConfigurableInitAndValidatable.
			curElement.Condition = update.Condition
			curElement.ConditionExpr = update.ConditionExpr
			// TODO: synchronize Fallback{ForID,s} when implemented

			return nil
		},
		func(delElement *rule.Entry) error {
			elementRule, ok := r.Rules[delElement.RuleID]
			if !ok {
				return nil
			}

			delete(elementRule.Entries, delElement.ID)
			return nil
		})

	incrementalApplyPending(
		r,
		&r.ruleEntryRecipients, &r.configChange.ruleEntryRecipients,
		func(newElement *rule.EntryRecipient) error {
			newElement.Recipient = r.GetRecipient(newElement.Key)
			if newElement.Recipient == nil {
				return fmt.Errorf("rule escalation recipient is missing or unknown")
			}

			escalation := r.GetRuleEntry(newElement.EntryID)
			if escalation == nil {
				return fmt.Errorf("rule escalation recipient refers to unknown escalation %d", newElement.EntryID)
			}
			escalation.Recipients = append(escalation.Recipients, newElement)

			return nil
		},
		nil,
		func(delElement *rule.EntryRecipient) error {
			escalation := r.GetRuleEntry(delElement.EntryID)
			if escalation == nil {
				return nil
			}

			escalation.Recipients = slices.DeleteFunc(escalation.Recipients, func(recipient *rule.EntryRecipient) bool {
				return recipient.EntryID == delElement.EntryID
			})
			return nil
		})
}
