package events

import (
	"context"
	"fmt"
	"github.com/icinga/icinga-go-library/logging"
	"github.com/icinga/icinga-notifications/internal/config"
	"github.com/icinga/icinga-notifications/internal/event"
	"github.com/icinga/icinga-notifications/internal/incident"
	"github.com/icinga/icinga-notifications/internal/notification"
	"github.com/icinga/icinga-notifications/internal/object"
	"github.com/icinga/icinga-notifications/internal/rule"
	"github.com/icinga/icinga-notifications/internal/utils"
	"github.com/jmoiron/sqlx"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// router dispatches all incoming events to their corresponding handlers and provides a default one if there is none.
//
// You should always use this type to handle events properly and shouldn't try to bypass it
// by accessing other handlers directly.
type router struct {
	// notification.Notifier is a helper type used to send notifications.
	// It is embedded to allow direct access to its members, such as logger, DB etc.
	notification.Notifier

	// config.Evaluable encapsulates all evaluable configuration types, such as rule.Rule, rule.Entry etc.
	// It is embedded to enable direct access to its members.
	*config.Evaluable

	logs *logging.Logging
}

// route routes the specified event.Event to its corresponding handler.
//
// This function first constructs the target object.Object and its incident.Incident from the provided event.Event.
// After some safety checks have been carried out, the event is then handed over to the process method.
//
// Returns an error if it fails to successfully route/process the provided event.
func (r *router) route(ctx context.Context, ev *event.Event) error {
	var wasObjectMuted bool
	if obj := object.GetFromCache(object.ID(ev.SourceId, ev.Tags)); obj != nil {
		wasObjectMuted = obj.IsMuted()
	}

	obj, err := object.FromEvent(ctx, r.DB, ev)
	if err != nil {
		r.Logger.Errorw("Failed to generate object from event", zap.Stringer("event", ev), zap.Error(err))
		return err
	}

	r.Logger = r.Logger.With(zap.String("object", obj.DisplayName()), zap.Stringer("event", ev))

	createIncident := ev.Severity != event.SeverityNone && ev.Severity != event.SeverityOK
	currentIncident, err := incident.GetCurrent(ctx, r.DB, obj, r.logs.GetChildLogger("incident"), r.RuntimeConfig, createIncident)
	if err != nil {
		r.Logger.Errorw("Failed to create/determine an incident", zap.Error(err))
		return err
	}

	if currentIncident == nil {
		switch {
		case ev.Severity == event.SeverityNone:
			// We need to ignore superfluous mute and unmute events here, as would be the case with an existing
			// incident, otherwise the event stream catch-up phase will generate useless events after each
			// Icinga 2 reload and overwhelm the database with the very same mute/unmute events.
			if wasObjectMuted && ev.Type == event.TypeMute {
				return event.ErrSuperfluousMuteUnmuteEvent
			}
			if !wasObjectMuted && ev.Type == event.TypeUnmute {
				return event.ErrSuperfluousMuteUnmuteEvent
			}
		case ev.Severity == event.SeverityOK:
			r.Logger.Debugw("Cannot process OK state event", zap.Int64("source_id", ev.SourceId))
			return errors.Wrapf(event.ErrSuperfluousStateChange, "OK state event from source %d", ev.SourceId)
		default:
			panic(fmt.Sprintf("cannot process event %v with a non-OK state %v without a known incident", ev, ev.Severity))
		}
	}

	return r.process(ctx, obj, ev, currentIncident, wasObjectMuted)
}

// process processes the provided event and notifies the recipients of the resulting notifications in a non-blocking manner.
// You should be aware, though, that this method might block competing events that refer to the same incident.Incident.
//
// process processes the specified event in an own transaction and rolls back any changes made to the database
// if it returns with an error. However, it should be noted that notifications are triggered outside a database
// transaction initiated after successful event processing and will not undo the changes made by the event processing
// tx if sending the notifications fails.
//
// Returns an error in case of internal processing errors.
func (r *router) process(ctx context.Context, obj *object.Object, ev *event.Event, currentIncident *incident.Incident, wasObjMuted bool) error {
	tx, err := r.DB.BeginTxx(ctx, nil)
	if err != nil {
		r.Logger.Errorw("Failed to start a database transaction", zap.Error(err))
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if err := ev.Sync(ctx, tx, r.DB, obj.ID); err != nil {
		r.Logger.Errorw("Failed to sync an event to the database", zap.Error(err))
		return err
	}

	r.RuntimeConfig.RLock()
	defer r.RuntimeConfig.RUnlock()

	if currentIncident != nil {
		currentIncident.Lock()
		defer currentIncident.Unlock()

		if err := currentIncident.ProcessEvent(ctx, tx, ev); err != nil {
			return err
		}
	}

	// EvaluateRules only returns an error if one of the provided callback hooks returns
	// an error or the OnError handler returns false, and since none of our callbacks return
	// an error nor false, we can safely discard the return value here.
	_ = r.EvaluateRules(r.RuntimeConfig, obj, config.EvalOptions[*rule.Rule, any]{
		OnPreEvaluate: func(r *rule.Rule) bool { return r.Type == rule.TypeRouting },
		OnFilterMatch: func(ru *rule.Rule) error {
			r.Logger.Infow("Rule matches", zap.Object("rule", ru))
			return nil
		},
		OnError: func(ru *rule.Rule, err error) bool {
			r.Logger.Warnw("Failed to evaluate non-state rule condition", zap.Object("rule", ru), zap.Error(err))
			return true
		},
	})

	filterContext := &rule.RoutingFilter{EventType: ev.Type}
	// EvaluateRuleEntries only returns an error if one of the provided callback hooks returns
	// an error or the OnError handler returns false, and since none of our callbacks return an
	// error nor false, we can safely discard the return value here.
	_ = r.EvaluateRuleEntries(r.RuntimeConfig, filterContext, config.EvalOptions[*rule.Entry, any]{
		OnFilterMatch: func(route *rule.Entry) error {
			ru := r.RuntimeConfig.Rules[route.RuleID]
			r.Logger.Debugw("Routing condition matches", zap.Object("rule", ru), zap.Object("rule_routing", route))
			return nil
		},
		OnError: func(route *rule.Entry, err error) bool {
			ru := r.RuntimeConfig.Rules[route.RuleID]
			r.Logger.Warnw("Failed to evaluate routing condition",
				zap.Object("rule", ru),
				zap.Object("rule_routing", route),
				zap.Error(err))
			return true
		},
	})

	var incidentID int64
	notifications := make(notification.PendingNotifications)
	if currentIncident != nil {
		incidentID = currentIncident.ID
		notifications, err = currentIncident.GenerateNotifications(ctx, tx, ev, currentIncident.GetRecipientsChannel(ev.Time))
		if err != nil {
			r.Logger.Errorw("Failed to generate incident notifications", zap.Error(err))
			return err
		}
	}
	if err := r.generateNotifications(ctx, tx, ev, wasObjMuted && obj.IsMuted(), incidentID, notifications); err != nil {
		return err
	}

	if err = tx.Commit(); err != nil {
		r.Logger.Errorw("Cannot commit database transaction", zap.Error(err))
		return err
	}

	if currentIncident != nil {
		// We've just committed the DB transaction and can safely update the incident muted flag.
		currentIncident.RefreshIsMuted()
		return currentIncident.NotifyContacts(ctx, currentIncident.MakeNotificationRequest(ev), notifications)
	}

	if err := r.NotifyContacts(ctx, notification.NewPluginRequest(obj, ev), notifications); err != nil {
		r.Logger.Errorw("Failed to send all pending notifications", zap.Error(err))
		return err
	}

	return nil
}

// generateNotifications generates non-state notifications and loads them into the provided map.
//
// Returns an error if it fails to persist the generated pending/suppressed notifications to the database.
func (r *router) generateNotifications(
	ctx context.Context, tx *sqlx.Tx, ev *event.Event, suppressed bool, incidentID int64,
	notifications notification.PendingNotifications,
) error {
	for _, route := range r.RuleEntries {
		channels := make(rule.ContactChannels)
		channels.LoadFromEntryRecipients(route, ev.Time, rule.AlwaysNotifiable)
		if len(channels) == 0 {
			r.Logger.Warnw("Rule routing expanded to no contacts",
				zap.Object("rule_routing", route))
			continue
		}

		histories, err := notification.AddNotifications(ctx, r.DB, tx, channels, func(h *notification.History) {
			h.RuleEntryID = utils.ToDBInt(route.ID)
			h.IncidentID = utils.ToDBInt(incidentID)
			h.Message = utils.ToDBString(ev.Message)
			if suppressed {
				h.NotificationState = notification.StateSuppressed
			}
		})
		if err != nil {
			r.Logger.Errorw("Failed to insert pending notification histories",
				zap.Inline(route), zap.Bool("suppressed", suppressed), zap.Error(err))
			return err
		}

		if !suppressed {
			for contact, entries := range histories {
				notifications[contact] = append(notifications[contact], entries...)
			}
		}
	}

	return nil
}
