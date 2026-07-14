package incident

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/icinga/icinga-go-library/database"
	baseEv "github.com/icinga/icinga-go-library/notifications/event"
	"github.com/icinga/icinga-go-library/types"
	"github.com/icinga/icinga-notifications/internal/channel"
	"github.com/icinga/icinga-notifications/internal/config"
	"github.com/icinga/icinga-notifications/internal/contracts"
	"github.com/icinga/icinga-notifications/internal/daemon"
	"github.com/icinga/icinga-notifications/internal/event"
	"github.com/icinga/icinga-notifications/internal/object"
	"github.com/icinga/icinga-notifications/internal/recipient"
	"github.com/icinga/icinga-notifications/internal/rule"
	"github.com/icinga/icinga-notifications/internal/utils"
	"github.com/jmoiron/sqlx"
	"go.uber.org/zap"
)

type ruleID = int64
type escalationID = int64

type Incident struct {
	Id          int64           `db:"id"`
	ObjectID    types.Binary    `db:"object_id"`
	StartedAt   types.UnixMilli `db:"started_at"`
	RecoveredAt types.UnixMilli `db:"recovered_at"`
	Severity    baseEv.Severity `db:"severity"`
	// MuteReason indicates whether this incident is currently muted; its non-null value contains the mute reason.
	MuteReason types.String `db:"mute_reason"`
	Message    types.String `db:"message"`

	// NextEscalationCheckAt stores when this Incident's time-based escalations should be reevaluated next. It is used
	// in the ReevaluateEscalations function to reevaluate incident escalations.
	//
	// For example, if there are escalations configured for incident_age>1h and incident_age>2h.
	//   - If the incident is less than an hour old, this field contains the incident start time plus one hour.
	//   - If the incident is between 1h and 2h old, this field contains the incident start time plus two hours.
	//   - If the incident is already older than 2h, no future escalations can be reached solely based on the incident
	//     aging, so the field is left nil.
	NextEscalationCheckAt types.UnixMilli `db:"next_escalation_check_at"`

	EscalationState map[escalationID]*EscalationState `db:"-"`
	Rules           map[ruleID]struct{}               `db:"-"`
	Recipients      map[recipient.Key]*RecipientState `db:"-"`

	db            *database.DB
	logger        *zap.SugaredLogger
	runtimeConfig *config.RuntimeConfig
}

func NewIncident(
	db *database.DB, obj *object.Object, runtimeConfig *config.RuntimeConfig, logger *zap.SugaredLogger,
) *Incident {
	i := &Incident{}
	i.initializeFields(db, runtimeConfig, logger)

	if obj != nil {
		i.ObjectID = obj.ID
	}

	return i
}

// initializeFields populates the runtime fields for an Incident.
func (i *Incident) initializeFields(db *database.DB, runtimeConfig *config.RuntimeConfig, logger *zap.SugaredLogger) {
	i.db = db
	i.logger = logger
	i.runtimeConfig = runtimeConfig
	i.EscalationState = map[escalationID]*EscalationState{}
	i.Rules = map[ruleID]struct{}{}
	i.Recipients = map[recipient.Key]*RecipientState{}
}

// Object fetches the object.Object this incident belongs to from the database.
func (i *Incident) Object(ctx context.Context) (*object.Object, error) {
	obj, err := object.Get(ctx, i.db, i.ObjectID)
	if err != nil {
		return nil, fmt.Errorf("cannot get incident object: %w", err)
	}
	return obj, nil
}

func (i *Incident) IncidentSeverity() baseEv.Severity {
	return i.Severity
}

func (i *Incident) String() string {
	return fmt.Sprintf("#%d", i.Id)
}

func (i *Incident) ID() int64 {
	return i.Id
}

// IsMuted returns whether this incident is currently muted.
func (i *Incident) IsMuted() bool {
	return i.MuteReason.Valid
}

// IsNew returns whether this incident has not yet been persisted to the database.
func (i *Incident) IsNew() bool {
	return i.StartedAt.Time().IsZero()
}

