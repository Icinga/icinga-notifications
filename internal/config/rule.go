package config

import (
	"context"
	"github.com/icinga/icinga-notifications/internal/filter"
	"github.com/icinga/icinga-notifications/internal/rule"
	"github.com/icinga/icinga-notifications/internal/utils"
	"github.com/icinga/icingadb/pkg/icingadb"
	"github.com/icinga/icingadb/pkg/logging"
	"github.com/jmoiron/sqlx"
	"go.uber.org/zap"
)

func (r *RuntimeConfig) fetchRules(ctx context.Context, tx *sqlx.Tx) error {
	rules, err := fetchRows[rule.Rule](ctx, r.db, tx, r.logger)
	if err != nil {
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
		ru.Routes = make(map[int64]*rule.Routing)

		rulesByID[ru.ID] = ru
		ruleLogger.Debugw("loaded rule config")
	}

	escalations, err := fetchRows[rule.Escalation](ctx, r.db, tx, r.logger)
	if err != nil {
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

		if err := escalation.Load(); err != nil {
			escalationLogger.Warnw("Ignoring escalation", zap.Error(err))
			continue
		}

		rule.Escalations[escalation.ID] = escalation
		escalationsByID[escalation.ID] = escalation
		escalationLogger.Debugw("loaded escalation config")
	}

	recipients, err := fetchRows[rule.EscalationRecipient](ctx, r.db, tx, r.logger)
	if err != nil {
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

	routes, err := fetchRows[rule.Routing](ctx, r.db, tx, r.logger)
	if err != nil {
		return err
	}

	routesByID := make(map[int64]*rule.Routing)
	for _, route := range routes {
		routeLogger := r.logger.With(zap.Inline(route))
		ru := rulesByID[route.RuleID]
		if ru == nil {
			routeLogger.Warnw("ignoring routing for unknown rule_id")
			continue
		}

		if err := route.Load(); err != nil {
			routeLogger.Warnw("Ignoring routing", zap.Error(err))
			continue
		}

		ru.Routes[route.ID] = route
		routesByID[route.ID] = route
		routeLogger.Debugw("Successfully loaded routing config")
	}

	routingRecipients, err := fetchRows[rule.RoutingRecipient](ctx, r.db, tx, r.logger)
	if err != nil {
		return err
	}

	for _, recipient := range routingRecipients {
		recipientLogger := r.logger.With(zap.Int64("id", recipient.ID), zap.Int64("routing_id", recipient.RoutingID),
			zap.Int64("channel_id", recipient.ChannelID.Int64))

		route := routesByID[recipient.RoutingID]
		if route == nil {
			recipientLogger.Warnw("ignoring recipient for unknown rule routing")
		} else {
			route.Recipients = append(route.Recipients, recipient)
			recipientLogger.Debugw("loaded routing recipient config")
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

	validateRecipientKey := func(recipient *rule.RecipientMeta) bool {
		if recipient.ContactID.Valid {
			if c := r.Contacts[recipient.ContactID.Int64]; c != nil {
				recipient.Recipient = c
				return true
			}
		} else if recipient.GroupID.Valid {
			if g := r.Groups[recipient.GroupID.Int64]; g != nil {
				recipient.Recipient = g
				return true
			}
		} else if recipient.ScheduleID.Valid {
			if s := r.Schedules[recipient.ScheduleID.Int64]; s != nil {
				recipient.Recipient = s
				return true
			}
		}

		return false
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

			for _, escalation := range pendingRule.Escalations {
				for i, escalationRecipient := range escalation.Recipients {
					recipientLogger := r.logger.With(
						zap.Int64("id", escalationRecipient.ID),
						zap.Int64("escalation_id", escalationRecipient.EscalationID),
						zap.Int64("channel_id", escalationRecipient.ChannelID.Int64))

					if !validateRecipientKey(&escalationRecipient.RecipientMeta) {
						recipientLogger.With(zap.Inline(escalationRecipient.Key)).Warnw("ignoring unknown escalation recipient")
						escalation.Recipients[i] = nil
					}
				}

				escalation.Recipients = utils.RemoveNils(escalation.Recipients)
			}

			for _, routing := range pendingRule.Routes {
				for i, rRecipient := range routing.Recipients {
					if !validateRecipientKey(&rRecipient.RecipientMeta) {
						routing.Recipients[i] = nil
						r.logger.With(zap.Int64("id", rRecipient.ID), zap.Int64("channel_id", rRecipient.ChannelID.Int64)).
							With(zap.Inline(rRecipient.Key)).Warnw("ignoring unknown escalation recipient")
					}
				}

				routing.Recipients = utils.RemoveNils(routing.Recipients)
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

func fetchRows[Row any](ctx context.Context, db *icingadb.DB, tx *sqlx.Tx, logger *logging.Logger) ([]*Row, error) {
	stmt := db.BuildSelectStmt(new(Row), new(Row))
	logger.Debugf("Executing query %q", stmt)

	var rows []*Row
	if err := tx.SelectContext(ctx, &rows, db.Rebind(stmt)); err != nil {
		logger.Errorln(err)
		return nil, err
	}

	return rows, nil
}
