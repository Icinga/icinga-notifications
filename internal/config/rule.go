package config

import (
	"fmt"
	"github.com/icinga/icinga-notifications/internal/rule"
	"slices"
)

func (r *RuntimeConfig) applyPendingRules() {
	incrementalApplyPending(
		r,
		&r.Rules, &r.configChange.Rules,
		func(newElement *rule.Rule) error {
			if newElement.TimePeriodID.Valid {
				timePeriod, ok := r.TimePeriods[newElement.TimePeriodID.Int64]
				if !ok {
					return fmt.Errorf("rule refers unknown time period %d", newElement.TimePeriodID.Int64)
				}

				newElement.TimePeriod = timePeriod
			}

			newElement.Escalations = make(map[int64]*rule.Escalation)
			return nil
		},
		func(curElement, update *rule.Rule) error {
			curElement.IsActive = update.IsActive
			curElement.Name = update.Name
			curElement.ObjectFilter = update.ObjectFilter
			curElement.ObjectFilterExpr = update.ObjectFilterExpr

			if curElement.TimePeriodID != update.TimePeriodID {
				if update.TimePeriodID.Valid {
					timePeriod, ok := r.TimePeriods[update.TimePeriodID.Int64]
					if !ok {
						return fmt.Errorf("updated rule refers unknown time period %d", update.TimePeriodID.Int64)
					}

					curElement.TimePeriod = timePeriod
				} else {
					curElement.TimePeriod = nil
				}
				curElement.TimePeriodID = update.TimePeriodID
			}

			return nil
		},
		nil)

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
			curElement.NameRaw = update.NameRaw
			curElement.Condition = update.Condition
			// TODO: synchronize FallbackForID/Fallback when implemented
			return nil
		},
		func(delElement *rule.Escalation) error {
			elementRule, ok := r.Rules[delElement.RuleID]
			if !ok {
				return fmt.Errorf("rule escalation refers unknown rule %d", delElement.RuleID)
			}

			delete(elementRule.Escalations, delElement.ID)
			return nil
		})

	incrementalApplyPending(
		r,
		&r.ruleEscalationRecipients, &r.configChange.ruleEscalationRecipients,
		func(newElement *rule.EscalationRecipient) error {
			ok := false
			if newElement.ContactID.Valid {
				newElement.Recipient, ok = r.Contacts[newElement.ContactID.Int64]
			} else if newElement.GroupID.Valid {
				newElement.Recipient, ok = r.Groups[newElement.GroupID.Int64]
			} else if newElement.ScheduleID.Valid {
				newElement.Recipient, ok = r.Schedules[newElement.ScheduleID.Int64]
			}
			if !ok {
				return fmt.Errorf("rule escalation recipient is missing or unknown")
			}

			ruleFound := false
			for id, elementRule := range r.Rules {
				_, ok := elementRule.Escalations[newElement.EscalationID]
				if ok {
					newElement.RuleID = id
					ruleFound = true
					break
				}
			}
			if !ruleFound {
				return fmt.Errorf("rule escalation recipient cannot be mapped to a rule")
			}

			escalation := r.Rules[newElement.RuleID].Escalations[newElement.EscalationID]
			escalation.Recipients = append(escalation.Recipients, newElement)
			return nil
		},
		nil,
		func(delElement *rule.EscalationRecipient) error {
			elementRule, ok := r.Rules[delElement.RuleID]
			if !ok {
				return fmt.Errorf("escalation recipient refers to unknown rule %d", delElement.RuleID)
			}

			escalation, ok := elementRule.Escalations[delElement.EscalationID]
			if !ok {
				return fmt.Errorf("escalation recipient refers to unknown escalation %d", delElement.EscalationID)
			}

			escalation.Recipients = slices.DeleteFunc(escalation.Recipients, func(recipient *rule.EscalationRecipient) bool {
				return recipient.EscalationID == delElement.EscalationID
			})
			return nil
		})
}
