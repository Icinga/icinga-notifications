package eventhandler

import (
	"context"
	"fmt"
	"github.com/icinga/icinga-notifications/internal/common"
	"github.com/icinga/icinga-notifications/internal/config"
	"github.com/icinga/icinga-notifications/internal/daemon"
	"github.com/icinga/icinga-notifications/internal/event"
	"github.com/icinga/icinga-notifications/internal/incident"
	"github.com/icinga/icinga-notifications/internal/object"
	"github.com/icinga/icinga-notifications/internal/recipient"
	"github.com/icinga/icinga-notifications/internal/rule"
	"github.com/icinga/icinga-notifications/internal/utils"
	"github.com/icinga/icingadb/pkg/icingadb"
	"github.com/icinga/icingadb/pkg/logging"
	"github.com/icinga/icingadb/pkg/types"
	"github.com/jmoiron/sqlx"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"sync"
	"time"
)

type ruleID = int64

type EventHandler struct {
	Object   *object.Object
	Event    *event.Event
	Incident *incident.Incident

	Rules      map[ruleID]struct{}
	Recipients map[recipient.Key]common.ContactRole

	db            *icingadb.DB
	logger        *zap.SugaredLogger
	runtimeConfig *config.RuntimeConfig

	// timer calls RetriggerEscalations the next time any escalation could be reached on the incident.
	//
	// For example, if there are escalations configured for incident_age>=1h and incident_age>=2h, if the incident
	// is less than an hour old, timer will fire 1h after incident start, if the incident is between 1h and 2h
	// old, timer will fire after 2h, and if the incident is already older than 2h, no future escalations can
	// be reached solely based on the incident aging, so no more timer is necessary and timer stores nil.
	timer *time.Timer

	sync.Mutex
}

// Notification is used to cache a set of incident/nonincident history fields of type Notified.
//
// The event processing workflow is performed in a separate transaction before trying to send the actual
// notifications. Thus, all resulting notification entries are marked as pending, and it creates a reference
// to them of this type. The cached entries are then used to actually notify the contacts and mark the pending
// notification entries as either NotificationStateSent or NotificationStateFailed.
type Notification struct {
	HistoryId int64 `db:"id"`
	ContactID int64
	ChannelId int64
	State     common.NotificationState `db:"notification_state"` // TODO: extract from incident package
	SentAt    types.UnixMilli          `db:"sent_at"`
}

// TableName implements the contracts.TableNamer interface.
func (n *Notification) TableName() string {
	return "history"
}

func NewEventHandler(
	db *icingadb.DB, runtimeConfig *config.RuntimeConfig, logger *zap.SugaredLogger, obj *object.Object, ev *event.Event, i *incident.Incident,
) *EventHandler {
	return &EventHandler{
		db:            db,
		runtimeConfig: runtimeConfig,
		logger:        logger,
		Object:        obj,
		Event:         ev,
		Incident:      i,
		Rules:         map[ruleID]struct{}{},
		Recipients:    make(map[recipient.Key]common.ContactRole),
	}
}

// ProcessEvent from an event.Event.
//
// This function first gets this Event's object.Object and its incident.Incident. Then, after performing some safety
// checks, it calls the handle method.
//
// The returned error might be wrapped around incident.ErrSuperfluousStateChange.
func ProcessEvent(
	ctx context.Context,
	db *icingadb.DB,
	logs *logging.Logging,
	runtimeConfig *config.RuntimeConfig,
	ev *event.Event,
) error {
	obj, err := object.FromEvent(ctx, db, ev)
	if err != nil {
		return errors.Wrap(err, "cannot sync event object")
	}

	createIncident := ev.Severity != event.SeverityNone && ev.Severity != event.SeverityOK
	currentIncident, err := incident.GetCurrent(
		ctx,
		db,
		obj,
		logs.GetChildLogger("incident"),
		runtimeConfig,
		createIncident)
	if err != nil {
		return errors.Wrapf(err, "cannot get current incident for %q", obj.DisplayName())
	}

	var logger *zap.SugaredLogger
	if currentIncident == nil {
		switch {
		case ev.Type == event.TypeAcknowledgement:
			return errors.Errorf("%q does not have an active incident, ignoring acknowledgement event from source %d",
				obj.DisplayName(), ev.SourceId)
		case ev.Type == event.TypeState:
			if ev.Severity != event.SeverityOK {
				return errors.Errorf("cannot process event with a non-OK state without a known incident")
			}
			return errors.Wrapf(incident.ErrSuperfluousStateChange, " ok state event from source %d", ev.SourceId)
		default:
			logger = logs.GetChildLogger("non-incident").With(zap.String("object", obj.DisplayName()), zap.String("event", ev.String()))
		}
	} else {
		logger = currentIncident.Logger()
	}

	return NewEventHandler(db, runtimeConfig, logger, obj, ev, currentIncident).handle(ctx)
}

