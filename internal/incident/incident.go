package incident

import (
	"database/sql"
	"database/sql/driver"
	"fmt"
	"github.com/icinga/icingadb/pkg/icingadb"
	"github.com/icinga/icingadb/pkg/types"
	"github.com/icinga/noma/internal/event"
	"github.com/icinga/noma/internal/object"
	"github.com/icinga/noma/internal/recipient"
	"github.com/icinga/noma/internal/rule"
	"github.com/icinga/noma/internal/utils"
	"log"
	"sync"
	"time"
)

type Incident struct {
	Object           *object.Object
	StartedAt        time.Time
	RecoveredAt      time.Time
	SeverityBySource map[int64]event.Severity

	State      map[*rule.Rule]map[*rule.Escalation]*EscalationState
	Events     []*event.Event
	Recipients map[recipient.Recipient]*RecipientState
	History    []*HistoryEntry

	incidentRowID int64
	db            *icingadb.DB

	sync.Mutex
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

func (i *Incident) AddHistory(history *HistoryEntry, historyRow *HistoryRow) error {
	i.History = append(i.History, history)
	log.Printf("[%s %s] %s", i.Object.DisplayName(), i.String(), history.Message)

	// Set the incident id, message and time if they're not already set!
	historyRow.IncidentID = i.incidentRowID
	historyRow.Message = history.Message
	historyRow.Time = types.UnixMilli(history.Time)

	_, err := i.db.NamedExec(utils.BuildInsertStmtWithout(i.db, historyRow, "id"), historyRow)
	if err != nil {
		return fmt.Errorf("failed to insert incident history: %s\n", err)
	}

	return nil
}

func (i *Incident) AddEscalationTriggeredHistory(state *EscalationState, history *HistoryEntry) error {
	// Set the incident id if it's not set already!
	state.IncidentID = i.incidentRowID

	stmt, _ := i.db.BuildInsertStmt(state)
	_, err := i.db.NamedExec(stmt, state)
	if err != nil {
		return fmt.Errorf("failed to insert incident rule escalation state: %s", err)
	}

	hr := &HistoryRow{
		IncidentID:       i.incidentRowID,
		RuleEscalationID: types.Int{NullInt64: sql.NullInt64{Int64: state.RuleEscalationID, Valid: true}},
		Time:             types.UnixMilli(history.Time),
		Type:             EscalationTriggered,
		Message:          history.Message,
	}

	return i.AddHistory(history, hr)
}

// AddEvent adds the given event to this incident events slice.
// Inserts also incident history record to the database and returns an error on db failure.
func (i *Incident) AddEvent(db *icingadb.DB, ev *event.Event) error {
	//i.Events = append(i.Events, ev)

	ie := &EventRow{IncidentID: i.incidentRowID, EventID: ev.ID}
	stmt, _ := db.BuildInsertStmt(ie)
	_, err := db.NamedExec(stmt, ie)
	if err != nil {
		return fmt.Errorf("failed to insert incident event: %s", err)
	}

	return nil
}

// AddRecipient adds recipient from the given *rule.Escalation to this incident.
// Syncs also all the recipients with the database and returns an error on db failure.
func (i *Incident) AddRecipient(escalation *rule.Escalation, t time.Time) error {
	newRole := RoleRecipient
	if i.HasManager() {
		newRole = RoleSubscriber
	}

	for _, escalationRecipient := range escalation.Recipients {
		r := escalationRecipient.Recipient
		cr := &ContactRow{IncidentID: i.incidentRowID, Role: newRole}
		switch c := r.(type) {
		case *recipient.Contact:
			cr.ContactID = types.Int{NullInt64: sql.NullInt64{Int64: c.ID, Valid: true}}
		case *recipient.Group:
			cr.ContactGroupID = types.Int{NullInt64: sql.NullInt64{Int64: c.ID, Valid: true}}
		case *recipient.Schedule:
			cr.ScheduleID = types.Int{NullInt64: sql.NullInt64{Int64: c.ID, Valid: true}}
		}

		state, ok := i.Recipients[r]
		if !ok {
			i.Recipients[r] = &RecipientState{
				Role:     newRole,
				Channels: map[string]struct{}{escalationRecipient.ChannelType: {}},
			}
		} else {
			if state.Role < newRole {
				oldRole := state.Role
				state.Role = newRole

				history := NewHistoryEntry(t, "contact %q role changed from %s to %s", r.RecipientName(), state.Role.String(), newRole.String())
				hr := &HistoryRow{
					IncidentID:       i.incidentRowID,
					ContactID:        cr.ContactID,
					ContactGroupID:   cr.ContactGroupID,
					ScheduleID:       cr.ScheduleID,
					Type:             RecipientRoleChanged,
					NewRecipientRole: newRole,
					OldRecipientRole: oldRole,
				}

				err := i.AddHistory(history, hr)
				if err != nil {
					return err
				}
			}
			state.Channels[escalationRecipient.ChannelType] = struct{}{}
			cr.Role = state.Role
		}

		stmt, _ := i.db.BuildUpsertStmt(cr)
		_, err := i.db.NamedExec(stmt, cr)
		if err != nil {
			return fmt.Errorf("failed to upsert incident contact %s: %s", r.RecipientName(), err)
		}
	}

	return nil
}

func (i *Incident) String() string {
	return fmt.Sprintf("#%#p", i)
}

// Sync initiates an *incident.IncidentRow from the current incident state and syncs it with the database.
// Before syncing any incident related database entries, this method should be called at least once.
// Returns an error on db failure.
func (i *Incident) Sync(history *HistoryEntry) error {
	if i.incidentRowID != 0 {
		return nil
	}

	incidentRow := &IncidentRow{
		ObjectID:    i.Object.ID,
		StartedAt:   types.UnixMilli(i.StartedAt),
		RecoveredAt: types.UnixMilli(i.RecoveredAt),
		Severity:    i.Severity(),
	}

	err := incidentRow.Sync(i.db)
	if err != nil {
		return err
	}

	i.incidentRowID = incidentRow.ID
	hr := &HistoryRow{
		IncidentID: i.incidentRowID,
		Time:       types.UnixMilli(history.Time),
		Type:       Opened,
		Message:    history.Message,
	}

	return i.AddHistory(history, hr)
}

// AddRuleMatchedHistory syncs the given *rule.Rule and history entry to the database.
// Returns an error on database failure.
func (i *Incident) AddRuleMatchedHistory(r *rule.Rule, history *HistoryEntry) error {
	rr := &RuleRow{IncidentID: i.incidentRowID, RuleID: r.ID}
	stmt, _ := i.db.BuildInsertStmt(rr)
	_, err := i.db.NamedExec(stmt, rr)
	if err != nil {
		return fmt.Errorf("failed to insert incident rule: %s", err)
	}

	hr := &HistoryRow{
		IncidentID: i.incidentRowID,
		RuleID:     types.Int{NullInt64: sql.NullInt64{Int64: r.ID, Valid: true}},
		Time:       types.UnixMilli(history.Time),
		Type:       RuleMatched,
		Message:    history.Message,
	}

	return i.AddHistory(history, hr)
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

type HistoryEntry struct {
	Time    time.Time
	Message string
}

func NewHistoryEntry(t time.Time, m string, args ...any) *HistoryEntry {
	if len(args) > 0 {
		m = fmt.Sprintf(m, args...)
	}

	return &HistoryEntry{Time: t, Message: m}
}

type ContactRole int

const (
	RoleNone ContactRole = iota
	RoleRecipient
	RoleSubscriber
	RoleManager
)

var contactRoleByName = map[string]ContactRole{
	"recipient":  RoleRecipient,
	"subscriber": RoleSubscriber,
	"manager":    RoleManager,
}

// Scan implements the sql.Scanner interface.
func (c *ContactRole) Scan(src any) error {
	if c == nil {
		*c = RoleNone
		return nil
	}

	name, ok := src.(string)
	if !ok {
		return fmt.Errorf("unable to scan type %T into ContactRole", src)
	}

	role, ok := contactRoleByName[name]
	if !ok {
		return fmt.Errorf("unknown contact role %q", name)
	}

	*c = role

	return nil
}

// Value implements the driver.Valuer interface.
func (c ContactRole) Value() (driver.Value, error) {
	val := c.String()
	if val == "" {
		return nil, nil
	}

	return val, nil
}

func (c *ContactRole) String() string {
	for name, role := range contactRoleByName {
		if role == *c {
			return name
		}
	}

	return ""
}

type RecipientState struct {
	Role     ContactRole
	Channels map[string]struct{}
}

func GetCurrent(db *icingadb.DB, obj *object.Object, create bool) (*Incident, bool) {
	currentIncidentsMu.Lock()
	defer currentIncidentsMu.Unlock()

	created := false
	currentIncident := currentIncidents[obj]

	if create && currentIncident == nil {
		created = true
		currentIncident = &Incident{
			Object: obj,
			db:     db,
		}
		currentIncidents[obj] = currentIncident
	}

	return currentIncident, created
}

func RemoveCurrent(obj *object.Object, history *HistoryEntry) error {
	currentIncidentsMu.Lock()
	defer currentIncidentsMu.Unlock()

	currentIncident := currentIncidents[obj]

	if currentIncident != nil {
		delete(currentIncidents, obj)
	}

	incidentRow := &IncidentRow{ID: currentIncident.incidentRowID, RecoveredAt: types.UnixMilli(currentIncident.RecoveredAt)}
	_, err := currentIncident.db.NamedExec(`UPDATE "incident" SET "recovered_at" = :recovered_at WHERE id = :id`, incidentRow)
	if err != nil {
		log.Printf("Failed to upsert current incident: %s", err)
	}

	hr := &HistoryRow{
		IncidentID: incidentRow.ID,
		Time:       types.UnixMilli(history.Time),
		Type:       Closed,
		Message:    history.Message,
	}

	return currentIncident.AddHistory(history, hr)
}

var (
	currentIncidents   = make(map[*object.Object]*Incident)
	currentIncidentsMu sync.Mutex
)
