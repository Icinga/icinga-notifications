package incident

import (
	"context"
	"errors"
	"fmt"
	"github.com/icinga/icinga-notifications/internal/config"
	"github.com/icinga/icinga-notifications/internal/contracts"
	"github.com/icinga/icinga-notifications/internal/daemon"
	"github.com/icinga/icinga-notifications/internal/event"
	"github.com/icinga/icinga-notifications/internal/object"
	"github.com/icinga/icinga-notifications/internal/recipient"
	"github.com/icinga/icinga-notifications/internal/rule"
	"github.com/icinga/icinga-notifications/internal/utils"
	"github.com/icinga/icingadb/pkg/icingadb"
	"github.com/icinga/icingadb/pkg/types"
	"github.com/jmoiron/sqlx"
	"go.uber.org/zap"
	"sync"
	"time"
)

type ruleID = int64
type escalationID = int64

type Incident struct {
	Object      *object.Object
	StartedAt   time.Time
	RecoveredAt time.Time
	Severity    event.Severity

	EscalationState map[escalationID]*EscalationState
	Rules           map[ruleID]struct{}
	Recipients      map[recipient.Key]*RecipientState

	incidentRowID int64

	// timer calls RetriggerEscalations the next time any escalation could be reached on the incident.
	//
	// For example, if there are escalations configured for incident_age>=1h and incident_age>=2h, if the incident
	// is less than an hour old, timer will fire 1h after incident start, if the incident is between 1h and 2h
	// old, timer will fire after 2h, and if the incident is already older than 2h, no future escalations can
	// be reached solely based on the incident aging, so no more timer is necessary and timer stores nil.
	timer *time.Timer

	db            *icingadb.DB
	logger        *zap.SugaredLogger
	runtimeConfig *config.RuntimeConfig

	sync.Mutex
}

func NewIncident(
	db *icingadb.DB, obj *object.Object, runtimeConfig *config.RuntimeConfig, logger *zap.SugaredLogger,
) *Incident {
	return &Incident{
		db:              db,
		Object:          obj,
		logger:          logger,
		runtimeConfig:   runtimeConfig,
		EscalationState: map[escalationID]*EscalationState{},
		Rules:           map[ruleID]struct{}{},
		Recipients:      map[recipient.Key]*RecipientState{},
	}
}

func (i *Incident) IncidentObject() *object.Object {
	return i.Object
}

func (i *Incident) SeverityString() string {
	return i.Severity.String()
}

func (i *Incident) String() string {
	return fmt.Sprintf("#%d", i.incidentRowID)
}

func (i *Incident) ID() int64 {
	return i.incidentRowID
}