func (eh *EventHandler) handle(ctx context.Context) error {
	eh.Lock()
	defer eh.Unlock()

	eh.runtimeConfig.RLock()
	defer eh.runtimeConfig.RUnlock()

	tx, err := eh.db.BeginTxx(ctx, nil)
	if err != nil {
		eh.logger.Errorw("Can't start a db transaction", zap.Error(err))

		return errors.New("can't start a db transaction")
	}
	defer func() { _ = tx.Rollback() }()

	ev := eh.Event
	if err = eh.Event.Sync(ctx, tx, eh.db, eh.Object.ID); err != nil {
		eh.logger.Errorw("Failed to insert event and fetch its ID", zap.String("event", ev.String()), zap.Error(err))

		return errors.New("can't insert event and fetch its ID")
	}

	var causedBy types.Int
	i := eh.Incident
	if i != nil {
		causedBy, err = i.ProcessEvent(ctx, tx, ev)
		if err != nil {
			return err
		}

		if i.IsNew { //TODO: handle logger properly....
			eh.logger = eh.logger.With(zap.String("incident", i.String()))
		}

		// recovered
		if !i.IsNew && i.Severity == event.SeverityOK && eh.timer != nil {
			eh.timer.Stop()
			eh.timer = nil
		}
	}

	// Check if any (additional) rules match this object. Filters of rules that already have a state don't have
	// to be checked again, these rules already matched and stay effective for the ongoing incident.
	causedBy, err = eh.evaluateRules(ctx, tx, ev.ID, causedBy)
	if err != nil {
		return err
	}

	// Re-evaluate escalations based on the newly evaluated rules.
	escalations, err := eh.evaluateEscalations()
	if err != nil {
		return err
	}

	if len(escalations) == 0 {
		// No non-state recipients configured, sent to all incident recipients
		if eh.Incident != nil && ev.Type != event.TypeState {
			eh.Recipients = eh.Incident.Recipients
		}
	} else if err := eh.triggerEscalations(ctx, tx, ev, causedBy, escalations); err != nil {
		return err
	}

	//TODO notificationEntry is dependent on incident. Make a general struct, which can be inherited to this(incident based notificationEntry), if an incident notification is triggred
	notifications, err := eh.addPendingNotifications(ctx, tx, ev, causedBy)
	if err != nil {
		return err
	}

	if err = tx.Commit(); err != nil {
		eh.logger.Errorw("Can't commit db transaction", zap.Error(err))

		return errors.New("can't commit db transaction")
	}

	return eh.notifyContacts(ctx, ev, notifications)
}

// evaluateRules evaluates all the configured rules for this *incident.Object and
// generates history entries for each matched rule.
// Returns error on database failure.
func (eh *EventHandler) evaluateRules(ctx context.Context, tx *sqlx.Tx, eventID int64, causedBy types.Int) (types.Int, error) {
	if eh.Rules == nil {
		eh.Rules = make(map[int64]struct{})
	}

	for _, r := range eh.runtimeConfig.Rules {
		if !r.IsActive.Valid || !r.IsActive.Bool {
			continue
		}

		var matched bool
		var err error
		if _, ok := eh.Rules[r.ID]; !ok {
			if r.ObjectFilter != nil {
				matched, err = r.ObjectFilter.Eval(eh.Object)
				if err != nil {
					eh.logger.Warnw("Failed to evaluate object filter", zap.String("rule", r.Name), zap.Error(err))
				}

				if err != nil || !matched {
					continue
				}
			}

			eh.Rules[r.ID] = struct{}{}
			eh.logger.Infof("Rule %q matches", r.Name)

			if eh.Incident != nil {
				causedBy, err = eh.Incident.HandleRuleMatched(ctx, tx, r, eventID, causedBy)
				if err != nil {
					return causedBy, err
				}
			}
		}
	}

	return causedBy, nil
}

