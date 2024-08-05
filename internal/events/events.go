package events

import (
	"context"
	"fmt"
	"strconv"

	baseEv "github.com/icinga/icinga-go-library/notifications/event"
	"github.com/icinga/icinga-go-library/notifications/plugin"
	"github.com/icinga/icinga-go-library/types"
	"github.com/icinga/icinga-notifications/internal/config"
	"github.com/icinga/icinga-notifications/internal/event"
	"github.com/icinga/icinga-notifications/internal/incident"
	"github.com/icinga/icinga-notifications/internal/notification"
	"github.com/icinga/icinga-notifications/internal/object"
	"github.com/icinga/icinga-notifications/internal/rule"
	"github.com/jmoiron/sqlx"
	"go.uber.org/zap"
)

// Process processes the specified event.Event.
//
// This function first constructs the target [object.Object] and its [incident.Incident] from the provided [event.Event].
// After some safety checks have been carried out, it calls doProcess() to actually process the event and send out
// the notifications.
//
// Returns an error if it fails to successfully route/process the provided event.
func Process(ctx context.Context, runtimeC *config.RuntimeConfig, ev *event.Event) error {
	res := config.MakeResources(runtimeC, "routing")
	var wasObjectMuted bool
	if obj := object.GetFromCache(object.ID(ev.SourceId, ev.Tags)); obj != nil {
		wasObjectMuted = obj.IsMuted()
	}

	obj, err := object.FromEvent(ctx, res.DB, ev)
	if err != nil {
		res.Logger.Errorw("Failed to generate object from event", zap.Stringer("event", ev), zap.Error(err))
		return err
	}

	res.Logger = res.Logger.With(zap.String("object", obj.DisplayName()), zap.Stringer("event", ev))

	createIncident := ev.Severity != baseEv.SeverityNone && ev.Severity != baseEv.SeverityOK
	currentIncident, err := incident.GetCurrent(ctx, obj, runtimeC, createIncident)
	if err != nil {
		res.Logger.Errorw("Failed to create/determine an incident", zap.Error(err))
		return err
	}

	if currentIncident == nil {
		switch ev.Severity {
		case baseEv.SeverityNone:
			// We need to ignore superfluous mute and unmute events here, as would be the case with an existing
			// incident, otherwise the event stream catch-up phase will generate useless events after each
			// Icinga 2 reload and overwhelm the database with the very same mute/unmute events.
			if wasObjectMuted && ev.Type == baseEv.TypeMute {
				return event.ErrSuperfluousMuteUnmuteEvent
			}
			if !wasObjectMuted && ev.Type == baseEv.TypeUnmute {
				return event.ErrSuperfluousMuteUnmuteEvent
			}
		case baseEv.SeverityOK:
			res.Logger.Debugw("Cannot process OK state event", zap.Int64("source_id", ev.SourceId))
			return fmt.Errorf("OK state event from source %d: %w", ev.SourceId, event.ErrSuperfluousStateChange)
		default:
			panic(fmt.Sprintf("cannot process event %v with a non-OK state %v without a known incident", ev, ev.Severity))
		}
	}
	return doProcess(ctx, res, obj, ev, currentIncident, wasObjectMuted)
}

