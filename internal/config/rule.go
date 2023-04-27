package config

import (
	"context"
	"github.com/icinga/icingadb/pkg/icingadb"
	"github.com/icinga/icingadb/pkg/logging"
	"github.com/icinga/noma/internal/filter"
	"github.com/icinga/noma/internal/rule"
	"github.com/jmoiron/sqlx"
	"go.uber.org/zap"
	"log"
)

func (r *RuntimeConfig) fetchRules(ctx context.Context, db *icingadb.DB, tx *sqlx.Tx, logger *logging.Logger) error {
	var rulePtr *rule.Rule
	stmt := db.BuildSelectStmt(rulePtr, rulePtr)
	log.Println(stmt)

	var rules []*rule.Rule
	if err := tx.SelectContext(ctx, &rules, stmt); err != nil {
		log.Println(err)
		return err
	}

	rulesByID := make(map[int64]*rule.Rule)
	for _, rule := range rules {
		ruleLogger := logger.With(
			zap.Int64("id", rule.ID),
			zap.String("name", rule.Name),
			zap.String("object_filter", rule.ObjectFilterExpr.String),
			zap.Int64("timeperiod_id", rule.TimePeriodID.Int64),
		)

		if rule.TimePeriodID.Valid {
			p := r.pending.TimePeriods[rule.TimePeriodID.Int64]
			if p == nil {
				ruleLogger.Warnw("ignoring rule with unknown timeperiod_id")
				continue
			}

			rule.TimePeriod = p
		}

		if rule.ObjectFilterExpr.Valid {
			f, err := filter.Parse(rule.ObjectFilterExpr.String)
			if err != nil {
				ruleLogger.Warnw("ignoring rule as parsing object_filter failed", zap.Error(err))
				continue
			}

			rule.ObjectFilter = f
		}

		rulesByID[rule.ID] = rule
		ruleLogger.Debugw("loaded rule config")
	}

	var escalationPtr *rule.Escalation
	stmt = db.BuildSelectStmt(escalationPtr, escalationPtr)
	log.Println(stmt)

	var escalations []*rule.Escalation
	if err := tx.SelectContext(ctx, &escalations, stmt); err != nil {
		log.Println(err)
		return err
	}

	escalationsByID := make(map[int64]*rule.Escalation)
	for _, escalation := range escalations {
		escalationLogger := logger.With(
			zap.Int64("id", escalation.ID),
			zap.Int64("rule_id", escalation.RuleID),
			zap.String("condition", escalation.ConditionExpr.String),
			zap.String("name", escalation.NameRaw.String),
			zap.Int64("fallback_for", escalation.FallbackForID.Int64),
		)

		rule := rulesByID[escalation.RuleID]
		if rule == nil {
			escalationLogger.Warnw("ignoring escalation for unknown rule_id")
			continue
		}

		if escalation.ConditionExpr.Valid {
			// TODO: implement condition parsing
			escalationLogger.Warnw("ignoring escalation with condition (not yet implemented)")
			continue
		}

		if escalation.FallbackForID.Valid {
			// TODO: implement fallbacks (needs extra validation: mismatching rule_id, cycles)
			escalationLogger.Warnw("ignoring fallback escalation (not yet implemented)")
			continue
		}

		if escalation.NameRaw.Valid {
			escalation.Name = escalation.NameRaw.String
		}

		rule.Escalations = append(rule.Escalations, escalation)
		escalationsByID[escalation.ID] = escalation
		escalationLogger.Debugw("loaded escalation config")
	}

	var recipientPtr *rule.EscalationRecipient
	stmt = db.BuildSelectStmt(recipientPtr, recipientPtr)
	log.Println(stmt)

	var recipients []*rule.EscalationRecipient
	if err := tx.SelectContext(ctx, &recipients, stmt); err != nil {
		log.Println(err)
		return err
	}

	for _, recipient := range recipients {
		recipientLogger := logger.With(
			zap.Int64("id", recipient.ID),
			zap.Int64("escalation_id", recipient.EscalationID),
			zap.String("channel_type", recipient.ChannelType))

		if recipient.ContactID.Valid {
			id := recipient.ContactID.Int64
			recipientLogger = recipientLogger.With(zap.Int64("contact_id", id))
			recipient.Recipient = r.pending.Contacts[id]
		} else if recipient.GroupID.Valid {
			id := recipient.GroupID.Int64
			recipientLogger = recipientLogger.With(zap.Int64("contactgroup_id", id))
			recipient.Recipient = r.pending.Groups[id]
		} else if recipient.ScheduleID.Valid {
			id := recipient.ScheduleID.Int64
			recipientLogger = recipientLogger.With(zap.Int64("schedule_id", id))
			recipient.Recipient = r.pending.Schedules[id]
		}

		escalation := escalationsByID[recipient.EscalationID]
		if escalation == nil {
			recipientLogger.Warnw("ignoring recipient for unknown escalation")
		} else if recipient.Recipient == nil {
			recipientLogger.Warnw("ignoring unknown escalation recipient")
		} else {
			escalation.Recipients = append(escalation.Recipients, recipient)
			recipientLogger.Debugw("loaded escalation recipient config")
		}
	}

	r.pending.Rules = rulesByID

	return nil
}