// evaluateEscalations evaluates this incidents rule escalations to be triggered if they aren't already.
// Returns the newly evaluated escalations to be triggered or an error on database failure.
func (eh *EventHandler) evaluateEscalations() ([]*rule.EscalationTemplate, error) {
	ev := eh.Event
	var escalations []*rule.EscalationTemplate
	var err error
	if ev.Type == event.TypeState {
		escalations, err = eh.EvaluateIncidentEscalations(ev)
		if err != nil {
			return nil, err
		}

		return escalations, nil
	}

	nonStateEscFilter := &rule.EscalationFilter{Type: ev.Type}
	for rID := range eh.Rules {
		r := eh.runtimeConfig.Rules[rID]

		if r == nil || !r.IsActive.Valid || !r.IsActive.Bool {
			continue
		}

		for _, escalation := range r.NonStateEscalations {
			matched := false
			var err error

			if escalation.Condition == nil {
				matched = true
			} else {
				matched, err = escalation.Condition.Eval(nonStateEscFilter)
				if err != nil {
					eh.logger.Warnw(
						"Failed to evaluate non-state escalation condition", zap.String("rule", r.Name),
						zap.String("non-state escalation", escalation.DisplayName()), zap.Error(err),
					)

					matched = false
				}
			}

			if matched {
				escalations = append(escalations, escalation.EscalationTemplate)
			}
		}
	}

	return escalations, nil
}

// RetriggerEscalations tries to re-evaluate the escalations and notify contacts.
func (eh *EventHandler) RetriggerEscalations(ev *event.Event) {
	eh.Lock()
	defer eh.Unlock()

	eh.runtimeConfig.RLock()
	defer eh.runtimeConfig.RUnlock()

	if eh.Incident == nil || !eh.Incident.RecoveredAt.IsZero() {
		// Incident is recovered in the meantime.
		return
	}

	if !time.Now().After(ev.Time) {
		eh.logger.DPanicw("Event from the future", zap.Time("event_time", ev.Time), zap.Any("event", ev))
		return
	}

	escalations, err := eh.EvaluateIncidentEscalations(ev)
	if err != nil {
		eh.logger.Errorw("Reevaluating time-based escalations failed", zap.Error(err))
		return
	}

	if len(escalations) == 0 {
		eh.logger.Debug("Reevaluated escalations, no new escalations triggered")
		return
	}

	var notifications []*Notification
	ctx := context.Background()
	err = utils.RunInTx(ctx, eh.db, func(tx *sqlx.Tx) error {
		err := ev.Sync(ctx, tx, eh.db, eh.Object.ID)
		if err != nil {
			return err
		}

		if err = eh.Incident.AddEvent(ctx, tx, ev); err != nil {
			return errors.Wrap(err, "can't insert incident event to the database")
		}

		channels := make(incident.ContactChannels)
		for _, escalation := range escalations {
			r := eh.runtimeConfig.Rules[escalation.RuleID]
			if err := eh.Incident.TriggerEscalation(ctx, tx, ev, types.Int{}, escalation, r); err != nil {
				return err
			}

			channels.LoadEscalationRecipientsChannel(escalation.Recipients, eh.Incident, ev.Time)
		}

		notifications, err = eh.addPendingNotifications(ctx, tx, ev, types.Int{})

		return err
	})
	if err != nil {
		eh.logger.Errorw("Reevaluating time-based escalations failed", zap.Error(err))
		return
	}

	if err = eh.notifyContacts(ctx, ev, notifications); err != nil {
		eh.logger.Errorw("Failed to notify reevaluated escalation recipients", zap.Error(err))
		return
	}

	eh.logger.Info("Successfully reevaluated time-based escalations")
}