func (i *Incident) HasManager() bool {
	for recipientKey, state := range i.Recipients {
		if i.runtimeConfig.GetRecipient(recipientKey) == nil {
			i.logger.Debugw("Incident refers unknown recipient key, might got deleted", zap.Inline(recipientKey))
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
	tx, err := i.db.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		i.logger.Errorw("Cannot start a db transaction", zap.Error(err))
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if err := i.RestoreState(ctx, tx); err != nil && !errors.Is(err, sql.ErrNoRows) {
		i.logger.Errorw("Failed to restore incident row from the database", zap.Error(err))
		return fmt.Errorf("cannot restore incident state: %w", err)
	}

	obj := object.New(ev)
	if err := obj.SyncFromEvent(ctx, i.db, tx, ev); err != nil {
		i.logger.Errorw("Cannot sync event object", zap.Error(err))
		return fmt.Errorf("cannot sync event object: %w", err)
	}

	triggerNotifications := true
	isNew := i.IsNew()
	if isNew {
		if !ev.OpenOrEscalate() {
			// There is no active incident and the event cannot open one, so nothing to process. Returning rolls the
			// transaction back, so the object sync from above is also not persisted.
			return nil
		}

		if ev.Severity == baseEv.SeverityNone {
			return ErrOpenIncidentWithoutSeverity
		}

		if err := i.processIncidentOpenedEvent(ctx, tx, ev); err != nil {
			return err
		}

		i.logger = i.logger.With(zap.String("incident", i.String()))
	} else {
		if sevChanged, err := i.processSeverityChangedEvent(ctx, tx, ev); err != nil {
			return err
		} else {
			// In case the severity didn't change, we need to check whether we can trigger notifications nonetheless.
			triggerNotifications = sevChanged || ev.NotifyRecipients() || (ev.Muted.Valid && ev.IsMuted() != i.IsMuted())
		}
	}

	var notifications []*NotificationEntry
	err = func() error {
		i.runtimeConfig.RLock()
		defer i.runtimeConfig.RUnlock()

		if ev.OpenOrEscalate() {
			if err := i.applyMatchingRules(ctx, tx, ev); err != nil {
				return err
			}

			// Re-evaluate escalations based on the newly evaluated rules.
			escalations, err := i.evaluateEscalations(ev.Time)
			if err != nil {
				return err
			}

			if err := i.triggerEscalations(ctx, tx, escalations); err != nil {
				return err
			}

			// If we have managed to trigger any new escalations, we must trigger notifications as well,
			// even if the event itself doesn't request it.
			triggerNotifications = triggerNotifications || len(escalations) > 0

			if !isNew {
				// Even if the severity didn't change, we want to update the message nonetheless.
				i.Message = types.MakeString(ev.Message, types.TransformEmptyStringToNull)
			}
		}

		// The unmute history entry, on the other hand, must be inserted first, so that the notifications generated
		// below appear logically after the unmute event. This way, when viewing the incident history in the UI, the
		// unmute event will appear before the notifications that were sent after unmuting.
		if err := i.handleUnmute(ctx, tx, ev); err != nil {
			i.logger.Errorw("Cannot insert incident muted history", zap.Error(err))
			return err
		}

		if triggerNotifications {
			notifications, err = i.generateNotifications(ctx, tx, ev, i.getRecipientsChannel(ev.Time))
			if err != nil {
				return err
			}
		}
		return nil
	}()
	if err != nil {
		return err
	}

	// So that the incident muted history appears logically after the just generated notifications, we must insert
	// the muted history last. This way, the history entries will make sense when viewed in chronological order.
	if err := i.handleMute(ctx, tx, ev); err != nil {
		i.logger.Errorw("Cannot insert incident muted history", zap.Error(err))
		return err
	}

	if ev.CloseIncident() {
		if err := i.Close(ctx, tx, false); err != nil {
			return err
		}
	}

	if err := i.Sync(ctx, tx); err != nil {
		i.logger.Errorw("Failed to update incident", zap.Error(err))
		return err
	}

	if err = tx.Commit(); err != nil {
		i.logger.Errorw("Cannot commit db transaction", zap.Error(err))
		return err
	}

	return i.notifyContacts(ctx, obj, ev, notifications)
}

// RetriggerEscalations tries to re-evaluate the escalations and notify contacts.
func (i *Incident) RetriggerEscalations(ctx context.Context, ev *event.Event) error {
	tx, err := i.db.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("cannot start transaction for escalation reevaluation: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := i.RestoreState(ctx, tx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Incident is recovered in the meantime.
			return nil
		}
		i.logger.Errorw("Failed to restore incident row from the database", zap.Error(err))
		return fmt.Errorf("cannot restore incident state for escalation reevaluation: %w", err)
	}

	if !time.Now().After(ev.Time) {
		i.logger.DPanicw("Event from the future", zap.Time("event_time", ev.Time), zap.Object("event", ev))
		return nil
	}

	if ev.Message == "" {
		ev.Message = fmt.Sprintf("Incident reached age %v", ev.Time.Sub(i.StartedAt.Time()))
	}

	var notifications []*NotificationEntry
	err = func() error {
		i.runtimeConfig.RLock()
		defer i.runtimeConfig.RUnlock()

		escalations, err := i.evaluateEscalations(ev.Time)
		if err != nil {
			return fmt.Errorf("cannot reevaluate escalations: %w", err)
		}

		if len(escalations) > 0 {
			if err := i.triggerEscalations(ctx, tx, escalations); err != nil {
				return fmt.Errorf("cannot trigger reevaluated escalations: %w", err)
			}

			channels := make(rule.ContactChannels)
			for _, escalation := range escalations {
				channels.LoadFromEscalationRecipients(escalation, ev.Time, i.isRecipientNotifiable)
			}

			notifications, err = i.generateNotifications(ctx, tx, ev, channels)
			if err != nil {
				return fmt.Errorf("cannot generate notifications for reevaluated escalations: %w", err)
			}
		} else {
			i.logger.Debug("Reevaluated escalations, no new escalations triggered")
		}
		return nil
	}()
	if err != nil {
		return err
	}

	if err := i.Sync(ctx, tx); err != nil {
		return fmt.Errorf("cannot persist next escalation check time: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("cannot commit transaction for escalation reevaluation: %w", err)
	}

	if len(notifications) == 0 {
		return nil
	}

	o, err := i.Object(ctx)
	if err != nil {
		return err
	}

	if err := i.notifyContacts(ctx, o, ev, notifications); err != nil {
		return fmt.Errorf("failed to notify reevaluated escalation recipients: %w", err)
	}

	return nil
}

// Close closes the current incident if not already recovered.
//
// If the incident is already recovered, this is a no-op. Returns an error if fails
// to persist the recovery time or insert the generated history to the database.
func (i *Incident) Close(ctx context.Context, tx *sqlx.Tx, persist bool) error {
	if i.RecoveredAt.Time().IsZero() {
		i.RecoveredAt = types.UnixMilli(time.Now())
		i.NextEscalationCheckAt = types.UnixMilli{}
		i.logger.Info("Received request to close the incident, marking it as recovered")

		hr := &HistoryRow{
			IncidentID: i.Id,
			Time:       i.RecoveredAt,
			Type:       Closed,
		}

		if err := hr.Sync(ctx, i.db, tx); err != nil {
			i.logger.Errorw("Cannot insert incident closed history to the database", zap.Error(err))
			return err
		}

		if persist {
			return i.Sync(ctx, tx)
		}
	}
	return nil
}

// ErrSeverityChangeWithoutIncidentFlag is returned when an event tries to change the severity of an incident
// but does not set the 'incident' flag.
var ErrSeverityChangeWithoutIncidentFlag = errors.New("cannot change severity of an incident with an event that doesn't set the 'incident' flag")

// processSeverityChangedEvent processes the given event as a severity changed event, if the severity has actually changed.
//
// If the severity has not changed, this is a no-op, otherwise returns true.
//
// Returns an error if fails to persist the generated history or [ErrSeverityChangeWithoutIncidentFlag]
// if the event does not set the 'incident' flag.
func (i *Incident) processSeverityChangedEvent(ctx context.Context, tx *sqlx.Tx, ev *event.Event) (bool, error) {
	sevChanged := ev.Severity != baseEv.SeverityNone && i.Severity != ev.Severity
	if sevChanged {
		if !ev.OpenOrEscalate() {
			i.logger.Errorw("Cannot change incident severity with an event that doesn't set the 'incident' flag")
			return false, ErrSeverityChangeWithoutIncidentFlag
		}
		i.logger.Infof("Incident severity changed from %s to %s", i.Severity.String(), ev.Severity.String())

		hr := &HistoryRow{
			IncidentID:  i.Id,
			Time:        types.UnixMilli(time.Now()),
			Type:        IncidentSeverityChanged,
			NewSeverity: ev.Severity,
			OldSeverity: i.Severity,
			Message:     types.MakeString(ev.Message, types.TransformEmptyStringToNull),
		}

		if err := hr.Sync(ctx, i.db, tx); err != nil {
			i.logger.Errorw("Failed to insert incident severity changed history", zap.Error(err))
			return false, err
		}

		i.Severity = ev.Severity
	}

	return sevChanged, nil
}

func (i *Incident) processIncidentOpenedEvent(ctx context.Context, tx *sqlx.Tx, ev *event.Event) error {
	i.StartedAt = types.UnixMilli(ev.Time)
	i.Severity = ev.Severity
	i.Message = types.MakeString(ev.Message, types.TransformEmptyStringToNull)
	if err := i.Sync(ctx, tx); err != nil {
		i.logger.Errorw("Cannot insert incident to the database", zap.Error(err))
		return err
	}

	i.logger.Infow(fmt.Sprintf("Source %d opened incident at severity %q", ev.SourceId, i.Severity.String()), zap.String("message", ev.Message))

	hr := &HistoryRow{
		IncidentID:  i.Id,
		Type:        Opened,
		Time:        types.UnixMilli(ev.Time),
		NewSeverity: i.Severity,
		Message:     types.MakeString(ev.Message, types.TransformEmptyStringToNull),
	}

	if err := hr.Sync(ctx, i.db, tx); err != nil {
		i.logger.Errorw("Cannot insert incident opened history event", zap.Error(err))
		return err
	}

	return nil
}

// handleUnmute clears the incident mute reason and generates the corresponding Unmuted history if the incident
// is going to be unmuted now.
//
// If the incident is already unmuted, or the event does not unmute, this is a no-op.
// Returns an error if fails to persist the cleared mute reason or insert the generated history to the database.
func (i *Incident) handleUnmute(ctx context.Context, tx *sqlx.Tx, ev *event.Event) error {
	if !i.IsMuted() || !ev.Muted.Valid || ev.IsMuted() {
		return nil
	}

	i.logger.Infow("Unmuting incident", zap.String("reason", i.MuteReason.String))

	i.MuteReason = types.String{}

	hr := &HistoryRow{
		IncidentID: i.Id,
		Time:       types.UnixMilli(time.Now()),
		Type:       Unmuted,
		Message:    types.MakeString(ev.MutedReason, types.TransformEmptyStringToNull),
	}
	return hr.Sync(ctx, i.db, tx)
}

// handleMute sets the incident mute reason and generates the corresponding Muted history if the incident
// is not yet muted but is going to be muted now.
//
// If the incident is already muted, or the event does not mute, this is a no-op.
// Returns an error if fails to persist the mute reason or insert the generated history to the database.
func (i *Incident) handleMute(ctx context.Context, tx *sqlx.Tx, ev *event.Event) error {
	if i.IsMuted() || !ev.IsMuted() {
		return nil
	}

	i.MuteReason = types.MakeString(ev.MutedReason, types.TransformEmptyStringToNull)
	i.logger.Infow("Muting incident", zap.String("reason", i.MuteReason.String))

	hr := &HistoryRow{
		IncidentID: i.Id,
		Time:       types.UnixMilli(time.Now()),
		Type:       Muted,
		Message:    i.MuteReason,
	}
	return hr.Sync(ctx, i.db, tx)
}

// applyMatchingRules walks through the rule IDs obtained from source and generates a RuleMatched history entry.
func (i *Incident) applyMatchingRules(ctx context.Context, tx *sqlx.Tx, ev *event.Event) error {
	if i.Rules == nil {
		i.Rules = make(map[int64]struct{})
	}

	src, ok := i.runtimeConfig.Sources[ev.SourceId]
	if !ok {
		i.logger.Warnw("Received event from unknown source, might got deleted", zap.Int64("source_id", ev.SourceId))
		return nil
	}

	for _, id := range src.RuleIDs() {
		if _, ok := i.Rules[id]; !ok {
			r, ok := i.runtimeConfig.Rules[id]
			if !ok {
				i.logger.Errorw("BUG: source references unknown event rule", zap.Object("source", src))
				continue
			}

			matched, err := r.Eval(ev)
			if err != nil {
				i.logger.Errorw("Failed to evaluate object filter", zap.Object("rule", r), zap.Error(err))
			}

			if err != nil || !matched {
				continue
			}

			i.Rules[r.ID] = struct{}{}
			i.logger.Infow("Rule matches", zap.Object("rule", r))

			if err := i.AddRuleMatched(ctx, tx, r); err != nil {
				i.logger.Errorw("Failed to upsert incident rule", zap.Object("rule", r), zap.Error(err))
				return err
			}

			hr := &HistoryRow{
				IncidentID: i.Id,
				Time:       types.UnixMilli(time.Now()),
				RuleID:     types.MakeInt(r.ID, types.TransformZeroIntToNull),
				Type:       RuleMatched,
			}
			if err := hr.Sync(ctx, i.db, tx); err != nil {
				i.logger.Errorw("Failed to insert rule matched incident history", zap.Object("rule", r), zap.Error(err))
				return err
			}
		}
	}

	return nil
}

// evaluateEscalations evaluates this incidents rule escalations to be triggered if they aren't already.
// Returns the newly evaluated escalations to be triggered or an error on database failure.
func (i *Incident) evaluateEscalations(eventTime time.Time) ([]*rule.Escalation, error) {
	if i.EscalationState == nil {
		i.EscalationState = make(map[int64]*EscalationState)
	}

	filterContext := &rule.EscalationFilter{IncidentAge: eventTime.Sub(i.StartedAt.Time()), IncidentSeverity: i.Severity}

	var escalations []*rule.Escalation
	retryAfter := rule.RetryNever

	for rID := range i.Rules {
		r := i.runtimeConfig.Rules[rID]
		if r == nil {
			i.logger.Debugw("Incident refers unknown rule, might got deleted", zap.Int64("rule_id", rID))
			continue
		}

		// Check if new escalation stages are reached
		for _, escalation := range r.Escalations {
			if _, ok := i.EscalationState[escalation.ID]; !ok {
				matched, err := escalation.Eval(filterContext)
				if err != nil {
					i.logger.Warnw(
						"Failed to evaluate escalation condition", zap.Object("rule", r),
						zap.Object("escalation", escalation), zap.Error(err),
					)
				} else if !matched {
					incidentAgeFilter := filterContext.ReevaluateAfter(escalation.Condition)
					retryAfter = min(retryAfter, incidentAgeFilter)
				} else {
					escalations = append(escalations, escalation)
				}
			}
		}
	}

	if retryAfter != rule.RetryNever {
		// The retryAfter duration is relative to the incident duration represented by the escalation filter,
		// i.e. if an incident is 15m old and an escalation rule evaluates incident_age>=1h the retryAfter would
		// contain 45m (1h - incident age (15m)). Therefore, we have to use the event time instead of the incident
		// start time here.
		nextEvalAt := eventTime.Add(retryAfter)

		i.logger.Infow("Scheduling escalation reevaluation", zap.Duration("after", retryAfter), zap.Time("at", nextEvalAt))
		i.NextEscalationCheckAt = types.UnixMilli(nextEvalAt)
	} else {
		i.NextEscalationCheckAt = types.UnixMilli{}
	}

	return escalations, nil
}

// triggerEscalations triggers the given escalations and generates incident history items for each of them.
// Returns an error on database failure.
func (i *Incident) triggerEscalations(ctx context.Context, tx *sqlx.Tx, escalations []*rule.Escalation) error {
	for _, escalation := range escalations {
		r := i.runtimeConfig.Rules[escalation.RuleID]
		if r == nil {
			i.logger.Debugw("Incident refers unknown rule, might got deleted", zap.Int64("rule_id", escalation.RuleID))
			continue
		}

		i.logger.Infow("Rule reached escalation", zap.Object("rule", r), zap.Object("escalation", escalation))

		state := &EscalationState{RuleEscalationID: escalation.ID, TriggeredAt: types.UnixMilli(time.Now())}
		i.EscalationState[escalation.ID] = state

		if err := i.AddEscalationTriggered(ctx, tx, state); err != nil {
			i.logger.Errorw(
				"Failed to upsert escalation state", zap.Object("rule", r),
				zap.Object("escalation", escalation), zap.Error(err),
			)
			return err
		}

		hr := &HistoryRow{
			IncidentID:       i.Id,
			Time:             state.TriggeredAt,
			RuleEscalationID: types.MakeInt(state.RuleEscalationID, types.TransformZeroIntToNull),
			RuleID:           types.MakeInt(r.ID, types.TransformZeroIntToNull),
			Type:             EscalationTriggered,
		}

		if err := hr.Sync(ctx, i.db, tx); err != nil {
			i.logger.Errorw(
				"Failed to insert escalation triggered incident history", zap.Object("rule", r),
				zap.Object("escalation", escalation), zap.Error(err),
			)
			return err
		}

		if err := i.AddRecipient(ctx, tx, escalation); err != nil {
			return err
		}
	}

	return nil
}

// notifyContacts executes all the given pending notifications of the current incident.
// Returns error on database failure or if the provided context is cancelled.
func (i *Incident) notifyContacts(
	ctx context.Context,
	obj *object.Object,
	ev *event.Event,
	notifications []*NotificationEntry,
) error {
	for _, notification := range notifications {
		i.runtimeConfig.RLock()
		contact := i.runtimeConfig.Contacts[notification.ContactID]
		if contact == nil {
			i.runtimeConfig.RUnlock()
			i.logger.Debugw("Incident refers unknown contact, might got deleted", zap.Int64("contact_id", notification.ContactID))
			continue
		}
		contactName := contact.String()

		ch := i.runtimeConfig.Channels[notification.ChannelID]
		if ch == nil {
			i.runtimeConfig.RUnlock()
			i.logger.Errorw("Could not find config for channel", zap.Int64("channel_id", notification.ChannelID))
			continue
		}
		i.runtimeConfig.RUnlock()

		err := i.notifyContact(obj, contact, ev, ch)
		if err != nil {
			notification.State = NotificationStateFailed
		} else {
			notification.State = NotificationStateSent
		}

		notification.SentAt = types.UnixMilli(time.Now())
		stmt, _ := i.db.BuildUpdateStmt(notification)
		if _, err := i.db.NamedExecContext(ctx, stmt, notification); err != nil {
			i.logger.Errorw(
				"Failed to update contact notified incident history", zap.String("contact", contactName),
				zap.Error(err),
			)
		}

		if err := ctx.Err(); err != nil {
			return err
		}
	}

	return nil
}

// notifyContact notifies the given recipient via a channel.
func (i *Incident) notifyContact(
	obj *object.Object,
	contact *recipient.Contact,
	ev *event.Event,
	ch *channel.Channel,
) error {
	i.logger.Infof("Notifying contact %q via %q of type %q", contact.FullName, ch.Name, ch.Type)

	if err := ch.Notify(contact, i, obj, ev, daemon.Config().IcingaWeb2UrlParsed); err != nil {
		i.logger.Errorw("Failed to send notification via channel plugin", zap.String("type", ch.Type), zap.Error(err))
		return err
	}

	i.logger.Infow("Successfully sent notification", zap.String("type", ch.Type), zap.Stringer("contact", contact))

	return nil
}

// getRecipientsChannel returns all the configured channels of the current incident and escalation recipients.
func (i *Incident) getRecipientsChannel(t time.Time) rule.ContactChannels {
	contactChs := make(rule.ContactChannels)
	// Load all escalations recipients channels
	for escalationID := range i.EscalationState {
		escalation := i.runtimeConfig.GetRuleEscalation(escalationID)
		if escalation == nil {
			i.logger.Debugw("Incident refers unknown escalation, might got deleted", zap.Int64("escalation_id", escalationID))
			continue
		}

		contactChs.LoadFromEscalationRecipients(escalation, t, i.isRecipientNotifiable)
	}

	// Check whether all the incident recipients do have an appropriate contact channel configured.
	// When a recipient has subscribed/managed this incident via the UI, fallback to the default contact channel.
	for recipientKey, state := range i.Recipients {
		r := i.runtimeConfig.GetRecipient(recipientKey)
		if r == nil {
			i.logger.Debugw("Incident refers unknown recipient key, might got deleted", zap.Inline(recipientKey))
			continue
		}

		if i.IsNotifiable(state.Role) {
			contacts := r.GetContactsAt(t)
			if len(contacts) > 0 {
				i.logger.Debugw("Expanded recipient to contacts",
					zap.Object("recipient", r),
					zap.Objects("contacts", contacts))

				for _, contact := range contacts {
					if contactChs[contact] == nil {
						contactChs[contact] = make(map[int64]bool)
						contactChs[contact][contact.DefaultChannelID] = true
					}
				}
			} else {
				i.logger.Warnw("Recipient expanded to no contacts", zap.Object("recipient", r))
			}
		}
	}

	return contactChs
}

// RestoreState reloads all incident state from the database within the given transaction.
//
// This method must be called for existing Incidents before processing an event, as another node in an HA setup may
// have altered the state in the meantime. The incident row is locked via SELECT FOR UPDATE so that concurrent calls
// on other nodes for the same incident are serialized until the caller commits.
//
// A caller must not hold a read lock on the Incident.runtimeConfig when calling this method, as it will be
// acquired internally to restore the incident's escalation states.
//
// If the Incident has recovered in the meantime, a sql.ErrNoRows error will be returned.
func (i *Incident) RestoreState(ctx context.Context, tx *sqlx.Tx) error {
	stmt := i.db.Rebind(i.db.BuildSelectStmt(i, i) + ` WHERE "recovered_at" IS NULL AND "object_id" = ? FOR UPDATE`)
	if err := tx.GetContext(ctx, i, stmt, i.ObjectID); err != nil {
		return err
	}

	return i.restoreRelatedState(ctx, tx)
}

// restoreRelatedState restores the incident's matched rules, escalation states, and recipients from the database.
//
// A caller must not hold a read lock on the Incident.runtimeConfig when calling this method, as it will be
// acquired internally to restore the incident's escalation states.
func (i *Incident) restoreRelatedState(ctx context.Context, tx *sqlx.Tx) error {
	i.Rules = make(map[ruleID]struct{})
	err := utils.ForEachRow(ctx, i.db, tx, "incident_id", []int64{i.Id}, func(rr *RuleRow) {
		i.Rules[rr.RuleID] = struct{}{}
	})
	if err != nil {
		i.logger.Errorw("Failed to restore incident rules from the database", zap.Error(err))
		return err
	}

	i.EscalationState = make(map[escalationID]*EscalationState)
	err = utils.ForEachRow(ctx, i.db, tx, "incident_id", []int64{i.Id}, func(es *EscalationState) {
		i.EscalationState[es.RuleEscalationID] = es

		i.runtimeConfig.RLock()
		defer i.runtimeConfig.RUnlock()

		if escalation := i.runtimeConfig.GetRuleEscalation(es.RuleEscalationID); escalation != nil {
			i.Rules[escalation.RuleID] = struct{}{}
		}
	})
	if err != nil {
		i.logger.Errorw("Failed to restore incident escalation states from the database", zap.Error(err))
		return err
	}

	i.Recipients = make(map[recipient.Key]*RecipientState)
	err = utils.ForEachRow(ctx, i.db, tx, "incident_id", []int64{i.Id}, func(cr *ContactRow) {
		i.Recipients[cr.Key] = &RecipientState{Role: cr.Role}
	})
	if err != nil {
		i.logger.Errorw("Failed to restore incident recipients from the database", zap.Error(err))
		return err
	}

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
	RuleEscalationID int64           `db:"rule_escalation_id"`
	TriggeredAt      types.UnixMilli `db:"triggered_at"`
}

// TableName implements the contracts.TableNamer interface.
func (e *EscalationState) TableName() string {
	return "incident_rule_escalation_state"
}

type RecipientState struct {
	Role ContactRole
}

var (
	_ contracts.Incident = (*Incident)(nil)
)
