package events

import (
	"context"
	"github.com/icinga/icinga-notifications/internal/config"
	"github.com/icinga/icinga-notifications/internal/contracts"
	"github.com/icinga/icinga-notifications/internal/event"
	"github.com/icinga/icinga-notifications/internal/history"
	"github.com/icinga/icinga-notifications/internal/incident"
	"github.com/icinga/icinga-notifications/internal/notifyutils"
	"github.com/icinga/icinga-notifications/internal/object"
	"github.com/icinga/icinga-notifications/internal/rule"
	"github.com/icinga/icinga-notifications/internal/utils"
	"github.com/icinga/icingadb/pkg/icingadb"
	"github.com/icinga/icingadb/pkg/logging"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// router is used to dispatch all incoming events to their corresponding handlers and provides a default
// handler if there is none. You should always use this type to handle events properly and must avoid bypassing
// it, and accessing the handlers directly.
type router struct {
	db            *icingadb.DB
	logs          *logging.Logging
	logger        *zap.SugaredLogger
	runtimeConfig *config.RuntimeConfig
}

// Process processes the specified event.Event.
//
// The returned error is guaranteed to be event.ErrEventProcessing or event.ErrSuperfluousStateChange
// in some way and can safely be forwarded to the clients.
func Process(ctx context.Context, db *icingadb.DB, logs *logging.Logging, rc *config.RuntimeConfig, ev *event.Event) error {
	r := &router{runtimeConfig: rc, db: db, logs: logs, logger: logs.GetChildLogger("routing").SugaredLogger}

	return r.route(ctx, ev)
}

// Route routes the specified event.Event to its corresponding handler.
//
// This function first constructs the target object.Object and its incident.Incident from the provided event.Event.
// After some safety checks have been carried out, the event is then handed over to the associated handler or its
// default one. Currently, ‘incident’ is the only (independent) event handler apart from the default one of this type.
//
// Note, that this function will always return event.ErrEventProcessing if the event cannot be processed successfully,
// either directly (unwrapped) or indirectly (wrapped with an additional context), except in one particular case
// where event.ErrSuperfluousStateChange is returned. Thus, the returned error can be safely forwarded to clients.
func (r *router) route(ctx context.Context, ev *event.Event) error {
	obj, err := object.FromEvent(ctx, r.db, ev)
	if err != nil {
		r.logger.Errorw("Failed to sync object with the database", zap.Int64("source_id", ev.SourceId), zap.Error(err))
		return event.ErrEventProcessing
	}

	r.logger = r.logger.With(zap.String("object", obj.DisplayName()), zap.Int64("source_id", ev.SourceId))

	createIncident := ev.Severity != event.SeverityNone && ev.Severity != event.SeverityOK
	incidentLogger := r.logs.GetChildLogger("incident")
	i, err := incident.GetCurrent(ctx, r.db, obj, incidentLogger, r.runtimeConfig, createIncident)
	if err != nil {
		r.logger.Errorw("Failed to create/determine an incident", zap.Error(err))
		return event.ErrEventProcessing
	}

	if i != nil {
		if err := i.ProcessEvent(ctx, ev); err != nil {
			if errors.Is(err, event.ErrSuperfluousStateChange) {
				return err
			}

			// Expect the actual error to be logged with additional context in the incident package.
			return event.ErrEventProcessing
		}

		return nil
	}

	switch ev.Type {
	case event.TypeState:
		if ev.Severity != event.SeverityOK {
			r.logger.Warn("Cannot process state event without an incident")
			return errors.Wrap(event.ErrEventProcessing, "cannot process event without an active incident")
		}

		r.logger.Debug("Received superfluous OK state event")
		return event.ErrSuperfluousStateChange
	case event.TypeAcknowledgementSet:
		r.logger.Warn("Cannot set acknowledgement without an active incident")
		return errors.Wrap(event.ErrEventProcessing, "cannot set acknowledgement without an active incident")
	case event.TypeAcknowledgementCleared:
		r.logger.Warn("Cannot clear acknowledgement without an active incident")
		return errors.Wrapf(event.ErrEventProcessing, "cannot clear acknowledgement without an active incident")
	}

	return r.process(ctx, obj, ev)
}

// process processes the provided event and notifies routing recipients in a non-blocking fashion.
//
// This function processes the specified event in an own transaction and rolls back all changes made to the
// database if it returns with an error. However, it should be noted that notifications are triggered outside
// a database transaction initiated after successful event processing and will not undo the changes made by the
// event processing tx if sending the notifications fails.
//
// Returns always event.ErrEventProcessing in some way in case of internal processing errors.
func (r *router) process(ctx context.Context, obj *object.Object, ev *event.Event) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		r.logger.Errorw("Failed to start a database transaction", zap.Error(err))
		return event.ErrEventProcessing
	}
	defer func() { _ = tx.Rollback() }()

	if err := ev.Sync(ctx, tx, r.db, obj.ID); err != nil {
		r.logger.Errorw("Failed to sync an event with the database", zap.Error(err))
		return event.ErrEventProcessing
	}

	// Incident filter rules are stateful, which means that once they have been matched, they remain
	// effective for the ongoing incident and never need to be rechecked. For non-state events, on the
	// other hand, there is no such (already matched) rule if they aren't linked to any active incident
	// and need to be reevaluated all over again.
	routes := r.evaluateRoutes(ev, r.evaluateRules(obj))

	notifications := make(history.PendingNotifications)
	for routing, channels := range r.getRecipientsChannel(ev, routes) {
		histories, err := history.AddPendingNotifications(ctx, r.db, tx, channels, func(h *history.NotificationHistory) {
			h.RuleRoutingID = utils.ToDBInt(routing.ID)
		})
		if err != nil {
			r.logger.Errorw("Failed to insert pending notification histories", zap.Inline(routing), zap.Error(err))
			return event.ErrEventProcessing
		}

		for contact, entries := range histories {
			notifications[contact] = append(notifications[contact], entries...)
		}
	}

	// Commit the event processing transaction before moving on to the next step and sending notifications.
	if err = tx.Commit(); err != nil {
		r.logger.Errorw("Cannot commit database transaction", zap.Error(err))
		return event.ErrEventProcessing
	}

	if len(notifications) == 0 {
		r.logger.Debugw("No routing recipients found, not sending notifications", zap.String("event", ev.String()))
		return nil
	}

	err = notifyutils.NotifyContacts(ctx, contracts.NewDefaultNotifyCtx(obj, r.logger), r.db, r.runtimeConfig, ev, notifications)
	if err != nil {
		r.logger.Errorw("Failed to send all pending notifications", zap.Error(err))
		return event.ErrEventProcessing
	}

	return nil
}

