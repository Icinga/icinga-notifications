package config

import (
	"fmt"
	"slices"
	"time"

	"github.com/icinga/icinga-notifications/internal/rule"
)

// SourceRulesInfo holds information about the rules associated with a specific source.
type SourceRulesInfo struct {
	// Version is the version of the rules for the source.
	//
	// It is a monotonically increasing number that is updated whenever a rule is added, modified, or deleted.
	// With each state change of the rules referenced by RuleIDs, the Version will always be incremented
	// by 1, starting from 0. When there are no configured rules for the source, Version will be reset to 0.
	//
	// The Version is not unique across different sources, but it is unique for a specific source at a specific time.
	Version uint64

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
			updatedSources[newElement.SourceID] = struct{}{}
			if r.RulesBySource == nil {
				r.RulesBySource = make(map[int64]*SourceRulesInfo)
			}

			// Add the new rule to the per-source rules cache.
			if sourceInfo := r.RulesBySource[newElement.SourceID]; sourceInfo == nil {
				r.RulesBySource[newElement.SourceID] = &SourceRulesInfo{RuleIDs: []int64{newElement.ID}}
			} else {
				sourceInfo.RuleIDs = append(sourceInfo.RuleIDs, newElement.ID)
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
			curElement.ObjectFilterExpr = update.ObjectFilterExpr
			updatedSources[curElement.SourceID] = struct{}{}

			return nil
		},
		func(delElement *rule.Rule) error {
			if sourceInfo, ok := r.RulesBySource[delElement.SourceID]; ok {
				sourceInfo.RuleIDs = slices.DeleteFunc(sourceInfo.RuleIDs, func(id int64) bool {
					return id == delElement.ID
				})
				if len(sourceInfo.RuleIDs) == 0 {
					delete(r.RulesBySource, delElement.SourceID) // Remove the source if no rules are left.
				}
			}
			return nil
		},
	)

	// After applying the rules, we need to update the version of the sources that were modified.
	// This is done to ensure that the version is incremented whenever a rule is added, modified,
	// or deleted only once per applyPendingRules call, even if multiple rules from the same source
	// were changed.
	for sourceID := range updatedSources {
		if r.RulesBySource != nil {
			if sourceInfo, ok := r.RulesBySource[sourceID]; ok {
				// Invariant: len(sourceInfo.RuleIDs) > 0 if the source exists in RulesBySource (see delete above).
				sourceInfo.Version++
			}
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

// Evaluate evaluates the rule.Escalation entries associated with the given rule IDs against the provided filterable object.
//
// The Evaluate method iterates over the specified rule IDs, retrieves the corresponding rule.Escalation entries
// from the RuntimeConfig, and evaluates their filters against the provided filterable object. It uses the
// provided EvalOptions to handle various events during the evaluation process.
//
// Returns an error if any of the evaluation callbacks return an error or if there are issues during the evaluation.
func (re RuleEntries) Evaluate(r *RuntimeConfig, filterable *rule.EscalationFilter, rules map[int64]struct{}, opts EvalOptions) error {
	retryAfter := rule.RetryNever

	for ruleID := range rules {
		ru := r.Rules[ruleID]
		if ru == nil {
			// It would be appropriate to have a debug log here, but unfortunately we don't have access to a logger.
			continue
		}

		for _, entry := range ru.Escalations {
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