// EvaluateIncidentEscalations returns incident escalations from which recipients are fetched.
// If a non-state event is triggered, this method will return all given escalations (in case no non-state rule is defined),
// otherwise empty slice is returned.
func (eh *EventHandler) EvaluateIncidentEscalations(ev *event.Event) ([]*rule.EscalationTemplate, error) {
	if eh.Incident == nil {
		return nil, errors.New("undefined incident")
	}

	// Escalations are reevaluated now, reset any existing timer, if there might be future time-based escalations,
	// this function will start a new timer.
	if eh.timer != nil {
		eh.logger.Info("Stopping reevaluate timer due to escalation evaluation")
		eh.timer.Stop()
		eh.timer = nil
	}

	filterContext := &rule.EscalationFilter{IncidentAge: ev.Time.Sub(eh.Incident.StartedAt), IncidentSeverity: eh.Incident.Severity}
	var escalations []*rule.EscalationTemplate
	retryAfter := rule.RetryNever

	for rID := range eh.Rules {
		r := eh.runtimeConfig.Rules[rID]

		if r == nil || !r.IsActive.Valid || !r.IsActive.Bool {
			continue
		}

		// Check if new escalation stages are reached
		for _, escalation := range r.Escalations {
			matched := false
			if _, ok := eh.Incident.EscalationState[escalation.ID]; !ok {
				if escalation.Condition == nil {
					matched = true
				} else {
					var err error
					matched, err = escalation.Condition.Eval(filterContext)
					if err != nil {
						eh.logger.Warnw(
							"Failed to evaluate escalation condition", zap.String("rule", r.Name),
							zap.String("escalation", escalation.DisplayName()), zap.Error(err),
						)

						matched = false
					} else if !matched {
						incidentAgeFilter := filterContext.ReevaluateAfter(escalation.Condition)
						retryAfter = min(retryAfter, incidentAgeFilter)
					}
				}
			}

			if matched {
				escalations = append(escalations, escalation.EscalationTemplate)
			}
		}
	}

	if retryAfter != rule.RetryNever {
		// The retryAfter duration is relative to the incident duration represented by the escalation filter,
		// i.e. if an incident is 15m old and an escalation rule evaluates incident_age>=1h the retryAfter would
		// contain 45m (1h - incident age (15m)). Therefore, we have to use the event time instead of the incident
		// start time here.
		nextEvalAt := ev.Time.Add(retryAfter)

		eh.logger.Infow("Scheduling escalation reevaluation", zap.Duration("after", retryAfter), zap.Time("at", nextEvalAt))
		eh.timer = time.AfterFunc(retryAfter, func() {
			eh.logger.Info("Reevaluating escalations")

			eh.RetriggerEscalations(&event.Event{
				Type:    event.TypeInternal,
				Time:    nextEvalAt,
				Message: fmt.Sprintf("Incident reached age %v", nextEvalAt.Sub(eh.Incident.StartedAt)),
			})
		})
	}

	return escalations, nil
}

