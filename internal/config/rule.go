package config

import (
	"context"
	"github.com/icinga/icinga-notifications/internal/filter"
	"github.com/icinga/icinga-notifications/internal/rule"
	"github.com/jmoiron/sqlx"
	"go.uber.org/zap"
	"slices"
)

func (r *RuntimeConfig) fetchRules(ctx context.Context, tx *sqlx.Tx) error {
	var rulePtr *rule.Rule
	stmt := r.db.BuildSelectStmt(rulePtr, rulePtr)
	r.logger.Debugf("Executing query %q", stmt)

	var rules []*rule.Rule
	if err := tx.SelectContext(ctx, &rules, stmt); err != nil {
		r.logger.Errorln(err)
		return err
	}

	rulesByID := make(map[int64]*rule.Rule)
	for _, ru := range rules {
		ruleLogger := r.logger.With(zap.Inline(ru))

		if ru.ObjectFilterExpr.Valid {
			f, err := filter.Parse(ru.ObjectFilterExpr.String)
			if err != nil {
				ruleLogger.Warnw("ignoring rule as parsing object_filter failed", zap.Error(err))
				continue
			}

			ru.ObjectFilter = f
		}

		ru.Escalations = make(map[int64]*rule.Escalation)

		rulesByID[ru.ID] = ru
		ruleLogger.Debugw("loaded rule config")
	}

	var escalationPtr *rule.Escalation
	stmt = r.db.BuildSelectStmt(escalationPtr, escalationPtr)
	r.logger.Debugf("Executing query %q", stmt)

	var escalations []*rule.Escalation
	if err := tx.SelectContext(ctx, &escalations, stmt); err != nil {
		r.logger.Errorln(err)
		return err
	}

	escalationsByID := make(map[int64]*rule.Escalation)
	for _, escalation := range escalations {
		escalationLogger := r.logger.With(zap.Inline(escalation))

		rule := rulesByID[escalation.RuleID]
		if rule == nil {
			escalationLogger.Warnw("ignoring escalation for unknown rule_id")
			continue
		}

		if escalation.ConditionExpr.Valid {
			cond, err := filter.Parse(escalation.ConditionExpr.String)
			if err != nil {
				escalationLogger.Warnw("ignoring escalation, failed to parse condition", zap.Error(err))
				continue
			}

			escalation.Condition = cond
		}

		if escalation.FallbackForID.Valid {
			// TODO: implement fallbacks (needs extra validation: mismatching rule_id, cycles)
			escalationLogger.Warnw("ignoring fallback escalation (not yet implemented)")
			continue
		}

		rule.Escalations[escalation.ID] = escalation
		escalationsByID[escalation.ID] = escalation
		escalationLogger.Debugw("loaded escalation config")
	}

	var recipientPtr *rule.EscalationRecipient
	stmt = r.db.BuildSelectStmt(recipientPtr, recipientPtr)
	r.logger.Debugf("Executing query %q", stmt)

	var recipients []*rule.EscalationRecipient
	if err := tx.SelectContext(ctx, &recipients, stmt); err != nil {
		r.logger.Errorln(err)
		return err
	}

	for _, recipient := range recipients {
		recipientLogger := r.logger.With(
			zap.Int64("id", recipient.ID),
			zap.Int64("escalation_id", recipient.EscalationID),
			zap.Int64("channel_id", recipient.ChannelID.Int64))

		escalation := escalationsByID[recipient.EscalationID]
		if escalation == nil {
			recipientLogger.Warnw("ignoring recipient for unknown escalation")
		} else {
			escalation.Recipients = append(escalation.Recipients, recipient)
			recipientLogger.Debugw("loaded escalation recipient config")
		}
	}

	if r.Rules != nil {
		// mark no longer existing rules for deletion
		for id := range r.Rules {
			if _, ok := rulesByID[id]; !ok {
				rulesByID[id] = nil
			}
		}
	}

	r.pending.Rules = rulesByID

	return nil
}

func (r *RuntimeConfig) applyPendingRules() {
	if r.Rules == nil {
		r.Rules = make(map[int64]*rule.Rule)
	}

	for id, pendingRule := range r.pending.Rules {
		if pendingRule == nil {
			delete(r.Rules, id)
		} else {
			ruleLogger := r.logger.With(zap.Inline(pendingRule))

			if pendingRule.TimePeriodID.Valid {
				if p := r.TimePeriods[pendingRule.TimePeriodID.Int64]; p == nil {
					ruleLogger.Warnw("ignoring rule with unknown timeperiod_id")
					continue
				} else {
					pendingRule.TimePeriod = p
				}
			}

			for _, escalation := range pendingRule.Escalations {
				for i, recipient := range escalation.Recipients {
					recipientLogger := r.logger.With(
						zap.Int64("id", recipient.ID),
						zap.Int64("escalation_id", recipient.EscalationID),
						zap.Int64("channel_id", recipient.ChannelID.Int64),
						zap.Inline(recipient.Key))

					if recipient.ContactID.Valid {
						id := recipient.ContactID.Int64
						if c := r.Contacts[id]; c != nil {
							recipient.Recipient = c
						} else {
							recipientLogger.Warnw("ignoring unknown escalation recipient")
							escalation.Recipients[i] = nil
						}
					} else if recipient.GroupID.Valid {
						id := recipient.GroupID.Int64
						if g := r.Groups[id]; g != nil {
							recipient.Recipient = g
						} else {
							recipientLogger.Warnw("ignoring unknown escalation recipient")
							escalation.Recipients[i] = nil
						}
					} else if recipient.ScheduleID.Valid {
						id := recipient.ScheduleID.Int64
						if s := r.Schedules[id]; s != nil {
							recipient.Recipient = s
						} else {
							recipientLogger.Warnw("ignoring unknown escalation recipient")
							escalation.Recipients[i] = nil
						}
					} else {
						recipientLogger.Warnw("ignoring unknown escalation recipient")
						escalation.Recipients[i] = nil
					}
				}

				escalation.Recipients = slices.DeleteFunc(escalation.Recipients, func(r *rule.EscalationRecipient) bool {
					return r == nil
				})
			}

			if currentRule := r.Rules[id]; currentRule != nil {
				*currentRule = *pendingRule
			} else {
				r.Rules[id] = pendingRule
			}
		}
	}

	r.pending.Rules = nil
}