// doProcess actually processes the event and sends out the notifications.
//
// This function is called by Process() after the target object and its incident have been determined.
// It processes the event in a database transaction and rolls back any changes made to the database
// if any of the processing steps fail. However, it should be noted that notifications are triggered
// outside a database transaction initiated after successful event processing and will not undo the
// changes made by the event processing tx if sending the notifications fails.
//
// Returns an error if it fails to process the event or send out the notifications.
func doProcess(
	ctx context.Context,
	res config.Resources,
	obj *object.Object,
	ev *event.Event,
	cIncident *incident.Incident,
	wasObjMuted bool,
) error {
	tx, err := res.DB.BeginTxx(ctx, nil)
	if err != nil {
		res.Logger.Errorw("Failed to start a database transaction", zap.Error(err))
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if err := ev.Sync(ctx, tx, res.DB, obj.ID); err != nil {
		res.Logger.Errorw("Failed to sync an event to the database", zap.Error(err))
		return err
	}

	res.RuntimeConfig.RLock()
	defer res.RuntimeConfig.RUnlock()

	if cIncident != nil {
		cIncident.Lock()
		defer cIncident.Unlock()

		if err := cIncident.ProcessEvent(ctx, tx, ev); err != nil {
			return err
		}
	}

	rules := make(map[int64]struct{})
	for _, ruleId := range ev.RuleIds {
		ruleIdInt, err := strconv.ParseInt(ruleId, 10, 64)
		if err != nil {
			res.Logger.Errorw("Event rule is not an integer", zap.String("rule_id", ruleId), zap.Error(err))
			return fmt.Errorf("cannot convert rule id %q to an int: %w", ruleId, err)
		}

		if _, ok := res.RuntimeConfig.Rules[ruleIdInt]; !ok {
			res.Logger.Errorw("Event refers to non-existing event rule, might got deleted", zap.Int64("rule_id", ruleIdInt))
			continue
		}
		rules[ruleIdInt] = struct{}{}
	}

	filterContext := &rule.RoutingFilter{EventType: ev.Type}
	entries := make(config.RuleEntries)
	// EvaluateRuleEntries only returns an error if one of the provided callback hooks returns
	// an error or the OnError handler returns false, and since none of our callbacks return an
	// error nor false, we can safely discard the return value here.
	err = entries.Evaluate(res, filterContext, rules, config.EvalOptions{
		OnFilterMatch: func(route *rule.Entry) error {
			ru := res.RuntimeConfig.Rules[route.RuleID]
			res.Logger.Debugw("Routing condition matches", zap.Object("rule", ru), zap.Object("rule_routing", route))
			return nil
		},
		OnError: func(route *rule.Entry, err error) bool {
			res.Logger.Warnw("Failed to evaluate routing condition",
				zap.Object("rule", res.RuntimeConfig.Rules[route.RuleID]),
				zap.Object("rule_routing", route),
				zap.Error(err))
			return true
		},
	})
	if err != nil {
		res.Logger.DPanicw("Failed to evaluate routing entries", zap.Error(err))
		return err
	}

	var incidentID int64
	notifications := make(notification.PendingNotifications)
	if cIncident != nil {
		incidentID = cIncident.ID
		notifications, err = cIncident.GenerateNotifications(ctx, tx, ev, cIncident.GetRecipientsChannel(ev.Time))
		if err != nil {
			res.Logger.Errorw("Failed to generate incident notifications", zap.Error(err))
			return err
		}
	}
	err = loadNotificationsFromEntries(ctx, &res, tx, ev, notifications, entries, wasObjMuted && obj.IsMuted(), incidentID)
	if err != nil {
		return err
	}

	if err = tx.Commit(); err != nil {
		res.Logger.Errorw("Cannot commit database transaction", zap.Error(err))
		return err
	}

	var req *plugin.NotificationRequest
	if cIncident != nil {
		// We've just committed the DB transaction and can safely update the incident muted flag.
		cIncident.RefreshIsMuted()
		req = cIncident.MakeNotificationRequest(ev)
	} else {
		req = notification.NewPluginRequest(obj, ev)
	}
	return notification.NotifyContacts(ctx, &res, req, notifications)
}

// loadNotificationsFromEntries loads pending notifications from the specified rule entries
// and adds them to the provided [notification.PendingNotifications] map.
//
// If suppressed is true, the loaded notifications will be marked as suppressed and won't be added to the
// [notification.PendingNotifications] map. However, they will still be inserted into the database as such.
//
// Returns an error if it fails to persist the generated pending/suppressed notifications to the database.
func loadNotificationsFromEntries(
	ctx context.Context,
	res *config.Resources,
	tx *sqlx.Tx,
	ev *event.Event,
	notifications notification.PendingNotifications,
	entries config.RuleEntries,
	suppressed bool,
	incidentID int64,
) error {
	for _, route := range entries {
		channels := make(rule.ContactChannels)
		channels.LoadFromEntryRecipients(route, ev.Time, rule.AlwaysNotifiable)
		if len(channels) == 0 {
			res.Logger.Warnw("Rule routing expanded to no contacts", zap.Object("rule_routing", route))
			continue
		}

		histories, err := notification.AddNotifications(ctx, res.DB, tx, channels, func(h *notification.History) {
			h.RuleEntryID = types.MakeInt(route.ID, types.TransformZeroIntToNull)
			h.IncidentID = types.MakeInt(incidentID, types.TransformZeroIntToNull)
			h.Message = types.MakeString(ev.Message, types.TransformEmptyStringToNull)
			if suppressed {
				h.NotificationState = notification.StateSuppressed
			}
		})
		if err != nil {
			res.Logger.Errorw("Failed to insert pending notification histories",
				zap.Inline(route),
				zap.Bool("suppressed", suppressed),
				zap.Error(err))
			return err
		}

		if !suppressed {
			for contact, items := range histories {
				notifications[contact] = append(notifications[contact], items...)
			}
		}
	}
	return nil
}
