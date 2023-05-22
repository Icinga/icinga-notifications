package incident

import (
	"errors"
	"fmt"
	"github.com/icinga/icinga-notifications/internal/config"
	"github.com/icinga/icinga-notifications/internal/contracts"
	"github.com/icinga/icinga-notifications/internal/event"
	"github.com/icinga/icinga-notifications/internal/object"
	"github.com/icinga/icinga-notifications/internal/recipient"
	"github.com/icinga/icinga-notifications/internal/utils"
	"github.com/icinga/icingadb/pkg/icingadb"
	"github.com/icinga/icingadb/pkg/logging"
	"github.com/icinga/icingadb/pkg/types"
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
