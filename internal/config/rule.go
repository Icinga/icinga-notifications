package config

import (
	"fmt"
	"slices"

	"github.com/icinga/icinga-notifications/internal/rule"
)

// GetRulesFilterColumnsForSource returns a set of all filter columns used in the rules of the given source.
//
// The second return value indicates whether there are any rules without an object filter, in which case the events
// from the provided src should be processed nonetheless, even if they don't carry all the required filter columns
// unless it was explicitly requested to reject such events by the client.
func (r *RuntimeConfig) GetRulesFilterColumnsForSource(src *Source) (rule.FilterAttrsType, bool) {
	r.RLock()
	defer r.RUnlock()

	var columns rule.FilterAttrsType
	var hasRulesWithoutFilter bool
	for id := range src.RuleIDs() {
		eventRule, ok := r.Rules[id]
		if !ok {
			continue
		}
		columns = append(columns, eventRule.FilterColumns...)
		hasRulesWithoutFilter = hasRulesWithoutFilter || eventRule.ObjectFilter == nil
	}
	return columns, hasRulesWithoutFilter
}

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
			for _, src := range r.Sources {
				if src.Type == newElement.SourceType {
					src.appendRuleID(newElement.ID)
				}
			}
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

			if curElement.SourceType != update.SourceType {
				for _, src := range r.Sources {
					if src.Type == curElement.SourceType {
						src.deleteRuleID(curElement.ID)
					}
					if src.Type == update.SourceType {
						src.appendRuleID(update.ID)
					}
				}
				curElement.SourceType = update.SourceType
			}

			// ObjectFilter{,Expr} are being initialized by config.IncrementalConfigurableInitAndValidatable.
			curElement.ObjectFilter = update.ObjectFilter
			curElement.ObjectFilterExpr = update.ObjectFilterExpr
			curElement.FilterColumns = update.FilterColumns

			return nil
		},
		func(delElement *rule.Rule) error {
			// Detach the rule from all sources that reference it, so that they don't try to use it anymore.
			for _, src := range r.Sources {
				if src.Type == delElement.SourceType {
					src.deleteRuleID(delElement.ID)
				}
			}
			return nil
		},
	)

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
			curElement.Position = update.Position
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
				return recipient.ID == delElement.ID
			})
			return nil
		})
}
