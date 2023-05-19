package incident

import (
	"context"
	"errors"
	"fmt"
	"github.com/icinga/icinga-notifications/internal/config"
	"github.com/icinga/icinga-notifications/internal/contracts"
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
	Object           *object.Object
	StartedAt        time.Time
	RecoveredAt      time.Time
	SeverityBySource map[int64]event.Severity

	EscalationState map[escalationID]*EscalationState
	Rules           map[ruleID]struct{}
	Recipients      map[recipient.Key]*RecipientState

	incidentRowID int64

	db            *icingadb.DB
	logger        *zap.SugaredLogger
	runtimeConfig *config.RuntimeConfig
	configFile    *config.ConfigFile

	sync.Mutex
}

func (i *Incident) ObjectDisplayName() string {
	return i.Object.DisplayName()
}

func (i *Incident) String() string {
	return fmt.Sprintf("#%d", i.incidentRowID)
}

func (i *Incident) ID() int64 {
	return i.incidentRowID
}

func (i *Incident) Severity() event.Severity {
	maxSeverity := event.SeverityOK
	for _, s := range i.SeverityBySource {
		if s > maxSeverity {
			maxSeverity = s
		}
	}
	return maxSeverity
}

func (i *Incident) HasManager() bool {
	for _, state := range i.Recipients {
		if state.Role == RoleManager {
			return true
		}
	}

	return false
}

// ProcessEvent processes the given event for the current incident.
func (i *Incident) ProcessEvent(ctx context.Context, tx *sqlx.Tx, ev event.Event, created bool) error {
	i.Lock()
	defer i.Unlock()

	i.runtimeConfig.RLock()
	defer i.runtimeConfig.RUnlock()

	if created {
		err := i.processIncidentOpenedEvent(ctx, tx, ev)
		if err != nil {
			return err
		}

		i.logger = i.logger.With(zap.String("incident", i.String()))
	}

	if err := i.AddEvent(ctx, tx, &ev); err != nil {
		i.logger.Errorw("Can't insert incident event to the database", zap.Error(err))

		return errors.New("can't insert incident event to the database")
	}

	if ev.Type == event.TypeAcknowledgement {
		return i.processAcknowledgementEvent(ctx, tx, ev)
	}

	causedBy, err := i.processSeverityChangedEvent(ctx, tx, ev)
	if err != nil {
		return err
	}

	// Check if any (additional) rules match this object. Filters of rules that already have a state don't have
	// to be checked again, these rules already matched and stay effective for the ongoing incident.
	causedBy, err = i.evaluateRules(ctx, tx, ev.ID, causedBy)
	if err != nil {
		return err
	}

	// Re-evaluate escalations based on the newly evaluated rules.
	i.evaluateEscalations()

	return i.notifyContacts(ctx, tx, &ev, causedBy)
}

