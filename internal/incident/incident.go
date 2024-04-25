package incident

import (
	"context"
	"errors"
	"fmt"
	baseEv "github.com/icinga/icinga-go-library/notifications/event"
	"github.com/icinga/icinga-go-library/notifications/plugin"
	"github.com/icinga/icinga-go-library/types"
	"github.com/icinga/icinga-notifications/internal/config"
	"github.com/icinga/icinga-notifications/internal/daemon"
	"github.com/icinga/icinga-notifications/internal/event"
	"github.com/icinga/icinga-notifications/internal/object"
	"github.com/icinga/icinga-notifications/internal/recipient"
	"github.com/icinga/icinga-notifications/internal/rule"
	"github.com/jmoiron/sqlx"
	"go.uber.org/zap"
	"strconv"
	"sync"
	"time"
)

type Incident struct {
	ID          int64           `db:"id"`
	ObjectID    types.Binary    `db:"object_id"`
	StartedAt   types.UnixMilli `db:"started_at"`
	RecoveredAt types.UnixMilli `db:"recovered_at"`
	Severity    baseEv.Severity `db:"severity"`

	Object *object.Object `db:"-"`

	EscalationState map[int64]*EscalationState        `db:"-"`
	Rules           map[int64]struct{}                `db:"-"`
	Recipients      map[recipient.Key]*RecipientState `db:"-"`

	// timer calls RetriggerEscalations the next time any escalation could be reached on the incident.
	//
	// For example, if there are escalations configured for incident_age>=1h and incident_age>=2h, if the incident
	// is less than an hour old, timer will fire 1h after incident start, if the incident is between 1h and 2h
	// old, timer will fire after 2h, and if the incident is already older than 2h, no future escalations can
	// be reached solely based on the incident aging, so no more timer is necessary and timer stores nil.
	timer *time.Timer

	// isMuted indicates whether the current Object was already muted before the ongoing event.Event being processed.
	// This prevents us from generating multiple muted histories when receiving several events that mute our Object.
	isMuted bool

	config.Resources // Contains common resources such as db, logger and runtimeConfig.

	sync.Mutex
}

// NewIncident creates a new incident for the given [object.Object].
//
// If obj is nil, the returned incident won't be associated with any object, and its ObjectID will be zeroed.
// The returned incident won't be persisted to the database, that has to be done by the caller using [Incident.Sync].
//
// Additionally, this function initializes the incident resources using the given runtimeConfig, and sets up
// the logger to include the object display name if obj is not nil. Otherwise, the logger won't contain any
// object or incident specific fields.
func NewIncident(obj *object.Object, runtimeConfig *config.RuntimeConfig) *Incident {
	i := &Incident{
		Object:          obj,
		EscalationState: map[int64]*EscalationState{},
		Rules:           map[int64]struct{}{},
		Recipients:      map[recipient.Key]*RecipientState{},
		Resources:       config.MakeResources(runtimeConfig, "incident"),
	}

	if obj != nil {
		i.ObjectID = obj.ID
		i.Logger = i.Logger.With(zap.String("object", obj.DisplayName()))
	}

	return i
}

func (i *Incident) String() string {
	return fmt.Sprintf("#%d", i.ID)
}

func (i *Incident) HasManager() bool {
	for recipientKey, state := range i.Recipients {
		if i.RuntimeConfig.GetRecipient(recipientKey) == nil {
			i.Logger.Debugw("Incident refers unknown recipient key, might got deleted", zap.Inline(recipientKey))
			continue
		}
		if state.Role == RoleManager {
			return true
		}
	}

	return false
}

// IsNotifiable returns whether contacts in the given role should be notified about this incident.
//
// For a managed incident, only managers and subscribers should be notified, for unmanaged incidents,
// regular recipients are notified as well.
func (i *Incident) IsNotifiable(role ContactRole) bool {
	if !i.HasManager() {
		return true
	}

	return role > RoleRecipient
}