// triggerEscalations triggers the given escalations and generates incident history items for each of them.
// Returns an error on database failure.
func (eh *EventHandler) triggerEscalations(ctx context.Context, tx *sqlx.Tx, ev *event.Event, causedBy types.Int, escalations []*rule.EscalationTemplate) error {
	for _, escalation := range escalations {
		r := eh.runtimeConfig.Rules[escalation.RuleID]

		if eh.Incident != nil {
			err := eh.Incident.TriggerEscalation(ctx, tx, ev, causedBy, escalation, r)
			if err != nil {
				return err
			}
			eh.Recipients = eh.Incident.Recipients
		} else {
			err := eh.AddRecipient(ctx, tx, escalation, ev.ID)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// AddRecipient adds recipient from the given *rule.Escalation to this incident.
// Syncs also all the recipients with the database and returns an error on db failure.
func (eh *EventHandler) AddRecipient(ctx context.Context, tx *sqlx.Tx, escalation *rule.EscalationTemplate, eventId int64) error {
	//TODO: either trigger i.AddRecipient here or remove unused ctx,tx params
	for _, escalationRecipient := range escalation.Recipients {
		r := escalationRecipient.Recipient

		recipientKey := recipient.ToKey(r)

		_, ok := eh.Recipients[recipientKey]
		if !ok {
			eh.Recipients[recipientKey] = common.RoleRecipient
		}
	}

	return nil
}

func (eh *EventHandler) getRecipientsChannel() incident.ContactChannels {
	eventTime := eh.Event.Time
	contactChs := make(incident.ContactChannels)
	i := eh.Incident
	if i != nil {
		contactChs = i.GetRecipientsChannel(eventTime)
	}

	// Check whether all the incident recipients do have an appropriate contact channel configured.
	// When a recipient has subscribed/managed this incident via the UI or using an ACK, fallback
	// to the default contact channel.
	for recipientKey, role := range eh.Recipients {
		r := eh.runtimeConfig.GetRecipient(recipientKey)
		if r == nil {
			continue
		}

		if i == nil || i.IsNotifiable(role) {
			for _, contact := range r.GetContactsAt(eventTime) {
				if contactChs[contact] == nil {
					contactChs[contact] = make(map[int64]bool)
					contactChs[contact][contact.DefaultChannelID] = true
				}
			}
		}
	}

	return contactChs
}

func (eh *EventHandler) addPendingNotifications(ctx context.Context, tx *sqlx.Tx, ev *event.Event, causedBy types.Int) ([]*Notification, error) {
	//TODO use general struct here and add to NOtificationEntry if incident is given, for history entry
	var notifications []*Notification
	for contact, channels := range eh.getRecipientsChannel() {
		for chID := range channels {
			var err error
			var historyId int64
			if eh.Incident != nil {
				historyId, err = eh.Incident.AddPendingNotificationHistory(ctx, tx, ev.ID, contact, causedBy, chID)
				if err != nil {
					return nil, err
				}
			} else {
				hr := &common.HistoryRow{
					ObjectID:          eh.Object.ID,
					Key:               recipient.ToKey(contact),
					EventID:           utils.ToDBInt(ev.ID),
					Time:              types.UnixMilli(time.Now()),
					Type:              common.Notified,
					ChannelID:         utils.ToDBInt(chID),
					CausedByHistoryID: causedBy,
					NotificationState: common.NotificationStatePending,
				}

				id, err := common.AddHistory(eh.db, ctx, tx, hr, true)
				if err != nil {
					eh.logger.Errorw(
						"Failed to insert contact pending notification non-incident history",
						zap.String("contact", contact.String()),
						zap.Error(err))

					return nil, fmt.Errorf("failed to insert contact pending notification non-incident history")
				}

				historyId = id.Int64
			}

			notifications = append(notifications, &Notification{
				HistoryId: historyId,
				ContactID: contact.ID,
				ChannelId: chID,
				State:     common.NotificationStatePending,
			})
		}
	}

	return notifications, nil
}

func (eh *EventHandler) notifyContacts(ctx context.Context, ev *event.Event, notifications []*Notification) error {
	for _, n := range notifications {
		contact := eh.runtimeConfig.Contacts[n.ContactID]

		if eh.notifyContact(contact, ev, n.ChannelId) != nil {
			n.State = common.NotificationStateFailed
		} else {
			n.State = common.NotificationStateSent
		}

		n.SentAt = types.UnixMilli(time.Now())

		if err := eh.addNotifiedHistory(ctx, n, contact.String()); err != nil {
			return err
		}
	}

	return nil
}

func (eh *EventHandler) addNotifiedHistory(ctx context.Context, n *Notification, contactStr string) error {
	stmt, _ := eh.db.BuildUpdateStmt(n)
	if _, err := eh.db.NamedExecContext(ctx, stmt, n); err != nil {
		eh.logger.Errorw(
			"Failed to update contact notified history", zap.String("contact", contactStr),
			zap.Error(err),
		)
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	return nil
}

// notifyContact notifies the given recipient via a channel matching the given ID.
func (eh *EventHandler) notifyContact(contact *recipient.Contact, ev *event.Event, chID int64) error {
	ch := eh.runtimeConfig.Channels[chID]
	if ch == nil {
		eh.logger.Errorw("Could not find config for channel", zap.Int64("channel_id", chID))

		return fmt.Errorf("could not find config for channel ID: %d", chID)
	}

	eh.logger.Infow(fmt.Sprintf("Notify contact %q via %q of type %q", contact.FullName, ch.Name, ch.Type), zap.Int64("channel_id", chID))

	//TODO: Incident can be nil
	fakeInc := incident.NewIncident(eh.db, eh.Object, eh.runtimeConfig, eh.logger)
	err := ch.Notify(contact, fakeInc, ev, daemon.Config().Icingaweb2URL)
	if err != nil {
		eh.logger.Errorw("Failed to send notification via channel plugin", zap.String("type", ch.Type), zap.Error(err))
		return err
	}

	eh.logger.Infow(
		"Successfully sent a notification via channel plugin", zap.String("type", ch.Type), zap.String("contact", contact.FullName),
	)

	return nil
}
