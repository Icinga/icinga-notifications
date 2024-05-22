package incident

import (
	"context"
	"errors"
	"fmt"
	"github.com/icinga/icinga-go-library/database"
	"github.com/icinga/icinga-go-library/types"
	"github.com/icinga/icinga-notifications/internal/config"
	"github.com/icinga/icinga-notifications/internal/event"
	"github.com/icinga/icinga-notifications/internal/notification"
	"github.com/icinga/icinga-notifications/internal/object"
	"github.com/icinga/icinga-notifications/internal/recipient"
	"github.com/icinga/icinga-notifications/internal/rule"
	"github.com/icinga/icinga-notifications/internal/utils"
	"github.com/jmoiron/sqlx"
	"go.uber.org/zap"
	"sync"
	"time"
)

type escalationID = int64

type Incident struct {
	ID          int64           `db:"id"`
	ObjectID    types.Binary    `db:"object_id"`
	StartedAt   types.UnixMilli `db:"started_at"`
	RecoveredAt types.UnixMilli `db:"recovered_at"`
	Severity    event.Severity  `db:"severity"`

	Object *object.Object `db:"-"`

	EscalationState map[escalationID]*EscalationState `db:"-"`
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

	// notification.Notifier is a helper type used to send notifications.
	// It is embedded to allow direct access to its members, such as logger, DB etc.
	notification.Notifier

	// config.Evaluable encapsulates all evaluable configuration types, such as rule.Rule, rule.Entry etc.
	// It is embedded to enable direct access to its members.
	*config.Evaluable

	sync.Mutex
}

