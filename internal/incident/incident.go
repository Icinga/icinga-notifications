package incident

import (
	"context"
	"errors"
	"fmt"
	"github.com/icinga/icinga-notifications/internal/common"
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
	Object      *object.Object
	StartedAt   time.Time
	RecoveredAt time.Time
	Severity    event.Severity

	EscalationState map[escalationID]*EscalationState
	Rules           map[ruleID]struct{}
	Recipients      map[recipient.Key]*RecipientState

	incidentRowID int64

	// set true if the incident is just created
	IsNew bool

	db            *icingadb.DB
	logger        *zap.SugaredLogger
	runtimeConfig *config.RuntimeConfig

	sync.Mutex //TODO: remove from DumpIncindents()
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
		if state.Role == common.RoleManager {
			return true
		}
	}

	return false
}

// IsNotifiable returns whether contacts in the given role should be notified about this incident.
//
// For a managed incident, only managers and subscribers should be notified, for unmanaged incidents,
// regular recipients are notified as well.
func (i *Incident) IsNotifiable(role common.ContactRole) bool {
	if !i.HasManager() {
		return true
	}

	return role > common.RoleRecipient
}

func (i *Incident) ProcessEvent(ctx context.Context, tx *sqlx.Tx, ev *event.Event) (types.Int, error) {
	var err error
	if i.IsNew {
		err = i.processIncidentOpenedEvent(ctx, tx, ev)
		if err != nil {
			return types.Int{}, err
		}

		i.logger = i.logger.With(zap.String("incident", i.String()))
	}

	if err = i.AddEvent(ctx, tx, ev); err != nil {
		i.logger.Errorw("Can't insert incident event to the database", zap.Error(err))

		return types.Int{}, errors.New("can't insert incident event to the database")
	}

	switch {
	case ev.Type != event.TypeState:
		return types.Int{}, i.processNonStateTypeEvent(ctx, tx, ev)
	case !i.IsNew:
		return i.processSeverityChangedEvent(ctx, tx, ev)
	default:
		return types.Int{}, nil //TODO:
	}
}

func (i *Incident) HandleRuleMatched(ctx context.Context, tx *sqlx.Tx, r *rule.Rule, eventID int64, causedBy types.Int) (types.Int, error) {
	i.Rules[r.ID] = struct{}{}
	err := i.AddRuleMatched(ctx, tx, r)

	if err != nil {
		i.logger.Errorw("Failed to upsert incident rule", zap.String("rule", r.Name), zap.Error(err))

		return types.Int{}, errors.New("failed to insert incident rule")
	}

	history := &HistoryRow{
		Time:              types.UnixMilli(time.Now()),
		EventID:           utils.ToDBInt(eventID),
		RuleID:            utils.ToDBInt(r.ID),
		Type:              RuleMatched,
		CausedByHistoryID: causedBy,
	}

	insertedID, err := i.AddHistory(ctx, tx, history, true)
	if err != nil {
		i.logger.Errorw("Failed to insert rule matched incident history", zap.String("rule", r.Name), zap.Error(err))

		return types.Int{}, errors.New("failed to insert rule matched incident history")
	}

	if insertedID.Valid && !causedBy.Valid {
		causedBy = insertedID
	}

	return causedBy, nil
}

func (i *Incident) Logger() *zap.SugaredLogger {
	return i.logger
}

func (i *Incident) processSeverityChangedEvent(ctx context.Context, tx *sqlx.Tx, ev *event.Event) (types.Int, error) {
	var causedByHistoryId types.Int
	oldSeverity := i.Severity
	newSeverity := ev.Severity
	if oldSeverity == newSeverity {
		err := fmt.Errorf("%w: %s state event from source %d", ErrSuperfluousStateChange, ev.Severity.String(), ev.SourceId)
		return causedByHistoryId, err
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

// TriggerEscalation triggers the given escalations and generates incident history items for each of them.
// Returns an error on database failure.
func (i *Incident) TriggerEscalation(ctx context.Context, tx *sqlx.Tx, ev *event.Event, causedBy types.Int, escalation *rule.EscalationTemplate, r *rule.Rule) error {
	i.logger.Infof("Rule %q reached escalation %q", r.Name, escalation.DisplayName())
	history := &HistoryRow{
		Time:              types.UnixMilli(time.Now()),
		EventID:           utils.ToDBInt(ev.ID),
		RuleID:            utils.ToDBInt(r.ID),
		Type:              EscalationTriggered,
		CausedByHistoryID: causedBy,
	}

	if ev.Type == event.TypeState {
		history.RuleEscalationID = utils.ToDBInt(escalation.ID)

		state := &EscalationState{RuleEscalationID: escalation.ID, TriggeredAt: history.Time}
		i.EscalationState[escalation.ID] = state

		if err := i.AddEscalationTriggered(ctx, tx, state); err != nil {
			i.logger.Errorw(
				"Failed to upsert escalation state", zap.String("rule", r.Name),
				zap.String("escalation", escalation.DisplayName()), zap.Error(err),
			)

			return errors.New("failed to upsert escalation state")
		}
	} else {
		history.RuleNonStateEscalationID = utils.ToDBInt(escalation.ID)
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

	return nil
}

func (i *Incident) processNonStateTypeEvent(ctx context.Context, tx *sqlx.Tx, ev *event.Event) error {
	if ev.Type == event.TypeAcknowledgement {
		if err := i.processAcknowledgementEvent(ctx, tx, ev); err != nil {
			return err
		}

		if err := tx.Commit(); err != nil {
			i.logger.Errorw("Can't commit db transaction", zap.Error(err))

			return errors.New("can't commit db transaction")
		}
	}

	historyEvType, err := GetHistoryEventType(ev.Type)
	if err != nil {
		return err
	}

	hr := &HistoryRow{
		EventID: utils.ToDBInt(ev.ID),
		Time:    types.UnixMilli(time.Now()),
		Type:    historyEvType,
		Message: utils.ToDBString(ev.Message),
	}

	_, err = i.AddHistory(ctx, tx, hr, false)
	if err != nil {
		i.logger.Errorw("Failed to add incident history", zap.String("type", historyEvType.String()), zap.Error(err))

		return fmt.Errorf("failed to add %s incident history", historyEvType.String())
	}

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
	oldRole := common.RoleNone
	newRole := common.RoleManager
	if state != nil {
		oldRole = state.Role

		if oldRole == common.RoleManager {
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

	cr := &ContactRow{IncidentID: hr.IncidentID.Int64, Key: recipientKey, Role: newRole}

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
	Role common.ContactRole
}

// ContactChannels stores a set of channel IDs for each set of individual contacts.
type ContactChannels map[*recipient.Contact]map[int64]bool

func (i *Incident) GetRecipientsChannel(evTime time.Time) ContactChannels {
	contactChs := make(ContactChannels)
	// Load all escalations recipients channels
	for escalationID := range i.EscalationState {
		escalation := i.runtimeConfig.GetRuleEscalation(escalationID)
		if escalation == nil {
			continue
		}

		contactChs.LoadEscalationRecipientsChannel(escalation.Recipients, i, evTime)
	}

	return contactChs
}

// LoadEscalationRecipientsChannel loads all the configured channel of all the provided escalation recipients.
func (rct ContactChannels) LoadEscalationRecipientsChannel(escRecipients []*rule.EscalationRecipient, i *Incident, t time.Time) {
	for _, escalationRecipient := range escRecipients {
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
