package config

import (
	"fmt"
	"github.com/icinga/icinga-notifications/internal/rule"
	"slices"
)

// GetRulesFilterColumnsForSource returns a set of all filter columns used in the rules of the given source.
//
// This can be used by sources to determine which columns they need to provide for the events to be
// able to evaluate the rules of this source.
func (r *RuntimeConfig) GetRulesFilterColumnsForSource(src *Source) []string {
	r.RLock()
	defer r.RUnlock()

	var columns []string
	for _, id := range src.RuleIDs() {
		eventRule, ok := r.Rules[id]
		if !ok {
			continue
		}
		for column := range eventRule.FilterColumns {
			if !slices.Contains(columns, column) {
				columns = append(columns, column)
			}
		}
	}
	return columns
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

			newElement.Escalations = make(map[int64]*rule.Escalation)
			// If the source this rule belongs to is already known, add this rule to the source's rule list.
			// Otherwise, the rule will be added to that list when its source is being loaded.
			if src, ok := r.Sources[newElement.SourceID]; ok {
				src.ruleIDs = append(src.ruleIDs, newElement.ID)
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

			// ObjectFilter{,Expr} are being initialized by config.IncrementalConfigurableInitAndValidatable.
			curElement.ObjectFilter = update.ObjectFilter
			curElement.ObjectFilterExpr = update.ObjectFilterExpr
			curElement.FilterColumns = update.FilterColumns

			return nil
		},
		func(delElement *rule.Rule) error {
			// If the source this rule belongs to is already known, remove this rule from the source's rule list.
			// Otherwise, there's nothing more to do!
			if src, ok := r.Sources[delElement.SourceID]; ok {
				src.ruleIDs = slices.DeleteFunc(src.ruleIDs, func(id int64) bool {
					return id == delElement.ID
				})
			}
			return nil
		},
	)

	incrementalApplyPending(
		r,
		&r.ruleEscalations, &r.configChange.ruleEscalations,
		func(newElement *rule.Escalation) error {
			elementRule, ok := r.Rules[newElement.RuleID]
			if !ok {
				return fmt.Errorf("rule escalation refers unknown rule %d", newElement.RuleID)
			}

			elementRule.Escalations[newElement.ID] = newElement
			return nil
		},
		func(curElement, update *rule.Escalation) error {
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
		func(delElement *rule.Escalation) error {
			elementRule, ok := r.Rules[delElement.RuleID]
			if !ok {
				return nil
			}

			delete(elementRule.Escalations, delElement.ID)
			return nil
		})

	incrementalApplyPending(
		r,
		&r.ruleEscalationRecipients, &r.configChange.ruleEscalationRecipients,
		func(newElement *rule.EscalationRecipient) error {
			newElement.Recipient = r.GetRecipient(newElement.Key)
			if newElement.Recipient == nil {
				return fmt.Errorf("rule escalation recipient is missing or unknown")
			}

			escalation := r.GetRuleEscalation(newElement.EscalationID)
			if escalation == nil {
				return fmt.Errorf("rule escalation recipient refers to unknown escalation %d", newElement.EscalationID)
			}
			escalation.Recipients = append(escalation.Recipients, newElement)

			return nil
		},
		nil,
		func(delElement *rule.EscalationRecipient) error {
			escalation := r.GetRuleEscalation(delElement.EscalationID)
			if escalation == nil {
				return nil
			}

			escalation.Recipients = slices.DeleteFunc(escalation.Recipients, func(recipient *rule.EscalationRecipient) bool {
				return recipient.ID == delElement.ID
			})
			return nil
		})
}
