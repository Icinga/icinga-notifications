package config

import (
	"fmt"
	"github.com/icinga/icinga-notifications/internal/rule"
	"slices"
)

// SourceRulesInfo holds information about the rules associated with a specific source.
type SourceRulesInfo struct {
	// Version is the version of the rules for the source.
	//
	// It is a monotonically increasing number that is updated whenever a rule is added, modified, or deleted.
	// With each state change of the rules referenced by RuleIDs, the Version will always be incremented
	// by 1, effectively starting at 1.
	//
	// The Version is not unique across different sources, but it is unique for a specific source at a specific time.
	Version int64

	// RuleIDs is a list of rule IDs associated with a specific source.
	//
	// It is used to quickly access the rules for a specific source without iterating over all rules.
	RuleIDs []int64
}

// RuleSet represents the set of event rules currently loaded in the runtime configuration.
// It contains the rules and their associated information, such as the source they belong to and their version.
type RuleSet struct {
	Rules map[int64]*rule.Rule // rules is a map of rule.Rule by their ID.

	RulesBySource map[int64]*SourceRulesInfo // RulesBySource maps source IDs to their rules and version information.
}

// applyPendingRules synchronizes changed rules.
func (r *RuntimeConfig) applyPendingRules() {
	// Keep track of sources the rules were updated for, so we can update their version later.
	updatedSources := make(map[int64]struct{})

	if r.RulesBySource == nil {
		r.RulesBySource = make(map[int64]*SourceRulesInfo)
	}

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

			// Add the new rule to the per-source rules cache.
			if sourceInfo, ok := r.RulesBySource[newElement.SourceID]; ok {
				sourceInfo.RuleIDs = append(sourceInfo.RuleIDs, newElement.ID)
			} else {
				r.RulesBySource[newElement.SourceID] = &SourceRulesInfo{RuleIDs: []int64{newElement.ID}}
			}

			updatedSources[newElement.SourceID] = struct{}{}

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
			curElement.ObjectFilterExpr = update.ObjectFilterExpr

			updatedSources[curElement.SourceID] = struct{}{}

			return nil
		},
		func(delElement *rule.Rule) error {
			if sourceInfo, ok := r.RulesBySource[delElement.SourceID]; ok {
				sourceInfo.RuleIDs = slices.DeleteFunc(sourceInfo.RuleIDs, func(id int64) bool {
					return id == delElement.ID
				})
			}

			updatedSources[delElement.SourceID] = struct{}{}

			return nil
		},
	)

	// After applying the rules, we need to update the version of the sources that were modified.
	// This is done to ensure that the version is incremented whenever a rule is added, modified,
	// or deleted only once per applyPendingRules call, even if multiple rules from the same source
	// were changed.
	for sourceID := range updatedSources {
		if sourceInfo, ok := r.RulesBySource[sourceID]; ok {
			sourceInfo.Version++
		}
	}

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