func (i *Incident) processSeverityChangedEvent(ctx context.Context, tx *sqlx.Tx, ev event.Event) (types.Int, error) {
	oldIncidentSeverity := i.Severity()
	oldSourceSeverity := i.SeverityBySource[ev.SourceId]
	if oldSourceSeverity == event.SeverityNone {
		oldSourceSeverity = event.SeverityOK
	}

	if oldSourceSeverity == ev.Severity {
		msg := fmt.Sprintf("Ignoring superfluous %q state event from source %d", ev.Severity.String(), ev.SourceId)
		i.logger.Warnln(msg)

		return types.Int{}, errors.New(msg)
	}

	i.logger.Infof(
		"Source %d severity changed from %s to %s", ev.SourceId, oldSourceSeverity.String(), ev.Severity.String(),
	)

	history := &HistoryRow{
		EventID:     utils.ToDBInt(ev.ID),
		Type:        SourceSeverityChanged,
		Time:        types.UnixMilli(time.Now()),
		NewSeverity: ev.Severity,
		OldSeverity: oldSourceSeverity,
		Message:     utils.ToDBString(ev.Message),
	}
	causedByHistoryId, err := i.AddHistory(ctx, tx, history, true)
	if err != nil {
		i.logger.Errorw("Can't insert source severity changed history to the database", zap.Error(err))

		return types.Int{}, errors.New("can't insert source severity changed history to the database")
	}

	if err = i.AddSourceSeverity(ctx, tx, ev.Severity, ev.SourceId); err != nil {
		i.logger.Errorw("Failed to upsert source severity", zap.Error(err))

		return types.Int{}, errors.New("failed to upsert source severity")
	}

	if ev.Severity == event.SeverityOK {
		delete(i.SeverityBySource, ev.SourceId)
	}

	newIncidentSeverity := i.Severity()
	if newIncidentSeverity != oldIncidentSeverity {
		i.logger.Infof(
			"Incident severity changed from %s to %s", oldIncidentSeverity.String(), newIncidentSeverity.String(),
		)

		if err = i.Sync(ctx, tx); err != nil {
			i.logger.Errorw("Failed to update incident severity", zap.Error(err))

			return types.Int{}, errors.New("failed to update incident severity")
		}

		history = &HistoryRow{
			EventID:                   utils.ToDBInt(ev.ID),
			Time:                      types.UnixMilli(time.Now()),
			Type:                      SeverityChanged,
			NewSeverity:               newIncidentSeverity,
			OldSeverity:               oldIncidentSeverity,
			CausedByIncidentHistoryID: causedByHistoryId,
		}
		if causedByHistoryId, err = i.AddHistory(ctx, tx, history, true); err != nil {
			i.logger.Errorw("Failed to insert incident severity changed history", zap.Error(err))

			return types.Int{}, errors.New("failed to insert incident severity changed history")
		}
	}

	if newIncidentSeverity == event.SeverityOK {
		i.RecoveredAt = time.Now()
		i.logger.Info("All sources recovered, closing incident")

		RemoveCurrent(i.Object)

		incidentRow := &IncidentRow{ID: i.incidentRowID, RecoveredAt: types.UnixMilli(i.RecoveredAt)}
		_, err = tx.NamedExecContext(ctx, `UPDATE "incident" SET "recovered_at" = :recovered_at WHERE id = :id`, incidentRow)
		if err != nil {
			i.logger.Errorw("Failed to close incident", zap.Error(err))

			return types.Int{}, errors.New("failed to close incident")
		}

		history = &HistoryRow{
			EventID: utils.ToDBInt(ev.ID),
			Time:    types.UnixMilli(i.RecoveredAt),
			Type:    Closed,
		}

		_, err = i.AddHistory(ctx, tx, history, false)
		if err != nil {
			i.logger.Errorw("Can't insert incident closed history to the database", zap.Error(err))

			return types.Int{}, errors.New("can't insert incident closed history to the database")
		}
	}

	return causedByHistoryId, nil
}

func (i *Incident) processIncidentOpenedEvent(ctx context.Context, tx *sqlx.Tx, ev event.Event) error {
	i.StartedAt = ev.Time
	if err := i.Sync(ctx, tx); err != nil {
		i.logger.Errorw("Can't insert incident to the database", zap.Error(err))

		return errors.New("can't insert incident to the database")
	}

	i.logger.Info("Opened incident")
	historyRow := &HistoryRow{
		Type:    Opened,
		Time:    types.UnixMilli(ev.Time),
		EventID: utils.ToDBInt(ev.ID),
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

// evaluateEscalations evaluates this incidents rule escalations if they aren't already.
// Returns error on database failure.
func (i *Incident) evaluateEscalations() {
	if i.EscalationState == nil {
		i.EscalationState = make(map[int64]*EscalationState)
	}

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
					cond := &rule.EscalationFilter{
						IncidentAge:      time.Now().Sub(i.StartedAt),
						IncidentSeverity: i.Severity(),
					}

					var err error
					matched, err = escalation.Condition.Eval(cond)
					if err != nil {
						i.logger.Warnw(
							"Failed to evaluate escalation condition", zap.String("rule", r.Name),
							zap.String("escalation", escalation.DisplayName()), zap.Error(err),
						)

						matched = false
					}
				}

				if matched {
					i.EscalationState[escalation.ID] = new(EscalationState)
				}
			}
		}
	}
}