// evaluateRules reevaluates and retrieves all the configured event rules that match on the given object.
// DO NOT call this while holding the runtime config lock!
func (r *router) evaluateRules(obj *object.Object) map[int64]*rule.Rule {
	r.runtimeConfig.RLock()
	defer r.runtimeConfig.RUnlock()

	rules := make(map[int64]*rule.Rule)
	for _, ru := range r.runtimeConfig.Rules {
		// Skip the event rule if it's disabled
		if ru == nil || !ru.IsActive.Valid || !ru.IsActive.Bool {
			continue
		}

		if ru.ObjectFilter != nil {
			matched, err := ru.ObjectFilter.Eval(obj)
			if err != nil {
				// Do not let our event processing tx fail due to the Object eval error, so log it and move on.
				r.logger.Warnw("Failed to evaluate object filter", zap.String("rule", ru.Name), zap.Error(err))
			}

			if err != nil || !matched {
				continue
			}

			r.logger.Debugw("Event rule filter matches", zap.String("rule", ru.Name),
				zap.String("filter", ru.ObjectFilterExpr.String))
		}

		r.logger.Infof("Rule %q matches", ru.Name)

		rules[ru.ID] = ru
	}

	return rules
}

// EvaluateRoutes evaluates and retrieves all the configured event routing that match on the given event.
func (r *router) evaluateRoutes(ev *event.Event, rules map[int64]*rule.Rule) []*rule.Routing {
	filterContext := &rule.RoutingFilter{EventType: ev.Type}

	var routes []*rule.Routing
	for _, ru := range rules {
		for _, routing := range ru.Routes {
			// Rule routing without any condition always matches.
			matched := routing.Condition == nil

			if !matched {
				var err error
				matched, err = routing.Condition.Eval(filterContext)
				if err != nil {
					r.logger.Warnw("Failed to evaluate routing condition", zap.String("rule", ru.Name),
						zap.Inline(routing), zap.Error(err))

					matched = false
				}
			}

			if matched {
				routes = append(routes, routing)
				r.logger.Debugw("Routing condition matches", zap.String("rule", ru.Name), zap.Inline(routing))
			}
		}
	}

	return routes
}

// GetRecipientsChannel retrieves all the recipients channels of the routes.
func (r *router) getRecipientsChannel(ev *event.Event, routes []*rule.Routing) map[*rule.Routing]rule.ContactChannels {
	routesChannels := make(map[*rule.Routing]rule.ContactChannels)
	for _, routing := range routes {
		if routesChannels[routing] == nil {
			routesChannels[routing] = make(rule.ContactChannels)
		}

		routesChannels[routing].LoadFromRoutingRecipients(routing, ev.Time, rule.IsNotifiable)
	}

	return routesChannels
}