func (i *Incident) HasManager() bool {
	for _, state := range i.Recipients {
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
func (i *Incident) ProcessEvent(ctx context.Context, ev *event.Event, created bool) error {
	i.Lock()
	defer i.Unlock()

	i.runtimeConfig.RLock()
	defer i.runtimeConfig.RUnlock()

	tx, err := i.db.BeginTxx(ctx, nil)
	if err != nil {
		i.logger.Errorw("Can't start a db transaction", zap.Error(err))

		return errors.New("can't start a db transaction")
	}
	defer func() { _ = tx.Rollback() }()

	if err = ev.Sync(ctx, tx, i.db, i.Object.ID); err != nil {
		i.logger.Errorw("Failed to insert event and fetch its ID", zap.String("event", ev.String()), zap.Error(err))

		return errors.New("can't insert event and fetch its ID")
	}

	if created {
		err = i.processIncidentOpenedEvent(ctx, tx, ev)
		if err != nil {
			return err
		}

		i.logger = i.logger.With(zap.String("incident", i.String()))
	}

	if err = i.AddEvent(ctx, tx, ev); err != nil {
		i.logger.Errorw("Can't insert incident event to the database", zap.Error(err))

		return errors.New("can't insert incident event to the database")
	}

	if ev.Type == event.TypeAcknowledgement {
		return i.processAcknowledgementEvent(ctx, tx, ev)
	}

	var causedBy types.Int
	if !created {
		causedBy, err = i.processSeverityChangedEvent(ctx, tx, ev)
		if err != nil {
			return err
		}
	}

	// Check if any (additional) rules match this object. Filters of rules that already have a state don't have
	// to be checked again, these rules already matched and stay effective for the ongoing incident.
	causedBy, err = i.evaluateRules(ctx, tx, ev.ID, causedBy)
	if err != nil {
		return err
	}

	// Re-evaluate escalations based on the newly evaluated rules.
	escalations, err := i.evaluateEscalations(ev.Time)
	if err != nil {
		return err
	}

	if err := i.triggerEscalations(ctx, tx, ev, causedBy, escalations); err != nil {
		return err
	}

	notifications, err := i.addPendingNotifications(ctx, tx, ev, i.getRecipientsChannel(ev.Time), causedBy)
	if err != nil {
		return err
	}

	if err = tx.Commit(); err != nil {
		i.logger.Errorw("Can't commit db transaction", zap.Error(err))

		return errors.New("can't commit db transaction")
	}

	return i.notifyContacts(ctx, ev, notifications)
}

// RetriggerEscalations tries to re-evaluate the escalations and notify contacts.
func (i *Incident) RetriggerEscalations(ev *event.Event) {
	i.Lock()
	defer i.Unlock()

	i.runtimeConfig.RLock()
	defer i.runtimeConfig.RUnlock()

	if !i.RecoveredAt.IsZero() {
		// Incident is recovered in the meantime.
		return
	}

	if !time.Now().After(ev.Time) {
		i.logger.DPanicw("Event from the future", zap.Time("event_time", ev.Time), zap.Any("event", ev))
		return
	}

	escalations, err := i.evaluateEscalations(ev.Time)
	if err != nil {
		i.logger.Errorw("Reevaluating time-based escalations failed", zap.Error(err))
		return
	}

	if len(escalations) == 0 {
		i.logger.Debug("Reevaluated escalations, no new escalations triggered")
		return
	}

	var notifications []*NotificationEntry
	ctx := context.Background()
	err = utils.RunInTx(ctx, i.db, func(tx *sqlx.Tx) error {
		err := ev.Sync(ctx, tx, i.db, i.Object.ID)
		if err != nil {
			return err
		}

		if err = i.triggerEscalations(ctx, tx, ev, types.Int{}, escalations); err != nil {
			return err
		}

		channels := make(contactChannels)
		for _, escalation := range escalations {
			channels.loadEscalationRecipientsChannel(escalation, i, ev.Time)
		}

		notifications, err = i.addPendingNotifications(ctx, tx, ev, channels, types.Int{})

		return err
	})
	if err != nil {
		i.logger.Errorw("Reevaluating time-based escalations failed", zap.Error(err))
	} else {
		if err = i.notifyContacts(ctx, ev, notifications); err != nil {
			i.logger.Errorw("Failed to notify reevaluated escalation recipients", zap.Error(err))
			return
		}

		i.logger.Info("Successfully reevaluated time-based escalations")
	}
}

func (i *Incident) processSeverityChangedEvent(ctx context.Context, tx *sqlx.Tx, ev *event.Event) (types.Int, error) {
	var causedByHistoryId types.Int
	oldSeverity := i.Severity
	newSeverity := ev.Severity
	if oldSeverity == newSeverity {
		msg := fmt.Sprintf("Ignoring superfluous %q state event from source %d", ev.Severity.String(), ev.SourceId)
		i.logger.Warnln(msg)

		return causedByHistoryId, errors.New(msg)
	}

	i.logger.Infof("Incident severity changed from %s to %s", oldSeverity.String(), newSeverity.String())

	history := &HistoryRow{
		EventID:     utils.ToDBInt(ev.ID),
		Time:        types.UnixMilli(time.Now()),
		Type:        SeverityChanged,
		NewSeverity: newSeverity,
		OldSeverity: oldSeverity,
		Message:     utils.ToDBString(ev.Message),
	}

	historyId, err := i.AddHistory(ctx, tx, history, true)
	if err != nil {
		i.logger.Errorw("Failed to insert incident severity changed history", zap.Error(err))

		return causedByHistoryId, errors.New("failed to insert incident severity changed history")
	}

	causedByHistoryId = historyId

	if newSeverity == event.SeverityOK {
		i.RecoveredAt = time.Now()
		i.logger.Info("All sources recovered, closing incident")

		RemoveCurrent(i.Object)

		history := &HistoryRow{
			EventID: utils.ToDBInt(ev.ID),
			Time:    types.UnixMilli(i.RecoveredAt),
			Type:    Closed,
		}

		_, err = i.AddHistory(ctx, tx, history, false)
		if err != nil {
			i.logger.Errorw("Can't insert incident closed history to the database", zap.Error(err))

			return types.Int{}, errors.New("can't insert incident closed history to the database")
		}

		if i.timer != nil {
			i.timer.Stop()
		}
	}

	i.Severity = newSeverity
	if err := i.Sync(ctx, tx); err != nil {
		i.logger.Errorw("Failed to update incident severity", zap.Error(err))

		return causedByHistoryId, errors.New("failed to update incident severity")
	}

	return causedByHistoryId, nil
}

func (i *Incident) processIncidentOpenedEvent(ctx context.Context, tx *sqlx.Tx, ev *event.Event) error {
	i.StartedAt = ev.Time
	i.Severity = ev.Severity
	if err := i.Sync(ctx, tx); err != nil {
		i.logger.Errorw("Can't insert incident to the database", zap.Error(err))

		return errors.New("can't insert incident to the database")
	}

	i.logger.Infow(fmt.Sprintf("Source %d opened incident at severity %q", ev.SourceId, i.Severity.String()), zap.String("message", ev.Message))

	historyRow := &HistoryRow{
		Type:        Opened,
		Time:        types.UnixMilli(ev.Time),
		EventID:     utils.ToDBInt(ev.ID),
		NewSeverity: i.Severity,
		Message:     utils.ToDBString(ev.Message),
	}

	if _, err := i.AddHistory(ctx, tx, historyRow, false); err != nil {
		i.logger.Errorw("Can't insert incident opened history event", zap.Error(err))

		return errors.New("can't insert incident opened history event")
	}

	return nil
}

// evaluateRules evaluates all the configured rules for this *incident.Object and
// generates history entries for each matched rule.
// Returns error on database failure.
func (i *Incident) evaluateRules(ctx context.Context, tx *sqlx.Tx, eventID int64, causedBy types.Int) (types.Int, error) {
	if i.Rules == nil {
		i.Rules = make(map[int64]struct{})
	}

	for _, r := range i.runtimeConfig.Rules {
		if !r.IsActive.Valid || !r.IsActive.Bool {
			continue
		}

		if _, ok := i.Rules[r.ID]; !ok {
			if r.ObjectFilter != nil {
				matched, err := r.ObjectFilter.Eval(i.Object)
				if err != nil {
					i.logger.Warnw("Failed to evaluate object filter", zap.String("rule", r.Name), zap.Error(err))
				}

				if err != nil || !matched {
					continue
				}
			}

			i.Rules[r.ID] = struct{}{}
			i.logger.Infof("Rule %q matches", r.Name)

			err := i.AddRuleMatched(ctx, tx, r)
			if err != nil {
				i.logger.Errorw("Failed to upsert incident rule", zap.String("rule", r.Name), zap.Error(err))

				return types.Int{}, errors.New("failed to insert incident rule")
			}

			history := &HistoryRow{
				Time:                      types.UnixMilli(time.Now()),
				EventID:                   utils.ToDBInt(eventID),
				RuleID:                    utils.ToDBInt(r.ID),
				Type:                      RuleMatched,
				CausedByIncidentHistoryID: causedBy,
			}
			insertedID, err := i.AddHistory(ctx, tx, history, true)
			if err != nil {
				i.logger.Errorw("Failed to insert rule matched incident history", zap.String("rule", r.Name), zap.Error(err))

				return types.Int{}, errors.New("failed to insert rule matched incident history")
			}

			if insertedID.Valid && !causedBy.Valid {
				causedBy = insertedID
			}
		}
	}

	return causedBy, nil
}

// evaluateEscalations evaluates this incidents rule escalations to be triggered if they aren't already.
// Returns the newly evaluated escalations to be triggered or an error on database failure.
func (i *Incident) evaluateEscalations(eventTime time.Time) ([]*rule.Escalation, error) {
	if i.EscalationState == nil {
		i.EscalationState = make(map[int64]*EscalationState)
	}

	// Escalations are reevaluated now, reset any existing timer, if there might be future time-based escalations,
	// this function will start a new timer.
	if i.timer != nil {
		i.logger.Info("Stopping reevaluate timer due to escalation evaluation")
		i.timer.Stop()
		i.timer = nil
	}

	filterContext := &rule.EscalationFilter{IncidentAge: eventTime.Sub(i.StartedAt), IncidentSeverity: i.Severity}

	var escalations []*rule.Escalation
	retryAfter := rule.RetryNever

	for rID := range i.Rules {
		r := i.runtimeConfig.Rules[rID]

		if r == nil || !r.IsActive.Valid || !r.IsActive.Bool {
			continue
		}

		// Check if new escalation stages are reached
		for _, escalation := range r.Escalations {
			if _, ok := i.EscalationState[escalation.ID]; !ok {
				matched := false

				if escalation.Condition == nil {
					matched = true
				} else {
					var err error
					matched, err = escalation.Condition.Eval(filterContext)
					if err != nil {
						i.logger.Warnw(
							"Failed to evaluate escalation condition", zap.String("rule", r.Name),
							zap.String("escalation", escalation.DisplayName()), zap.Error(err),
						)

						matched = false
					} else if !matched {
						incidentAgeFilter := filterContext.ReevaluateAfter(escalation.Condition)
						retryAfter = min(retryAfter, incidentAgeFilter)
					}
				}

				if matched {
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
		i.timer = time.AfterFunc(retryAfter, func() {
			i.logger.Info("Reevaluating escalations")

			i.RetriggerEscalations(&event.Event{
				Type:    event.TypeInternal,
				Time:    nextEvalAt,
				Message: fmt.Sprintf("Incident reached age %v", nextEvalAt.Sub(i.StartedAt)),
			})
		})
	}

	return escalations, nil
}

// triggerEscalations triggers the given escalations and generates incident history items for each of them.
// Returns an error on database failure.
func (i *Incident) triggerEscalations(ctx context.Context, tx *sqlx.Tx, ev *event.Event, causedBy types.Int, escalations []*rule.Escalation) error {
	for _, escalation := range escalations {
		r := i.runtimeConfig.Rules[escalation.RuleID]
		i.logger.Infof("Rule %q reached escalation %q", r.Name, escalation.DisplayName())

		state := &EscalationState{RuleEscalationID: escalation.ID, TriggeredAt: types.UnixMilli(time.Now())}
		i.EscalationState[escalation.ID] = state

		if err := i.AddEscalationTriggered(ctx, tx, state); err != nil {
			i.logger.Errorw(
				"Failed to upsert escalation state", zap.String("rule", r.Name),
				zap.String("escalation", escalation.DisplayName()), zap.Error(err),
			)

			return errors.New("failed to upsert escalation state")
		}

		history := &HistoryRow{
			Time:                      state.TriggeredAt,
			EventID:                   utils.ToDBInt(ev.ID),
			RuleEscalationID:          utils.ToDBInt(state.RuleEscalationID),
			RuleID:                    utils.ToDBInt(r.ID),
			Type:                      EscalationTriggered,
			CausedByIncidentHistoryID: causedBy,
		}

		if _, err := i.AddHistory(ctx, tx, history, false); err != nil {
			i.logger.Errorw(
				"Failed to insert escalation triggered incident history", zap.String("rule", r.Name),
				zap.String("escalation", escalation.DisplayName()), zap.Error(err),
			)

			return errors.New("failed to insert escalation triggered incident history")
		}

		if err := i.AddRecipient(ctx, tx, escalation, ev.ID); err != nil {
			return err
		}
	}

	return nil
}

// notifyContacts executes all the given pending notifications of the current incident.
// Returns error on database failure or if the provided context is cancelled.
func (i *Incident) notifyContacts(ctx context.Context, ev *event.Event, notifications []*NotificationEntry) error {
	for _, notification := range notifications {
		contact := i.runtimeConfig.Contacts[notification.ContactID]

		if i.notifyContact(contact, ev, notification.ChannelID) != nil {
			notification.State = NotificationStateFailed
		} else {
			notification.State = NotificationStateSent
		}

		notification.SentAt = types.UnixMilli(time.Now())
		stmt, _ := i.db.BuildUpdateStmt(notification)
		if _, err := i.db.NamedExecContext(ctx, stmt, notification); err != nil {
			i.logger.Errorw(
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
func (i *Incident) notifyContact(contact *recipient.Contact, ev *event.Event, chID int64) error {
	ch := i.runtimeConfig.Channels[chID]
	if ch == nil {
		i.logger.Errorw("Could not find config for channel", zap.Int64("channel_id", chID))

		return fmt.Errorf("could not find config for channel ID: %d", chID)
	}

	i.logger.Infow(fmt.Sprintf("Notify contact %q via %q of type %q", contact.FullName, ch.Name, ch.Type), zap.Int64("channel_id", chID))

	err := ch.Notify(contact, i, ev, daemon.Config().Icingaweb2URL)
	if err != nil {
		i.logger.Errorw("Failed to send notification via channel plugin", zap.String("type", ch.Type), zap.Error(err))
		return err
	}

	i.logger.Infow(
		"Successfully sent a notification via channel plugin", zap.String("type", ch.Type), zap.String("contact", contact.FullName),
	)

	return nil
}

// processAcknowledgementEvent processes the given ack event.
// Promotes the ack author to incident.RoleManager if it's not already the case and generates a history entry.
// Returns error on database failure.
func (i *Incident) processAcknowledgementEvent(ctx context.Context, tx *sqlx.Tx, ev *event.Event) error {
	contact := i.runtimeConfig.GetContact(ev.Username)
	if contact == nil {
		i.logger.Warnw("Ignoring acknowledgement event from an unknown author", zap.String("author", ev.Username))

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
			return nil
		}
	} else {
		i.Recipients[recipientKey] = &RecipientState{Role: newRole}
	}

	i.logger.Infof("Contact %q role changed from %s to %s", contact.String(), oldRole.String(), newRole.String())

	hr := &HistoryRow{
		Key:              recipientKey,
		EventID:          utils.ToDBInt(ev.ID),
		Type:             RecipientRoleChanged,
		Time:             types.UnixMilli(time.Now()),
		NewRecipientRole: newRole,
		OldRecipientRole: oldRole,
		Message:          utils.ToDBString(ev.Message),
	}

	_, err := i.AddHistory(ctx, tx, hr, false)
	if err != nil {
		i.logger.Errorw(
			"Failed to add recipient role changed history", zap.String("recipient", contact.String()), zap.Error(err),
		)

		return errors.New("failed to add recipient role changed history")
	}

	cr := &ContactRow{IncidentID: hr.IncidentID, Key: recipientKey, Role: newRole}

	stmt, _ := i.db.BuildUpsertStmt(cr)
	_, err = tx.NamedExecContext(ctx, stmt, cr)
	if err != nil {
		i.logger.Errorw(
			"Failed to upsert incident contact", zap.String("contact", contact.String()), zap.Error(err),
		)

		return errors.New("failed to upsert incident contact")
	}

	return nil
}

// RestoreEscalationStateRules restores this incident's rules based on the given escalation states.
func (i *Incident) RestoreEscalationStateRules(states []*EscalationState) {
	i.runtimeConfig.RLock()
	defer i.runtimeConfig.RUnlock()

	for _, state := range states {
		escalation := i.runtimeConfig.GetRuleEscalation(state.RuleEscalationID)
		i.Rules[escalation.RuleID] = struct{}{}
	}
}

// getRecipientsChannel returns all the configured channels of the current incident and escalation recipients.
func (i *Incident) getRecipientsChannel(t time.Time) contactChannels {
	contactChs := make(contactChannels)
	// Load all escalations recipients channels
	for escalationID := range i.EscalationState {
		escalation := i.runtimeConfig.GetRuleEscalation(escalationID)
		if escalation == nil {
			continue
		}

		contactChs.loadEscalationRecipientsChannel(escalation, i, t)
	}

	// Check whether all the incident recipients do have an appropriate contact channel configured.
	// When a recipient has subscribed/managed this incident via the UI or using an ACK, fallback
	// to the default contact channel.
	for recipientKey, state := range i.Recipients {
		r := i.runtimeConfig.GetRecipient(recipientKey)
		if r == nil {
			continue
		}

		if i.IsNotifiable(state.Role) {
			for _, contact := range r.GetContactsAt(t) {
				if contactChs[contact] == nil {
					contactChs[contact] = make(map[int64]bool)
					contactChs[contact][contact.DefaultChannelID] = true
				}
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
	err := i.db.SelectContext(ctx, &contacts, i.db.Rebind(i.db.BuildSelectStmt(contact, contact)+` WHERE "incident_id" = ?`), i.ID())
	if err != nil {
		i.logger.Errorw(
			"Failed to restore incident recipients from the database", zap.String("object", i.IncidentObject().DisplayName()),
			zap.String("incident", i.String()), zap.Error(err),
		)

		return errors.New("failed to restore incident recipients")
	}

	recipients := make(map[recipient.Key]*RecipientState)
	for _, contact := range contacts {
		recipients[contact.Key] = &RecipientState{Role: contact.Role}
	}

	i.Recipients = recipients

	return nil
}

// restoreEscalationsState restores all escalation states matching the current incident id from the database.
// Returns error on database failure.
func (i *Incident) restoreEscalationsState(ctx context.Context) error {
	state := &EscalationState{}
	var states []*EscalationState
	err := i.db.SelectContext(ctx, &states, i.db.Rebind(i.db.BuildSelectStmt(state, state)+` WHERE "incident_id" = ?`), i.ID())
	if err != nil {
		i.logger.Errorw("Failed to restore incident rule escalation states", zap.Error(err))

		return errors.New("failed to restore incident rule escalation states")
	}

	for _, state := range states {
		i.EscalationState[state.RuleEscalationID] = state
	}

	i.RestoreEscalationStateRules(states)

	return nil
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

// contactChannels stores a set of channel IDs for each set of individual contacts.
type contactChannels map[*recipient.Contact]map[int64]bool

// loadEscalationRecipientsChannel loads all the configured channel of all the provided escalation recipients.
func (rct contactChannels) loadEscalationRecipientsChannel(escalation *rule.Escalation, i *Incident, t time.Time) {
	for _, escalationRecipient := range escalation.Recipients {
		state := i.Recipients[escalationRecipient.Key]
		if state == nil {
			continue
		}

		if i.IsNotifiable(state.Role) {
			for _, c := range escalationRecipient.Recipient.GetContactsAt(t) {
				if rct[c] == nil {
					rct[c] = make(map[int64]bool)
				}
				if escalationRecipient.ChannelID.Valid {
					rct[c][escalationRecipient.ChannelID.Int64] = true
				} else {
					rct[c][c.DefaultChannelID] = true
				}
			}
		}
	}
}

var (
	_ contracts.Incident = (*Incident)(nil)
)
