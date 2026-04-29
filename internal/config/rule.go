package config

import (
	"fmt"
	"github.com/icinga/icinga-notifications/internal/rule"
	"slices"
	"time"
)

// SourceRuleVersion for SourceRulesInfo, consisting of two numbers, one static and one incrementable.
type SourceRuleVersion struct {
	Major int64
	Minor int64
}

// NewSourceRuleVersion creates a new source version based on the current timestamp and a zero counter.
func NewSourceRuleVersion() SourceRuleVersion {
	return SourceRuleVersion{
		Major: time.Now().UnixMilli(),
		Minor: 0,
	}
}

// Increment the version counter.
func (sourceVersion *SourceRuleVersion) Increment() {
	sourceVersion.Minor++
}

// String implements fmt.Stringer and returns a pretty-printable representation.
func (sourceVersion *SourceRuleVersion) String() string {
	return fmt.Sprintf("%x-%x", sourceVersion.Major, sourceVersion.Minor)
}

// SourceRulesInfo holds information about the rules associated with a specific source.
type SourceRulesInfo struct {
	// Version is the version of the rules for the source.
	//
	// Multiple source's versions are independent of another.
	Version SourceRuleVersion

	// RuleIDs is a list of rule IDs associated with a specific source.
	//
	// It is used to quickly access the rules for a specific source without iterating over all rules.
	RuleIDs []int64
}

// applyPendingRules synchronizes changed rules.
func (r *RuntimeConfig) applyPendingRules() {
	// Keep track of sources the rules were updated for, so we can update their version later.
	updatedSources := make(map[int64]struct{})

	if r.RulesBySource == nil {
		r.RulesBySource = make(map[int64]*SourceRulesInfo)
	}

	addToRulesBySource := func(elem *rule.Rule) {
		if sourceInfo, ok := r.RulesBySource[elem.SourceID]; ok {
			sourceInfo.RuleIDs = append(sourceInfo.RuleIDs, elem.ID)
		} else {
			r.RulesBySource[elem.SourceID] = &SourceRulesInfo{
				Version: NewSourceRuleVersion(),
				RuleIDs: []int64{elem.ID},
			}
		}

		updatedSources[elem.SourceID] = struct{}{}
	}

	delFromRulesBySource := func(elem *rule.Rule) {
		if sourceInfo, ok := r.RulesBySource[elem.SourceID]; ok {
			sourceInfo.RuleIDs = slices.DeleteFunc(sourceInfo.RuleIDs, func(id int64) bool {
				return id == elem.ID
			})
		}

		updatedSources[elem.SourceID] = struct{}{}
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

			addToRulesBySource(newElement)

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

			if curElement.SourceID != update.SourceID {
				delFromRulesBySource(curElement)
				curElement.SourceID = update.SourceID
				addToRulesBySource(curElement)
			}

			// ObjectFilter{,Expr} are being initialized by config.IncrementalConfigurableInitAndValidatable.
			curElement.ObjectFilterExpr = update.ObjectFilterExpr

			updatedSources[curElement.SourceID] = struct{}{}

			return nil
		},
		func(delElement *rule.Rule) error {
			delFromRulesBySource(delElement)

			return nil
		},
	)

	// After applying the rules, we need to update the version of the sources that were modified.
	// This is done to ensure that the version is incremented whenever a rule is added, modified,
	// or deleted only once per applyPendingRules call, even if multiple rules from the same source
	// were changed.
	for sourceID := range updatedSources {
		if sourceInfo, ok := r.RulesBySource[sourceID]; ok {
			sourceInfo.Version.Increment()
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
