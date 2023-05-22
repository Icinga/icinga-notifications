package incident

import (
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
	"github.com/icinga/icingadb/pkg/logging"
	"github.com/icinga/icingadb/pkg/types"
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
	logger        *logging.Logger
	runtimeConfig *config.RuntimeConfig
	configFile    *config.ConfigFile

	sync.Mutex
}

func (i *Incident) ObjectDisplayName() string {
	return i.Object.DisplayName()
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

func (i *Incident) String() string {
	return fmt.Sprintf("%d", i.incidentRowID)
}

func (i *Incident) ProcessEvent(ev event.Event, created bool) (types.Int, error) {
	if created {
		i.StartedAt = ev.Time
		if err := i.Sync(); err != nil {
			i.logger.Errorln(err)

			return types.Int{}, err
		}

		i.logger.Infof("[%s %s] opened incident", i.Object.DisplayName(), i.String())
		historyRow := &HistoryRow{
			Type:    Opened,
			Time:    types.UnixMilli(ev.Time),
			EventID: utils.ToDBInt(ev.ID),
		}

		if _, err := i.AddHistory(historyRow, false); err != nil {
			i.logger.Errorln(err)

			return types.Int{}, err
		}
	}

	if err := i.AddEvent(&ev); err != nil {
		i.logger.Errorln(err)

		return types.Int{}, err
	}

	if ev.Type == event.TypeAcknowledgement {
		return types.Int{}, i.ProcessAcknowledgementEvent(ev)
	}

	oldIncidentSeverity := i.Severity()
	oldSourceSeverity := i.SeverityBySource[ev.SourceId]
	if oldSourceSeverity == event.SeverityNone {
		oldSourceSeverity = event.SeverityOK
	}

	if oldSourceSeverity == ev.Severity {
		msg := fmt.Sprintf("%s: ignoring superfluous %q state event from source %d", i.Object.DisplayName(), ev.Severity.String(), ev.SourceId)
		i.logger.Warnln(msg)

		return types.Int{}, errors.New(msg)
	}

	i.logger.Infof(
		"[%s %s] source %d severity changed from %s to %s",
		i.Object.DisplayName(), i.String(), ev.SourceId, oldSourceSeverity.String(), ev.Severity.String(),
	)

	history := &HistoryRow{
		EventID:     utils.ToDBInt(ev.ID),
		Type:        SourceSeverityChanged,
		Time:        types.UnixMilli(time.Now()),
		NewSeverity: ev.Severity,
		OldSeverity: oldSourceSeverity,
		Message:     utils.ToDBString(ev.Message),
	}
	causedByHistoryId, err := i.AddHistory(history, true)
	if err != nil {
		i.logger.Errorln(err)

		return types.Int{}, err
	}

	if err = i.AddSourceSeverity(ev.Severity, ev.SourceId); err != nil {
		i.logger.Errorln(err)

		return types.Int{}, err
	}

	if ev.Severity == event.SeverityOK {
		delete(i.SeverityBySource, ev.SourceId)
	}

	newIncidentSeverity := i.Severity()
	if newIncidentSeverity != oldIncidentSeverity {
		i.logger.Infof(
			"[%s %s] incident severity changed from %s to %s",
			i.Object.DisplayName(), i.String(), oldIncidentSeverity.String(), newIncidentSeverity.String(),
		)

		if err = i.Sync(); err != nil {
			i.logger.Errorln(err)

			return types.Int{}, err
		}

		history = &HistoryRow{
			EventID:                   utils.ToDBInt(ev.ID),
			Time:                      types.UnixMilli(time.Now()),
			Type:                      SeverityChanged,
			NewSeverity:               newIncidentSeverity,
			OldSeverity:               oldIncidentSeverity,
			CausedByIncidentHistoryID: causedByHistoryId,
		}
		if causedByHistoryId, err = i.AddHistory(history, true); err != nil {
			i.logger.Errorln(err)

			return types.Int{}, err
		}
	}

	if newIncidentSeverity == event.SeverityOK {
		i.RecoveredAt = time.Now()
		i.logger.Infof("[%s %s] all sources recovered, closing incident", i.Object.DisplayName(), i.String())

		history = &HistoryRow{
			EventID: utils.ToDBInt(ev.ID),
			Time:    types.UnixMilli(i.RecoveredAt),
			Type:    Closed,
		}
		err := RemoveCurrent(i.Object, history)
		if err != nil {
			i.logger.Errorln(err)
			return types.Int{}, err
		}
	}

	return causedByHistoryId, nil
}

// EvaluateRules evaluates all the configured rules for this *incident.Object and
// generates history entries for each matched rule.
// Returns error on database failure.
func (i *Incident) EvaluateRules(eventID int64, causedBy types.Int) (types.Int, error) {
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
					i.logger.Warnf("[%s %s] rule %q failed to evaluate object filter: %s", i.Object.DisplayName(), i.String(), r.Name, err)
				}

				if err != nil || !matched {
					continue
				}
			}

			i.Rules[r.ID] = struct{}{}
			i.logger.Infof("[%s %s] rule %q matches", i.Object.DisplayName(), i.String(), r.Name)

			history := &HistoryRow{
				Time:                      types.UnixMilli(time.Now()),
				EventID:                   utils.ToDBInt(eventID),
				RuleID:                    utils.ToDBInt(r.ID),
				Type:                      RuleMatched,
				CausedByIncidentHistoryID: causedBy,
			}
			insertedID, err := i.AddRuleMatchedHistory(r, history)
			if err != nil {
				i.logger.Errorln(err)

				return types.Int{}, err
			}

			if insertedID.Valid && !causedBy.Valid {
				causedBy = insertedID
			}
		}
	}

	return causedBy, nil
}