func NewIncident(
	db *database.DB, obj *object.Object, runtimeConfig *config.RuntimeConfig, logger *zap.SugaredLogger,
) *Incident {
	i := &Incident{
		Object:          obj,
		Evaluable:       config.NewEvaluable(),
		Notifier:        notification.Notifier{DB: db, RuntimeConfig: runtimeConfig, Logger: logger},
		EscalationState: map[escalationID]*EscalationState{},
		Recipients:      map[recipient.Key]*RecipientState{},
	}

	if obj != nil {
		i.ObjectID = obj.ID
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
	if i.isMuted && ev.Type == event.TypeMute {
		i.Logger.Debugw("Ignoring superfluous mute event", zap.String("event", ev.String()))
		return event.ErrSuperfluousMuteUnmuteEvent
	} else if !i.isMuted && ev.Type == event.TypeUnmute {
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

	if err := i.handleMuteUnmute(ctx, tx, ev); err != nil {
		i.Logger.Errorw("Cannot insert incident muted history", zap.String("event", ev.String()), zap.Error(err))
		return err
	}

	switch ev.Type {
	case event.TypeState:
		if !isNew {
			if err := i.processSeverityChangedEvent(ctx, tx, ev); err != nil {
				return err
			}
		}

		// Check if any (additional) rules match this object. Incident filter rules are stateful, which means that
		// once they have been matched, they remain effective for the ongoing incident and never need to be rechecked.
		err := i.EvaluateRules(i.RuntimeConfig, i.Object, config.EvalOptions[*rule.Rule, any]{
			OnPreEvaluate: func(r *rule.Rule) bool { return r.Type == rule.TypeEscalation },
			OnFilterMatch: func(r *rule.Rule) error { return i.onFilterRuleMatch(ctx, r, tx, ev) },
			OnError: func(r *rule.Rule, err error) bool {
				i.Logger.Warnw("Failed to evaluate object filter", zap.Object("rule", r), zap.Error(err))

				// We don't want to stop evaluating the remaining rules just because one of them couldn't be evaluated.
				return true
			},
		})
		if err != nil {
			return err
		}

		// Reset the evaluated escalations when leaving this function while holding the incident lock,
		// otherwise the pointers could be invalidated in the meantime and lead to unexpected behaviour.
		defer func() { i.RuleEntries = make(map[int64]*rule.Entry) }()

		// Re-evaluate escalations based on the newly evaluated rules.
		i.evaluateEscalations(ev.Time)

		if err := i.triggerEscalations(ctx, tx, ev); err != nil {
			return err
		}
	case event.TypeAcknowledgementSet:
		if err := i.processAcknowledgementEvent(ctx, tx, ev); err != nil {
			if errors.Is(err, errSuperfluousAckEvent) {
				// That ack error type indicates that the acknowledgement author was already a manager, thus
				// we can safely ignore that event and return without even committing the DB transaction.
				return nil
			}

			return err
		}
	}

	notifications, err := i.generateNotifications(ctx, tx, ev, i.getRecipientsChannel(ev.Time))
	if err != nil {
		return err
	}

	if err = tx.Commit(); err != nil {
		i.Logger.Errorw("Cannot commit db transaction", zap.Error(err))
		return err
	}

	// We've just committed the DB transaction and can safely update the incident muted flag.
	i.isMuted = i.Object.IsMuted()

	return i.NotifyContacts(ctx, i.makeNotificationRequest(ev), notifications)
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

	// Reset the evaluated escalations when leaving this function while holding the incident lock,
	// otherwise the pointers could be invalidated in the meantime and lead to unexpected behaviour.
	defer func() { i.RuleEntries = make(map[int64]*rule.Entry) }()

	i.evaluateEscalations(ev.Time)
	if len(i.RuleEntries) == 0 {
		i.Logger.Debug("Reevaluated escalations, no new escalations triggered")
		return
	}

	notifications := make(notification.PendingNotifications)
	ctx := context.Background()
	err := utils.RunInTx(ctx, i.DB, func(tx *sqlx.Tx) error {
		err := ev.Sync(ctx, tx, i.DB, i.Object.ID)
		if err != nil {
			return err
		}

		if err = i.triggerEscalations(ctx, tx, ev); err != nil {
			return err
		}

		channels := make(rule.ContactChannels)
		for _, escalation := range i.RuleEntries {
			channels.LoadFromEntryRecipients(escalation, ev.Time, i.isRecipientNotifiable)
		}

		notifications, err = i.generateNotifications(ctx, tx, ev, channels)
		return err
	})
	if err != nil {
		i.Logger.Errorw("Reevaluating time-based escalations failed", zap.Error(err))
	} else {
		if err = i.NotifyContacts(ctx, i.makeNotificationRequest(ev), notifications); err != nil {
			i.Logger.Errorw("Failed to notify reevaluated escalation recipients", zap.Error(err))
			return
		}

		i.Logger.Info("Successfully reevaluated time-based escalations")
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
		EventID:     utils.ToDBInt(ev.ID),
		Time:        types.UnixMilli(time.Now()),
		Type:        IncidentSeverityChanged,
		NewSeverity: newSeverity,
		OldSeverity: oldSeverity,
	}

	if err := hr.Sync(ctx, i.DB, tx); err != nil {
		i.Logger.Errorw("Failed to insert incident severity changed history", zap.Error(err))
		return err
	}

	if newSeverity == event.SeverityOK {
		i.RecoveredAt = types.UnixMilli(time.Now())
		i.Logger.Info("All sources recovered, closing incident")

		RemoveCurrent(i.Object)

		hr = &HistoryRow{
			IncidentID: i.ID,
			EventID:    utils.ToDBInt(ev.ID),
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
		EventID:     utils.ToDBInt(ev.ID),
		NewSeverity: i.Severity,
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

	hr := &HistoryRow{IncidentID: i.ID, EventID: utils.ToDBInt(ev.ID), Time: types.UnixMilli(time.Now())}
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
		hr.Message = utils.ToDBString(ev.MuteReason)
		logger.Infow("Unmuting incident", zap.String("reason", ev.MuteReason))
	}

	return hr.Sync(ctx, i.DB, tx)
}

// onFilterRuleMatch records a database entry in the `incident_rule` table that refers to the specified rule.Rule.
// In addition, it generates a RuleMatched Incident History and synchronises it with the database.
//
// This function should only be used as an OnFilterMatch handler that is passed to the Evaluable#EvaluateRules
// function, which only fires when the event rule filter matches on the current Incident Object.
//
// Returns an error if it fails to persist the database entries.
func (i *Incident) onFilterRuleMatch(ctx context.Context, r *rule.Rule, tx *sqlx.Tx, ev *event.Event) error {
	i.Logger.Infow("Rule matches", zap.Object("rule", r))

	if err := i.AddRuleMatched(ctx, tx, r); err != nil {
		i.Logger.Errorw("Failed to upsert incident rule", zap.Object("rule", r), zap.Error(err))
		return err
	}

	hr := &HistoryRow{
		IncidentID: i.ID,
		Time:       types.UnixMilli(time.Now()),
		EventID:    utils.ToDBInt(ev.ID),
		RuleID:     utils.ToDBInt(r.ID),
		Type:       RuleMatched,
	}
	if err := hr.Sync(ctx, i.DB, tx); err != nil {
		i.Logger.Errorw("Failed to insert rule matched incident history", zap.Object("rule", r), zap.Error(err))
		return err
	}

	return nil
}

// evaluateEscalations evaluates this incidents rule escalations to be triggered if they aren't already.
func (i *Incident) evaluateEscalations(eventTime time.Time) {
	if i.EscalationState == nil {
		i.EscalationState = make(map[int64]*EscalationState)
	}

	// Escalations are reevaluated now, reset any existing timer, if there might be future time-based escalations,
	// this function will start a new timer.
	if i.timer != nil {
		i.Logger.Info("Stopping reevaluate timer due to escalation evaluation")
		i.timer.Stop()
		i.timer = nil
	}

	filterContext := &rule.EscalationFilter{IncidentAge: eventTime.Sub(i.StartedAt.Time()), IncidentSeverity: i.Severity}

	// EvaluateRuleEntries only returns an error if one of the provided callback hooks returns
	// an error or the OnError handler returns false, and since none of our callbacks return an
	// error nor false, we can safely discard the return value here.
	_ = i.EvaluateRuleEntries(i.RuntimeConfig, filterContext, config.EvalOptions[*rule.Entry, any]{
		// Prevent reevaluation of an already triggered escalation via the pre run hook.
		OnPreEvaluate: func(escalation *rule.Entry) bool { return i.EscalationState[escalation.ID] == nil },
		OnError: func(escalation *rule.Entry, err error) bool {
			r := i.RuntimeConfig.Rules[escalation.RuleID]
			i.Logger.Warnw("Failed to evaluate escalation condition", zap.Object("rule", r),
				zap.Object("escalation", escalation), zap.Error(err))
			return true
		},
		OnAllConfigEvaluated: func(result any) {
			retryAfter := result.(time.Duration)
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
						Time:    nextEvalAt,
						Type:    event.TypeIncidentAge,
						Message: fmt.Sprintf("Incident reached age %v", nextEvalAt.Sub(i.StartedAt.Time())),
					})
				})
			}
		},
	})
}

// triggerEscalations triggers the given escalations and generates incident history items for each of them.
// Returns an error on database failure.
func (i *Incident) triggerEscalations(ctx context.Context, tx *sqlx.Tx, ev *event.Event) error {
	for _, escalation := range i.RuleEntries {
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
			EventID:     utils.ToDBInt(ev.ID),
			RuleEntryID: utils.ToDBInt(state.RuleEscalationID),
			RuleID:      utils.ToDBInt(r.ID),
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
		EventID:          utils.ToDBInt(ev.ID),
		Type:             RecipientRoleChanged,
		Time:             types.UnixMilli(time.Now()),
		NewRecipientRole: newRole,
		OldRecipientRole: oldRole,
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