// ProcessEvent processes the given event for the current incident in an own transaction.
func (i *Incident) ProcessEvent(ctx context.Context, ev *event.Event) error {
	i.Lock()
	defer i.Unlock()

	i.RuntimeConfig.RLock()
	defer i.RuntimeConfig.RUnlock()

	// These event types are not like the others used to mute an object/incident, such as DowntimeStart, which
	// uniquely identify themselves why an incident is being muted, but are rather super generic types, and as
	// such, we are ignoring superfluous ones that don't have any effect on that incident.
	if i.isMuted && ev.Type == baseEv.TypeMute {
		i.Logger.Debugw("Ignoring superfluous mute event", zap.String("event", ev.String()))
		return event.ErrSuperfluousMuteUnmuteEvent
	} else if !i.isMuted && ev.Type == baseEv.TypeUnmute {
		i.Logger.Debugw("Ignoring superfluous unmute event", zap.String("event", ev.String()))
		return event.ErrSuperfluousMuteUnmuteEvent
	}

	tx, err := i.DB.BeginTxx(ctx, nil)
	if err != nil {
		i.Logger.Errorw("Cannot start a db transaction", zap.Error(err))
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if err = ev.Sync(ctx, tx, i.DB, i.Object.ID); err != nil {
		i.Logger.Errorw("Failed to insert event and fetch its ID", zap.String("event", ev.String()), zap.Error(err))
		return err
	}

	isNew := i.StartedAt.Time().IsZero()
	if isNew {
		err = i.processIncidentOpenedEvent(ctx, tx, ev)
		if err != nil {
			return err
		}

		i.Logger = i.Logger.With(zap.String("incident", i.String()))
	}

	if err = i.AddEvent(ctx, tx, ev); err != nil {
		i.Logger.Errorw("Cannot insert incident event to the database", zap.Error(err))
		return err
	}

	if err := i.handleMuteUnmute(ctx, tx, ev); err != nil {
		i.Logger.Errorw("Cannot insert incident muted history", zap.String("event", ev.String()), zap.Error(err))
		return err
	}

	switch ev.Type {
	case baseEv.TypeState:
		if !isNew {
			if err := i.processSeverityChangedEvent(ctx, tx, ev); err != nil {
				return err
			}
		}

		if err := i.applyMatchingRules(ctx, tx, ev); err != nil {
			return err
		}

		if err := i.triggerEscalations(ctx, tx, ev, i.evaluateEscalations(ev.Time)); err != nil {
			return err
		}
	case baseEv.TypeAcknowledgementSet:
		if err := i.processAcknowledgementEvent(ctx, tx, ev); err != nil {
			if errors.Is(err, errSuperfluousAckEvent) {
				// That ack error type indicates that the acknowledgement author was already a manager, thus
				// we can safely ignore that event and return without even committing the DB transaction.
				return nil
			}

			return err
		}
	}

	var notifications []*NotificationEntry
	notifications, err = i.generateNotifications(ctx, tx, ev, i.getRecipientsChannel(ev.Time))
	if err != nil {
		return err
	}

	if err = tx.Commit(); err != nil {
		i.Logger.Errorw("Cannot commit db transaction", zap.Error(err))
		return err
	}

	// We've just committed the DB transaction and can safely update the incident muted flag.
	i.isMuted = i.Object.IsMuted()

	return i.notifyContacts(ctx, ev, notifications)
}

// RetriggerEscalations tries to re-evaluate the escalations and notify contacts.
func (i *Incident) RetriggerEscalations(ev *event.Event) {
	i.Lock()
	defer i.Unlock()

	i.RuntimeConfig.RLock()
	defer i.RuntimeConfig.RUnlock()

	if !i.RecoveredAt.Time().IsZero() {
		// Incident is recovered in the meantime.
		return
	}

	if !time.Now().After(ev.Time) {
		i.Logger.DPanicw("Event from the future", zap.Time("event_time", ev.Time), zap.Any("event", ev))
		return
	}

	escalations := i.evaluateEscalations(ev.Time)
	if len(escalations) == 0 {
		i.Logger.Debug("Reevaluated escalations, no new escalations triggered")
		return
	}

	var notifications []*NotificationEntry
	ctx := context.Background()
	err := i.DB.ExecTx(ctx, func(ctx context.Context, tx *sqlx.Tx) error {
		err := ev.Sync(ctx, tx, i.DB, i.Object.ID)
		if err != nil {
			return err
		}

		if err = i.AddEvent(ctx, tx, ev); err != nil {
			return fmt.Errorf("cannot insert incident event to the database: %w", err)
		}

		if err = i.triggerEscalations(ctx, tx, ev, escalations); err != nil {
			return err
		}

		channels := make(rule.ContactChannels)
		for _, escalation := range escalations {
			channels.LoadFromEntryRecipients(escalation, ev.Time, i.isRecipientNotifiable)
		}

		notifications, err = i.generateNotifications(ctx, tx, ev, channels)
		return err
	})
	if err != nil {
		i.Logger.Errorw("Reevaluating time-based escalations failed", zap.Error(err))
	} else {
		if err = i.notifyContacts(ctx, ev, notifications); err != nil {
			i.Logger.Errorw("Failed to notify reevaluated escalation recipients", zap.Error(err))
			return
		}

		i.Logger.Info("Successfully reevaluated time-based escalations")
	}
}

// MakeNotificationRequest creates a notification request for the current incident and the given event.
//
// The returned request doesn't contain any contact information, that has to be filled in by the caller.
// The URL of the incident is constructed using the Icinga Web 2 URL configured in the daemon configuration.
func (i *Incident) MakeNotificationRequest(ev *event.Event) *plugin.NotificationRequest {
	incidentUrl := daemon.Config().IcingaWeb2UrlParsed.JoinPath("/notifications/incident")
	incidentUrl.RawQuery = fmt.Sprintf("id=%d", i.ID)

	return &plugin.NotificationRequest{
		Object: &plugin.Object{
			Name: i.Object.DisplayName(),
			Url:  ev.URL,
			Tags: i.Object.Tags,
		},
		Incident: &plugin.Incident{
			Id:       i.ID,
			Url:      incidentUrl.String(),
			Severity: i.Severity,
		},
		Event: &plugin.Event{
			Time:     ev.Time,
			Type:     ev.Type,
			Username: ev.Username,
			Message:  ev.Message,
		},
	}
}

func (i *Incident) processSeverityChangedEvent(ctx context.Context, tx *sqlx.Tx, ev *event.Event) error {
	oldSeverity := i.Severity
	newSeverity := ev.Severity
	if oldSeverity == newSeverity {
		i.Logger.Debugw("Ignoring superfluous severity change event", zap.Int64("source_id", ev.SourceId), zap.Stringer("event", ev))
		return event.ErrSuperfluousStateChange
	}

	i.Logger.Infof("Incident severity changed from %s to %s", oldSeverity.String(), newSeverity.String())

	hr := &HistoryRow{
		IncidentID:  i.ID,
		EventID:     types.MakeInt(ev.ID, types.TransformZeroIntToNull),
		Time:        types.UnixMilli(time.Now()),
		Type:        IncidentSeverityChanged,
		NewSeverity: newSeverity,
		OldSeverity: oldSeverity,
		Message:     types.MakeString(ev.Message, types.TransformEmptyStringToNull),
	}

	if err := hr.Sync(ctx, i.DB, tx); err != nil {
		i.Logger.Errorw("Failed to insert incident severity changed history", zap.Error(err))
		return err
	}

	if newSeverity == baseEv.SeverityOK {
		i.RecoveredAt = types.UnixMilli(time.Now())
		i.Logger.Info("All sources recovered, closing incident")

		RemoveCurrent(i.Object)

		hr = &HistoryRow{
			IncidentID: i.ID,
			EventID:    types.MakeInt(ev.ID, types.TransformZeroIntToNull),
			Time:       i.RecoveredAt,
			Type:       Closed,
		}

		if err := hr.Sync(ctx, i.DB, tx); err != nil {
			i.Logger.Errorw("Cannot insert incident closed history to the database", zap.Error(err))
			return err
		}

		if i.timer != nil {
			i.timer.Stop()
		}
	}

	i.Severity = newSeverity
	if err := i.Sync(ctx, tx); err != nil {
		i.Logger.Errorw("Failed to update incident severity", zap.Error(err))
		return err
	}

	return nil
}

func (i *Incident) processIncidentOpenedEvent(ctx context.Context, tx *sqlx.Tx, ev *event.Event) error {
	i.StartedAt = types.UnixMilli(ev.Time)
	i.Severity = ev.Severity
	if err := i.Sync(ctx, tx); err != nil {
		i.Logger.Errorw("Cannot insert incident to the database", zap.Error(err))
		return err
	}

	i.Logger.Infow(fmt.Sprintf("Source %d opened incident at severity %q", ev.SourceId, i.Severity.String()), zap.String("message", ev.Message))

	hr := &HistoryRow{
		IncidentID:  i.ID,
		Type:        Opened,
		Time:        types.UnixMilli(ev.Time),
		EventID:     types.MakeInt(ev.ID, types.TransformZeroIntToNull),
		NewSeverity: i.Severity,
		Message:     types.MakeString(ev.Message, types.TransformEmptyStringToNull),
	}

	if err := hr.Sync(ctx, i.DB, tx); err != nil {
		i.Logger.Errorw("Cannot insert incident opened history event", zap.Error(err))
		return err
	}

	return nil
}

// handleMuteUnmute generates an incident Muted or Unmuted history based on the Object state.
// Returns an error if fails to insert the generated history to the database.
func (i *Incident) handleMuteUnmute(ctx context.Context, tx *sqlx.Tx, ev *event.Event) error {
	if i.isMuted == i.Object.IsMuted() {
		return nil
	}

	hr := &HistoryRow{IncidentID: i.ID, EventID: types.MakeInt(ev.ID, types.TransformZeroIntToNull), Time: types.UnixMilli(time.Now())}
	logger := i.Logger.With(zap.String("event", ev.String()))
	if i.Object.IsMuted() {
		hr.Type = Muted
		// Since the object may have already been muted with previous events before this incident even
		// existed, we have to use the mute reason from this object and not from the ongoing event.
		hr.Message = i.Object.MuteReason
		logger.Infow("Muting incident", zap.String("reason", i.Object.MuteReason.String))
	} else {
		hr.Type = Unmuted
		// On the other hand, if an object is unmuted, its mute reason is already reset, and we can't access it anymore.
		hr.Message = types.MakeString(ev.MuteReason, types.TransformEmptyStringToNull)
		logger.Infow("Unmuting incident", zap.String("reason", ev.MuteReason))
	}

	return hr.Sync(ctx, i.DB, tx)
}

// applyMatchingRules walks through the rule IDs obtained from source and generates a RuleMatched history entry.
func (i *Incident) applyMatchingRules(ctx context.Context, tx *sqlx.Tx, ev *event.Event) error {
	if i.Rules == nil {
		i.Rules = make(map[int64]struct{})
	}

	for _, ruleId := range ev.RuleIds {
		ruleIdInt, err := strconv.ParseInt(ruleId, 10, 64)
		if err != nil {
			i.Logger.Errorw("Event rule is not an integer", zap.String("rule_id", ruleId), zap.Error(err))
			return fmt.Errorf("cannot convert rule id %q to an int: %w", ruleId, err)
		}

		r, ok := i.RuntimeConfig.Rules[ruleIdInt]
		if !ok {
			i.Logger.Errorw("Event refers to non-existing event rule, might got deleted", zap.Int64("rule_id", ruleIdInt))
			return fmt.Errorf("cannot apply unknown rule %d", ruleIdInt)
		}

		if r.SourceID != ev.SourceId {
			i.Logger.Errorw("Rule source ID does not match event source ID",
				zap.Int64("event_source_id", ev.SourceId),
				zap.Int64("rule_source_id", r.SourceID),
				zap.Int64("rule_id", ruleIdInt))
			return fmt.Errorf("rule %d source ID %d does not match event source %d", ruleIdInt, r.SourceID, ev.SourceId)
		}

		if _, ok := i.Rules[r.ID]; !ok {
			i.Rules[r.ID] = struct{}{}
			i.Logger.Infow("Rule matches", zap.Object("rule", r))

			err := i.AddRuleMatched(ctx, tx, r)
			if err != nil {
				i.Logger.Errorw("Failed to upsert incident rule", zap.Object("rule", r), zap.Error(err))
				return err
			}

			hr := &HistoryRow{
				IncidentID: i.ID,
				Time:       types.UnixMilli(time.Now()),
				EventID:    types.MakeInt(ev.ID, types.TransformZeroIntToNull),
				RuleID:     types.MakeInt(r.ID, types.TransformZeroIntToNull),
				Type:       RuleMatched,
			}
			if err := hr.Sync(ctx, i.DB, tx); err != nil {
				i.Logger.Errorw("Failed to insert rule matched incident history", zap.Object("rule", r), zap.Error(err))
				return err
			}
		}
	}

	return nil
}

// evaluateEscalations evaluates all the escalations of the current incident's rules and returns the ones that matched.
//
// It also sets up a timer to re-evaluate the escalations when necessary, for example when an escalation
// condition is based on the incident age and the incident is not old enough yet to match that condition.
//
// Note that this function does not trigger the matched escalations, it only evaluates them and returns
// the ones that matched. It's the caller's responsibility to trigger them afterwards.
func (i *Incident) evaluateEscalations(eventTime time.Time) config.RuleEntries {
	// Escalations are reevaluated now, reset any existing timer, if there might be future time-based escalations,
	// this function will start a new timer.
	if i.timer != nil {
		i.Logger.Info("Stopping reevaluate timer due to escalation evaluation")
		i.timer.Stop()
		i.timer = nil
	}

	filterContext := &rule.EscalationFilter{IncidentAge: eventTime.Sub(i.StartedAt.Time()), IncidentSeverity: i.Severity}
	escalations := make(config.RuleEntries)

	// EvaluateRuleEntries only returns an error if one of the provided callback hooks returns
	// an error or the OnError handler returns false, and since none of our callbacks return an
	// error nor false, we can safely discard the return value here.
	_ = escalations.Evaluate(i.Resources, filterContext, i.Rules, config.EvalOptions{
		// Prevent reevaluation of an already triggered escalation via the pre run hook.
		OnPreEvaluate: func(escalation *rule.Entry) bool {
			return i.EscalationState == nil || i.EscalationState[escalation.ID] == nil
		},
		OnError: func(escalation *rule.Entry, err error) bool {
			r := i.RuntimeConfig.Rules[escalation.RuleID]
			i.Logger.Warnw("Failed to evaluate escalation condition", zap.Object("rule", r),
				zap.Object("escalation", escalation), zap.Error(err))
			return true
		},
		OnAllConfigEvaluated: func(retryAfter time.Duration) {
			if retryAfter != rule.RetryNever {
				// The retryAfter duration is relative to the incident duration represented by the escalation filter,
				// i.e. if an incident is 15m old and an escalation rule evaluates incident_age>=1h the retryAfter
				// would contain 45m (1h - incident age (15m)). Therefore, we have to use the event time instead of
				// the incident start time here.
				nextEvalAt := eventTime.Add(retryAfter)

				i.Logger.Infow("Scheduling escalation reevaluation", zap.Duration("after", retryAfter), zap.Time("at", nextEvalAt))
				i.timer = time.AfterFunc(retryAfter, func() {
					i.Logger.Info("Reevaluating escalations")

					i.RetriggerEscalations(&event.Event{
						Time: nextEvalAt,
						Event: baseEv.Event{
							Type:    baseEv.TypeIncidentAge,
							Message: fmt.Sprintf("Incident reached age %v", nextEvalAt.Sub(i.StartedAt.Time())),
						},
					})
				})
			}
		},
	})

	return escalations
}

// triggerEscalations triggers the given escalations for the current incident.
//
// For each escalation, it creates an [EscalationState] entry, generates an [EscalationTriggered]
// history entry and adds the escalation recipients to the incident recipients cache. If any of these
// operations fail, the function returns an error and stops processing further escalations.
func (i *Incident) triggerEscalations(ctx context.Context, tx *sqlx.Tx, ev *event.Event, escalations config.RuleEntries) error {
	if i.EscalationState == nil {
		i.EscalationState = make(map[int64]*EscalationState)
	}

	for _, escalation := range escalations {
		r := i.RuntimeConfig.Rules[escalation.RuleID]
		if r == nil {
			i.Logger.Debugw("Incident refers unknown rule, might got deleted", zap.Int64("rule_id", escalation.RuleID))
			continue
		}

		i.Logger.Infow("Rule reached escalation", zap.Object("rule", r), zap.Object("escalation", escalation))

		state := &EscalationState{RuleEscalationID: escalation.ID, TriggeredAt: types.UnixMilli(time.Now())}
		i.EscalationState[escalation.ID] = state

		if err := i.AddEscalationTriggered(ctx, tx, state); err != nil {
			i.Logger.Errorw(
				"Failed to upsert escalation state", zap.Object("rule", r),
				zap.Object("escalation", escalation), zap.Error(err),
			)
			return err
		}

		hr := &HistoryRow{
			IncidentID:  i.ID,
			Time:        state.TriggeredAt,
			EventID:     types.MakeInt(ev.ID, types.TransformZeroIntToNull),
			RuleEntryID: types.MakeInt(state.RuleEscalationID, types.TransformZeroIntToNull),
			RuleID:      types.MakeInt(r.ID, types.TransformZeroIntToNull),
			Type:        EscalationTriggered,
		}

		if err := hr.Sync(ctx, i.DB, tx); err != nil {
			i.Logger.Errorw(
				"Failed to insert escalation triggered incident history", zap.Object("rule", r),
				zap.Object("escalation", escalation), zap.Error(err),
			)
			return err
		}

		if err := i.AddRecipient(ctx, tx, escalation, ev.ID); err != nil {
			return err
		}
	}

	return nil
}

// notifyContacts sends notifications to the given contacts via their configured channels.
//
// It updates the given NotificationEntry states in the database, marking them as sent or failed.
// Failing to update a notification entry is logged but doesn't stop the notification process, thus
// it will only return an error if the given context is done.
func (i *Incident) notifyContacts(ctx context.Context, ev *event.Event, notifications []*NotificationEntry) error {
	req := i.MakeNotificationRequest(ev)
	for _, notification := range notifications {
		contact := i.RuntimeConfig.Contacts[notification.ContactID]
		if contact == nil {
			i.Logger.Debugw("Incident refers unknown contact, might got deleted", zap.Int64("contact_id", notification.ContactID))
			continue
		}

		if i.notifyContact(contact, req, notification.ChannelID) != nil {
			notification.State = NotificationStateFailed
		} else {
			notification.State = NotificationStateSent
		}

		notification.SentAt = types.UnixMilli(time.Now())
		stmt, _ := i.DB.BuildUpdateStmt(notification)
		if _, err := i.DB.NamedExecContext(ctx, stmt, notification); err != nil {
			i.Logger.Errorw(
				"Failed to update contact notified incident history", zap.String("contact", contact.String()),
				zap.Error(err),
			)
		}

		if err := ctx.Err(); err != nil {
			return err
		}
	}

	return nil
}

// notifyContact notifies the given recipient via a channel matching the given ID.
func (i *Incident) notifyContact(contact *recipient.Contact, req *plugin.NotificationRequest, chID int64) error {
	ch := i.RuntimeConfig.Channels[chID]
	if ch == nil {
		i.Logger.Errorw("Could not find config for channel", zap.Int64("channel_id", chID))

		return fmt.Errorf("could not find config for channel ID: %d", chID)
	}

	i.Logger.Infow(fmt.Sprintf("Notify contact %q via %q of type %q", contact.FullName, ch.Name, ch.Type),
		zap.Int64("channel_id", chID), zap.Stringer("event_type", req.Event.Type))

	contactStruct := &plugin.Contact{FullName: contact.FullName}
	for _, addr := range contact.Addresses {
		contactStruct.Addresses = append(contactStruct.Addresses, &plugin.Address{Type: addr.Type, Address: addr.Address})
	}
	req.Contact = contactStruct

	if err := ch.Notify(req); err != nil {
		i.Logger.Errorw("Failed to send notification via channel plugin", zap.String("type", ch.Type), zap.Error(err))
		return err
	}

	i.Logger.Infow("Successfully sent a notification via channel plugin", zap.String("type", ch.Type),
		zap.String("contact", contact.FullName), zap.Stringer("event_type", req.Event.Type))

	return nil
}

// errSuperfluousAckEvent is returned when the same ack author submits two successive ack set events on an incident.
// This is error is going to be used only within this incident package.
var errSuperfluousAckEvent = errors.New("superfluous acknowledgement set event, author is already a manager")

// processAcknowledgementEvent processes the given ack event.
// Promotes the ack author to incident.RoleManager if it's not already the case and generates a history entry.
// Returns error on database failure.
func (i *Incident) processAcknowledgementEvent(ctx context.Context, tx *sqlx.Tx, ev *event.Event) error {
	contact := i.RuntimeConfig.GetContact(ev.Username)
	if contact == nil {
		i.Logger.Warnw("Ignoring acknowledgement event from an unknown author", zap.String("author", ev.Username))

		return fmt.Errorf("unknown acknowledgment author %q", ev.Username)
	}

	recipientKey := recipient.ToKey(contact)
	state := i.Recipients[recipientKey]
	oldRole := RoleNone
	newRole := RoleManager
	if state != nil {
		oldRole = state.Role

		if oldRole == RoleManager {
			// The user is already a manager
			i.Logger.Debugw("Ignoring acknowledgement-set event, author is already a manager", zap.String("author", ev.Username))
			return errSuperfluousAckEvent
		}
	} else {
		i.Recipients[recipientKey] = &RecipientState{Role: newRole}
	}

	i.Logger.Infof("Contact %q role changed from %s to %s", contact.String(), oldRole.String(), newRole.String())

	hr := &HistoryRow{
		IncidentID:       i.ID,
		Key:              recipientKey,
		EventID:          types.MakeInt(ev.ID, types.TransformZeroIntToNull),
		Type:             RecipientRoleChanged,
		Time:             types.UnixMilli(time.Now()),
		NewRecipientRole: newRole,
		OldRecipientRole: oldRole,
		Message:          types.MakeString(ev.Message, types.TransformEmptyStringToNull),
	}

	if err := hr.Sync(ctx, i.DB, tx); err != nil {
		i.Logger.Errorw("Failed to add recipient role changed history", zap.String("recipient", contact.String()), zap.Error(err))
		return err
	}

	cr := &ContactRow{IncidentID: hr.IncidentID, Key: recipientKey, Role: newRole}

	stmt, _ := i.DB.BuildUpsertStmt(cr)
	_, err := tx.NamedExecContext(ctx, stmt, cr)
	if err != nil {
		i.Logger.Errorw("Failed to upsert incident contact", zap.String("contact", contact.String()), zap.Error(err))
		return err
	}

	return nil
}

// getRecipientsChannel returns all the configured channels of the current incident and escalation recipients.
func (i *Incident) getRecipientsChannel(t time.Time) rule.ContactChannels {
	contactChs := make(rule.ContactChannels)
	// Load all escalations recipients channels
	for escalationID := range i.EscalationState {
		escalation := i.RuntimeConfig.GetRuleEntry(escalationID)
		if escalation == nil {
			i.Logger.Debugw("Incident refers unknown escalation, might got deleted", zap.Int64("escalation_id", escalationID))
			continue
		}

		contactChs.LoadFromEntryRecipients(escalation, t, i.isRecipientNotifiable)
	}

	// Check whether all the incident recipients do have an appropriate contact channel configured.
	// When a recipient has subscribed/managed this incident via the UI or using an ACK, fallback
	// to the default contact channel.
	for recipientKey, state := range i.Recipients {
		r := i.RuntimeConfig.GetRecipient(recipientKey)
		if r == nil {
			i.Logger.Debugw("Incident refers unknown recipient key, might got deleted", zap.Inline(recipientKey))
			continue
		}

		if i.IsNotifiable(state.Role) {
			contacts := r.GetContactsAt(t)
			if len(contacts) > 0 {
				i.Logger.Debugw("Expanded recipient to contacts",
					zap.Object("recipient", r),
					zap.Objects("contacts", contacts))

				for _, contact := range contacts {
					if contactChs[contact] == nil {
						contactChs[contact] = make(map[int64]bool)
						contactChs[contact][contact.DefaultChannelID] = true
					}
				}
			} else {
				i.Logger.Warnw("Recipient expanded to no contacts", zap.Object("recipient", r))
			}
		}
	}

	return contactChs
}

// restoreRecipients reloads the current incident recipients from the database.
// Returns error on database failure.
func (i *Incident) restoreRecipients(ctx context.Context) error {
	contact := &ContactRow{}
	var contacts []*ContactRow
	err := i.DB.SelectContext(ctx, &contacts, i.DB.Rebind(i.DB.BuildSelectStmt(contact, contact)+` WHERE "incident_id" = ?`), i.ID)
	if err != nil {
		i.Logger.Errorw("Failed to restore incident recipients from the database", zap.Error(err))
		return err
	}

	recipients := make(map[recipient.Key]*RecipientState)
	for _, contact := range contacts {
		recipients[contact.Key] = &RecipientState{Role: contact.Role}
	}

	i.Recipients = recipients

	return nil
}

// isRecipientNotifiable checks whether the given recipient should be notified about the current incident.
// If the specified recipient has not yet been notified of this incident, it always returns false.
// Otherwise, the recipient role is forwarded to IsNotifiable and may or may not return true.
func (i *Incident) isRecipientNotifiable(key recipient.Key) bool {
	state := i.Recipients[key]
	if state == nil {
		return false
	}

	return i.IsNotifiable(state.Role)
}

type EscalationState struct {
	IncidentID       int64           `db:"incident_id"`
	RuleEscalationID int64           `db:"rule_entry_id"`
	TriggeredAt      types.UnixMilli `db:"triggered_at"`
}

// TableName implements the contracts.TableNamer interface.
func (e *EscalationState) TableName() string {
	return "incident_rule_entry_state"
}

type RecipientState struct {
	Role ContactRole
}