// EvaluateEscalations evaluates this incidents rule escalations if they aren't already.
// Returns error on database failure.
func (i *Incident) EvaluateEscalations() {
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
						i.logger.Warnf(
							"[%s %s] rule %q failed to evaulte escalation %q condition: %s",
							i.Object.DisplayName(), i.String(), r.Name, escalation.DisplayName(), err,
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

// NotifyContacts evaluates the incident.EscalationState and checks if escalations need to be triggered
// as well as builds the incident recipients along with their channel types and sends notifications based on that.
// Returns error on database failure.
func (i *Incident) NotifyContacts(ev *event.Event, causedBy types.Int) error {
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
			i.logger.Infof("[%s %s] rule %q reached escalation %q", i.Object.DisplayName(), i.String(), r.Name, escalation.DisplayName())

			history := &HistoryRow{
				Time:                      state.TriggeredAt,
				EventID:                   utils.ToDBInt(ev.ID),
				RuleEscalationID:          utils.ToDBInt(state.RuleEscalationID),
				RuleID:                    utils.ToDBInt(r.ID),
				Type:                      EscalationTriggered,
				CausedByIncidentHistoryID: causedBy,
			}

			causedByHistoryId, err := i.AddEscalationTriggered(state, history)
			if err != nil {
				i.logger.Errorln(err)

				return err
			}

			causedBy = causedByHistoryId

			err = i.AddRecipient(escalation, ev.ID)
			if err != nil {
				i.logger.Errorln(err)

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

		for chType := range channels {
			i.logger.Infof("[%s %s] notify %q via %q", i.Object.DisplayName(), i.String(), contact.FullName, chType)

			hr.ChannelType = utils.ToDBString(chType)

			_, err := i.AddHistory(hr, false)
			if err != nil {
				i.logger.Errorln(err)
			}

			chConf := i.runtimeConfig.Channels[chType]
			if chConf == nil {
				i.logger.Errorf("could not find config for channel type %q", chType)
				continue
			}

			plugin, err := chConf.GetPlugin()
			if err != nil {
				i.logger.Errorw("couldn't initialize channel", zap.String("type", chType), zap.Error(err))
				continue
			}

			err = plugin.Send(contact, i, ev, i.configFile.Icingaweb2URL)
			if err != nil {
				i.logger.Errorw("failed to send via channel", zap.String("type", chType), zap.Error(err))
				continue
			}
		}
	}

	return nil
}

// ProcessAcknowledgementEvent processes the given ack event.
// Promotes the ack author to incident.RoleManager if it's not already the case and generates a history entry.
// Returns error on database failure.
func (i *Incident) ProcessAcknowledgementEvent(ev event.Event) error {
	i.runtimeConfig.RLock()
	defer i.runtimeConfig.RUnlock()

	contact := i.runtimeConfig.GetContact(ev.Username)
	if contact == nil {
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

	i.logger.Infof("[%s %s] contact %q role changed from %s to %s", i.Object.DisplayName(), i.String(), contact.String(), oldRole.String(), newRole.String())

	hr := &HistoryRow{
		Key:              recipientKey,
		EventID:          utils.ToDBInt(ev.ID),
		Type:             RecipientRoleChanged,
		Time:             types.UnixMilli(time.Now()),
		NewRecipientRole: newRole,
		OldRecipientRole: oldRole,
		Message:          utils.ToDBString(ev.Message),
	}

	_, err := i.AddHistory(hr, false)
	if err != nil {
		return err
	}

	cr := &ContactRow{IncidentID: hr.IncidentID, Key: recipientKey, Role: newRole}

	stmt, _ := i.db.BuildUpsertStmt(cr)
	_, err = i.db.NamedExec(stmt, cr)
	if err != nil {
		return fmt.Errorf("failed to upsert incident contact %s: %s", contact.String(), err)
	}

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

var (
	_ contracts.Incident = (*Incident)(nil)
)
