package config

import (
	"fmt"
	"github.com/icinga/icinga-notifications/internal/rule"
	"go.uber.org/zap"
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

// EvalOptions specifies optional callbacks that are executed upon certain filter evaluation events.
//
// The EvalOptions type is used to configure the behaviour of the evaluation process when evaluating
// filter expressions against a set of [rule.Escalation] entries. It allows you to hook into specific
// events during the evaluation process and perform custom actions based on your requirements.
type EvalOptions struct {
	// OnPreEvaluate can be used to perform some actions before evaluating the filter for the current entry.
	//
	// This callback receives the current [rule.Escalation] entry as an argument, which is about to be
	// evaluated. If this callback returns "false", the filter evaluation for the current entry is skipped,
	// and the evaluation continues with the next one. If it returns "true" or is nil, the filter evaluation
	// proceeds as normal.
	//
	// Note that if you skip the evaluation of an entry using this callback, the OnFilterMatch callback
	// will not be triggered for that entry, even if its filter would have matched on the filterable object.
	OnPreEvaluate func(*rule.Escalation) bool

	// OnError is called when an error occurs during the filter evaluation.
	//
	// This callback receives the current [rule.Escalation] entry and the error that occurred as arguments.
	// By default, the evaluation continues even if some entries fail, but you can override this behaviour
	// by returning "false" in your handler, which aborts the evaluation prematurely. If you return "true"
	// or if this callback is nil, the evaluation continues with the remaining entries.
	//
	// Note that if you choose to abort the evaluation by returning "false", the OnAllConfigEvaluated callback
	// will not be triggered, as the evaluation did not complete successfully.
	OnError func(*rule.Escalation, error) bool

	// OnFilterMatch is called when the filter for an entry matches successfully.
	//
	// This callback receives the current [rule.Escalation] entry as an argument. If this callback returns
	// an error, the evaluation is aborted prematurely, and the error is returned. Otherwise, the evaluation
	// continues with the remaining entries.
	//
	// Note that if you return an error from this callback, the OnAllConfigEvaluated callback will not be triggered,
	// as the evaluation did not complete successfully.
	OnFilterMatch func(*rule.Escalation) error

	// OnAllConfigEvaluated is called after all configured entries have been evaluated.
	//
	// This callback receives a value of type [time.Duration] derived from the evaluation process as an argument.
	// This callback is guaranteed to be called if none of the individual evaluation callbacks return prematurely
	// with an error. If any of the callbacks return prematurely, this callback will not be triggered.
	//
	// The [time.Duration] argument can be used to indicate a duration after which a re-evaluation might be necessary,
	// based on the evaluation results. This is optional and can be ignored if not needed.
	OnAllConfigEvaluated func(time.Duration)
}

// RuleEntries is a map of rule.Escalation entries, keyed by their ID.
//
// This type is used to store the results of evaluating rule.Escalation entries against a filterable object.
// It allows for efficient lookups and ensures that each entry is unique based on its ID.
type RuleEntries map[int64]*rule.Escalation

// Evaluate evaluates the rule.Escalation entries against the provided filterable object.
//
// Depending on the provided EvalOptions, various callbacks may be triggered during the evaluation process.
// The results of the evaluation are stored in the RuleEntries map, with entries that match the filter
// being added to the map.
//
// If an error occurs during the evaluation of an entry, the OnError callback is triggered (if provided).
// If this callback returns "false", the evaluation is aborted prematurely, and the error is returned.
// Otherwise, the evaluation continues with the remaining entries.
func (re RuleEntries) Evaluate(res Resources, filterable *rule.EscalationFilter, rules map[int64]struct{}, opts EvalOptions) error {
	retryAfter := rule.RetryNever

	for ruleID := range rules {
		r := res.RuntimeConfig.Rules[ruleID]
		if r == nil {
			res.Logger.Debugw("Referenced rule does not exist", zap.Int64("rule_id", ruleID))
			continue
		}

		for _, entry := range r.Escalations {
			if opts.OnPreEvaluate != nil && !opts.OnPreEvaluate(entry) {
				continue
			}

			if matched, err := entry.Eval(filterable); err != nil {
				if opts.OnError != nil && !opts.OnError(entry, err) {
					return err
				}
			} else if !matched {
				incidentAgeFilter := filterable.ReevaluateAfter(entry.Condition)
				retryAfter = min(retryAfter, incidentAgeFilter)
			} else {
				if opts.OnFilterMatch != nil {
					if err := opts.OnFilterMatch(entry); err != nil {
						return err
					}
				}
				re[entry.ID] = entry
			}
		}
	}

	if opts.OnAllConfigEvaluated != nil {
		opts.OnAllConfigEvaluated(retryAfter)
	}

	return nil
}