// notifyContacts evaluates the incident.EscalationState and checks if escalations need to be triggered
// as well as builds the incident recipients along with their channel types and sends notifications based on that.
// Returns error on database failure.
func (i *Incident) notifyContacts(ctx context.Context, tx *sqlx.Tx, ev *event.Event, causedBy types.Int) error {
	managed := i.HasManager()

	contactChannels := make(map[*recipient.Contact]map[string]struct{})

	if i.Recipients == nil {
		i.Recipients = make(map[recipient.Key]*RecipientState)
	}

	escalationRecipients := make(map[recipient.Key]bool)
	for escalationID, state := range i.EscalationState {
		escalation := i.runtimeConfig.GetRuleEscalation(escalationID)
		if state.TriggeredAt.Time().IsZero() {
			if escalation == nil {
				continue
			}

			state.RuleEscalationID = escalationID
			state.TriggeredAt = types.UnixMilli(time.Now())

			r := i.runtimeConfig.Rules[escalation.RuleID]
			i.logger.Infof("Rule %q reached escalation %q", r.Name, escalation.DisplayName())

			err := i.AddEscalationTriggered(ctx, tx, state)
			if err != nil {
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
			causedBy, err = i.AddHistory(ctx, tx, history, true)
			if err != nil {
				i.logger.Errorw(
					"Failed to insert escalation triggered incident history", zap.String("rule", r.Name),
					zap.String("escalation", escalation.DisplayName()), zap.Error(err),
				)

				return errors.New("failed to insert escalation triggered incident history")
			}

			err = i.AddRecipient(ctx, tx, escalation, ev.ID)
			if err != nil {
				return err
			}
		}

		for _, escalationRecipient := range escalation.Recipients {
			state := i.Recipients[escalationRecipient.Key]
			if state == nil {
				continue
			}

			escalationRecipients[escalationRecipient.Key] = true

			if !managed || state.Role > RoleRecipient {
				for _, c := range escalationRecipient.Recipient.GetContactsAt(ev.Time) {
					if contactChannels[c] == nil {
						contactChannels[c] = make(map[string]struct{})
					}
					if escalationRecipient.ChannelType.Valid {
						contactChannels[c][escalationRecipient.ChannelType.String] = struct{}{}
					} else {
						contactChannels[c][c.DefaultChannel] = struct{}{}
					}
				}
			}
		}
	}

	for recipientKey, state := range i.Recipients {
		r := i.runtimeConfig.GetRecipient(recipientKey)
		if r == nil {
			continue
		}

		isEscalationRecipient := escalationRecipients[recipientKey]
		if !isEscalationRecipient && (!managed || state.Role > RoleRecipient) {
			for _, contact := range r.GetContactsAt(ev.Time) {
				if contactChannels[contact] == nil {
					contactChannels[contact] = make(map[string]struct{})
				}
				contactChannels[contact][contact.DefaultChannel] = struct{}{}
			}
		}
	}

	for contact, channels := range contactChannels {
		hr := &HistoryRow{
			Key:                       recipient.ToKey(contact),
			EventID:                   utils.ToDBInt(ev.ID),
			Time:                      types.UnixMilli(time.Now()),
			Type:                      Notified,
			CausedByIncidentHistoryID: causedBy,
		}

		if err := ctx.Err(); err != nil {
			return err
		}

		for chType := range channels {
			i.logger.Infof("Notify contact %q via %q", contact.FullName, chType)

			hr.ChannelType = utils.ToDBString(chType)

			_, err := i.AddHistory(ctx, tx, hr, false)
			if err != nil {
				i.logger.Errorw(
					"Failed to insert contact notified incident history", zap.String("contact", contact.String()),
					zap.Error(err),
				)
			}

			chConf := i.runtimeConfig.Channels[chType]
			if chConf == nil {
				i.logger.Errorw("Could not find config for channel", zap.String("type", chType))
				continue
			}

			plugin, err := chConf.GetPlugin()
			if err != nil {
				i.logger.Errorw("Could not initialize channel", zap.String("type", chType), zap.Error(err))
				continue
			}

			err = plugin.Send(contact, i, ev, i.configFile.Icingaweb2URL)
			if err != nil {
				i.logger.Errorw("Failed to send via channel", zap.String("type", chType), zap.Error(err))
				continue
			}

			i.logger.Infow(
				"Successfully sent a message via channel", zap.String("type", chType), zap.String("contact", contact.String()),
			)
		}
	}

	return nil
}

// processAcknowledgementEvent processes the given ack event.
// Promotes the ack author to incident.RoleManager if it's not already the case and generates a history entry.
// Returns error on database failure.
func (i *Incident) processAcknowledgementEvent(ctx context.Context, tx *sqlx.Tx, ev event.Event) error {
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
