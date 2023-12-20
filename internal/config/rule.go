package config

import (
	"context"
	"fmt"
	"github.com/icinga/icinga-notifications/internal/filter"
	"github.com/icinga/icinga-notifications/internal/rule"
	"github.com/icinga/icinga-notifications/internal/utils"
	"github.com/jmoiron/sqlx"
	"go.uber.org/zap"
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
		ruleLogger := r.logger.With(
			zap.Int64("id", ru.ID),
			zap.String("name", ru.Name),
			zap.String("object_filter", ru.ObjectFilterExpr.String),
			zap.Int64("timeperiod_id", ru.TimePeriodID.Int64),
		)

		if ru.ObjectFilterExpr.Valid {
			f, err := filter.Parse(ru.ObjectFilterExpr.String)
			if err != nil {
				ruleLogger.Warnw("ignoring rule as parsing object_filter failed", zap.Error(err))
				continue
			}

			ru.ObjectFilter = f
		}

		ru.Escalations = make(map[int64]*rule.Escalation)
		ru.NonStateEscalations = make(map[int64]*rule.NonStateEscalation)

		rulesByID[ru.ID] = ru
		ruleLogger.Debugw("loaded rule config")
	}

	escalationsByID, err := r.loadEscalations(ctx, tx, rule.TypeEscalation, rulesByID)
	if err != nil {
		return err
	}

	nonStateEscalationsByID, err := r.loadEscalations(ctx, tx, rule.TypeNonStateEscalation, rulesByID)
	if err != nil {
		return err
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
			zap.Int64("escalation_id", recipient.EscalationID.Int64),
			zap.Int64("non_state_escalation_id", recipient.NonStateEscalationID.Int64),
			zap.Int64("channel_id", recipient.ChannelID.Int64))

		escalation := escalationsByID[recipient.EscalationID.Int64]
		nonStateEscalation := nonStateEscalationsByID[recipient.NonStateEscalationID.Int64]
		if escalation == nil && nonStateEscalation == nil {
			recipientLogger.Warnw("ignoring recipient for unknown escalation and non-state escalation")
		}

		if escalation != nil {
			escalation.Recipients = append(escalation.Recipients, recipient)
			recipientLogger.Debugw("loaded escalation recipient config")
		}

		if nonStateEscalation != nil {
			nonStateEscalation.Recipients = append(nonStateEscalation.Recipients, recipient)
			recipientLogger.Debugw("loaded non state escalation recipient config")
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
			ruleLogger := r.logger.With(
				zap.Int64("id", pendingRule.ID),
				zap.String("name", pendingRule.Name),
				zap.String("object_filter", pendingRule.ObjectFilterExpr.String),
				zap.Int64("timeperiod_id", pendingRule.TimePeriodID.Int64),
			)

			if pendingRule.TimePeriodID.Valid {
				if p := r.TimePeriods[pendingRule.TimePeriodID.Int64]; p == nil {
					ruleLogger.Warnw("ignoring rule with unknown timeperiod_id")
					continue
				} else {
					pendingRule.TimePeriod = p
				}
			}

			var allEscalations []*rule.EscalationTemplate
			for _, esc := range pendingRule.Escalations {
				allEscalations = append(allEscalations, esc.EscalationTemplate)
			}
			for _, esc := range pendingRule.NonStateEscalations {
				allEscalations = append(allEscalations, esc.EscalationTemplate)
			}

			for _, escalation := range allEscalations {
				for i, recipient := range escalation.Recipients {
					recipientLogger := r.logger.With(
						zap.Int64("id", recipient.ID),
						zap.Int64("escalation_id", recipient.EscalationID.Int64),
						zap.Int64("non_state_escalation_id", recipient.NonStateEscalationID.Int64),
						zap.Int64("channel_id", recipient.ChannelID.Int64))

					if recipient.ContactID.Valid {
						id := recipient.ContactID.Int64
						recipientLogger = recipientLogger.With(zap.Int64("contact_id", id))
						if c := r.Contacts[id]; c != nil {
							recipient.Recipient = c
						} else {
							recipientLogger.Warnw("ignoring unknown escalation recipient")
							escalation.Recipients[i] = nil
						}
					} else if recipient.GroupID.Valid {
						id := recipient.GroupID.Int64
						recipientLogger = recipientLogger.With(zap.Int64("contactgroup_id", id))
						if g := r.Groups[id]; g != nil {
							recipient.Recipient = g
						} else {
							recipientLogger.Warnw("ignoring unknown escalation recipient")
							escalation.Recipients[i] = nil
						}
					} else if recipient.ScheduleID.Valid {
						id := recipient.ScheduleID.Int64
						recipientLogger = recipientLogger.With(zap.Int64("schedule_id", id))
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

				escalation.Recipients = utils.RemoveNils(escalation.Recipients)
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

func (r *RuntimeConfig) loadEscalations(ctx context.Context, tx *sqlx.Tx, escType string, rulesByID map[int64]*rule.Rule) (map[int64]*rule.EscalationTemplate, error) {
	var escalationPtr any

	switch escType {
	case rule.TypeEscalation:
		escalationPtr = &rule.Escalation{}
	case rule.TypeNonStateEscalation:
		escalationPtr = &rule.NonStateEscalation{}
	default:
		return nil, fmt.Errorf("unknown escalation type: %s", escType)
	}

	stmt := r.db.BuildSelectStmt(escalationPtr, escalationPtr)
	r.logger.Debugf("Executing query %q", stmt)

	var escalations []*rule.EscalationTemplate
	if err := tx.SelectContext(ctx, &escalations, stmt); err != nil {
		r.logger.Errorln(err)
		return nil, err
	}

	escalationsByID := make(map[int64]*rule.EscalationTemplate)
	for _, escalation := range escalations {
		escalationLogger := r.logger.With(
			zap.Int64("id", escalation.ID),
			zap.Int64("rule_id", escalation.RuleID),
			zap.String("condition", escalation.ConditionExpr.String),
			zap.String("name", escalation.NameRaw.String),
			zap.Int64("fallback_for", escalation.FallbackForID.Int64),
		)

		ru := rulesByID[escalation.RuleID]
		if ru == nil {
			escalationLogger.Warnw("ignoring escalation for unknown rule_id", zap.String("type", escType))
			continue
		}

		if escalation.ConditionExpr.Valid {
			cond, err := filter.Parse(escalation.ConditionExpr.String)
			if err != nil {
				escalationLogger.Warnw("ignoring escalation, failed to parse condition", zap.String("type", escType), zap.Error(err))
				continue
			}

			escalation.Condition = cond
		}

		if escalation.FallbackForID.Valid {
			// TODO: implement fallbacks (needs extra validation: mismatching rule_id, cycles)
			escalationLogger.Warnw("ignoring fallback escalation (not yet implemented)", zap.String("type", escType))
			continue
		}

		if escalation.NameRaw.Valid {
			escalation.Name = escalation.NameRaw.String
		}

		if escType == rule.TypeNonStateEscalation {
			ru.NonStateEscalations[escalation.ID] = &rule.NonStateEscalation{EscalationTemplate: escalation}
		} else {
			ru.Escalations[escalation.ID] = &rule.Escalation{EscalationTemplate: escalation}
		}

		escalationsByID[escalation.ID] = escalation
		escalationLogger.Debugw("loaded escalation config")
	}

	return escalationsByID, nil
}
