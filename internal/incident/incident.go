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

type ruleID = int64
type escalationID = int64

type Incident struct {
	Object           *object.Object
	StartedAt        time.Time
	RecoveredAt      time.Time
	SeverityBySource map[int64]event.Severity

	State      map[ruleID]map[escalationID]*EscalationState
	Events     []*event.Event
	Recipients map[RecipientKey]*RecipientState
	History    []*HistoryEntry

	incidentRowID int64

	db *icingadb.DB

	sync.Mutex
}

type RecipientKey struct {
	// Only one of the members is allowed to be a non-zero value.
	ContactID  int64
	GroupID    int64
	ScheduleID int64
}

func RecipientToKey(r recipient.Recipient) RecipientKey {
	switch v := r.(type) {
	case *recipient.Contact:
		return RecipientKey{ContactID: v.ID}
	case *recipient.Group:
		return RecipientKey{GroupID: v.ID}
	case *recipient.Schedule:
		return RecipientKey{ScheduleID: v.ID}
	default:
		panic(fmt.Sprintf("unexpected recipient type: %T", r))
	}
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

func (i *Incident) AddHistory(history *HistoryEntry, historyRow *HistoryRow, fetchId bool) (types.Int, error) {
	i.History = append(i.History, history)

	historyRow.IncidentID = i.incidentRowID
	historyRow.Message = utils.ToDBString(history.Message)
	historyRow.Time = types.UnixMilli(history.Time)
	historyRow.EventID = types.Int{NullInt64: sql.NullInt64{Int64: history.EventRowID, Valid: true}}

	stmt := utils.BuildInsertStmtWithout(i.db, historyRow, "id")
	if fetchId {
		historyId, err := utils.InsertAndFetchId(i.db, stmt, historyRow)
		if err != nil {
			return types.Int{}, err
		}

		return types.Int{NullInt64: sql.NullInt64{Int64: historyId, Valid: true}}, nil
	} else {
		_, err := i.db.NamedExec(stmt, historyRow)
		if err != nil {
			return types.Int{}, fmt.Errorf("failed to insert incident history: %s\n", err)
		}
	}

	return types.Int{}, nil
}

func (i *Incident) AddEscalationTriggered(state *EscalationState, history *HistoryEntry) (types.Int, error) {
	state.IncidentID = i.incidentRowID

	stmt, _ := i.db.BuildUpsertStmt(state)
	_, err := i.db.NamedExec(stmt, state)
	if err != nil {
		return types.Int{}, fmt.Errorf("failed to insert incident rule escalation state: %s", err)
	}

	hr := &HistoryRow{
		RuleEscalationID:          types.Int{NullInt64: sql.NullInt64{Int64: state.RuleEscalationID, Valid: true}},
		Type:                      EscalationTriggered,
		CausedByIncidentHistoryID: history.CausedByIncidentHistoryId,
	}

	return i.AddHistory(history, hr, true)
}

// AddEvent adds the given event to this incident events slice.
// Inserts also incident history record to the database and returns an error on db failure.
func (i *Incident) AddEvent(db *icingadb.DB, ev *event.Event) error {
	i.Events = append(i.Events, ev)

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
func (i *Incident) AddRecipient(escalation *rule.Escalation, t time.Time, eventId int64) error {
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

		recipientKey := RecipientToKey(r)
		state, ok := i.Recipients[recipientKey]
		if !ok {
			i.Recipients[recipientKey] = &RecipientState{
				Role:     newRole,
				Channels: map[string]struct{}{escalationRecipient.ChannelType: {}},
			}
		} else {
			if state.Role < newRole {
				oldRole := state.Role
				state.Role = newRole

				log.Printf("[%s %s] contact %q role changed from %s to %s", i.Object.DisplayName(), i.String(), r, state.Role.String(), newRole.String())

				hr := &HistoryRow{
					IncidentID:       i.incidentRowID,
					ContactID:        cr.ContactID,
					ContactGroupID:   cr.ContactGroupID,
					ScheduleID:       cr.ScheduleID,
					Type:             RecipientRoleChanged,
					NewRecipientRole: newRole,
					OldRecipientRole: oldRole,
				}

				_, err := i.AddHistory(&HistoryEntry{Time: t, EventRowID: eventId}, hr, false)
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
			return fmt.Errorf("failed to upsert incident contact %s: %s", r, err)
		}
	}

	return nil
}

func (i *Incident) String() string {
	return fmt.Sprintf("%d", i.incidentRowID)
}

// Sync initiates an *incident.IncidentRow from the current incident state and syncs it with the database.
// Before syncing any incident related database entries, this method should be called at least once.
// Returns an error on db failure.
func (i *Incident) Sync() error {
	incidentRow := &IncidentRow{
		ID:          i.incidentRowID,
		ObjectID:    i.Object.ID,
		StartedAt:   types.UnixMilli(i.StartedAt),
		RecoveredAt: types.UnixMilli(i.RecoveredAt),
		Severity:    i.Severity(),
	}

	err := incidentRow.Sync(i.db, i.incidentRowID != 0)
	if err != nil {
		return err
	}

	i.incidentRowID = incidentRow.ID

	return nil
}

// AddRuleMatchedHistory syncs the given *rule.Rule and history entry to the database.
// Returns an error on database failure.
func (i *Incident) AddRuleMatchedHistory(r *rule.Rule, history *HistoryEntry) (types.Int, error) {
	rr := &RuleRow{IncidentID: i.incidentRowID, RuleID: r.ID}
	stmt, _ := i.db.BuildUpsertStmt(rr)
	_, err := i.db.NamedExec(stmt, rr)
	if err != nil {
		return types.Int{}, fmt.Errorf("failed to insert incident rule: %s", err)
	}

	hr := &HistoryRow{
		RuleID:                    types.Int{NullInt64: sql.NullInt64{Int64: r.ID, Valid: true}},
		Type:                      RuleMatched,
		CausedByIncidentHistoryID: history.CausedByIncidentHistoryId,
	}

	return i.AddHistory(history, hr, true)
}

func (i *Incident) AddSourceSeverity(severity event.Severity, sourceID int64) error {
	i.SeverityBySource[sourceID] = severity

	sourceSeverity := &SourceSeverity{
		IncidentID: i.incidentRowID,
		SourceID:   sourceID,
		Severity:   severity,
	}

	stmt, _ := i.db.BuildUpsertStmt(sourceSeverity)
	_, err := i.db.NamedExec(stmt, sourceSeverity)

	return err
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
	Time                      time.Time
	Message                   string
	CausedByIncidentHistoryId types.Int
	EventRowID                int64
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

var contactRoleToName = func() map[ContactRole]string {
	cr := make(map[ContactRole]string)
	for name, role := range contactRoleByName {
		cr[role] = name
	}
	return cr
}()

// Scan implements the sql.Scanner interface.
func (c *ContactRole) Scan(src any) error {
	if c == nil {
		*c = RoleNone
		return nil
	}

	var name string
	switch val := src.(type) {
	case string:
		name = val
	case []byte:
		name = string(val)
	default:
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
	if c == RoleNone {
		return nil, nil
	}

	return c.String(), nil
}

func (c *ContactRole) String() string {
	return contactRoleToName[*c]
}

type RecipientState struct {
	Role     ContactRole
	Channels map[string]struct{}
}

func GetCurrent(db *icingadb.DB, obj *object.Object, source int64, create bool) (*Incident, bool, error) {
	currentIncidentsMu.Lock()
	defer currentIncidentsMu.Unlock()

	created := false
	currentIncident := currentIncidents[obj]

	if create && currentIncident == nil {
		ir := &IncidentRow{}
		currentIncident = &Incident{Object: obj, db: db}
		err := db.QueryRowx(db.Rebind(db.BuildSelectStmt(ir, ir)+` WHERE "object_id" = ? AND "recovered_at" IS NULL`), obj.ID).StructScan(ir)
		if err != nil {
			created = true

			if err != sql.ErrNoRows {
				return nil, false, fmt.Errorf("incident query failed with: %s", err)
			}
		} else {
			currentIncident.SeverityBySource = make(map[int64]event.Severity)
			currentIncident.incidentRowID = ir.ID
			currentIncident.StartedAt = ir.StartedAt.Time()

			sourceSeverity := &SourceSeverity{IncidentID: ir.ID}
			var sources []SourceSeverity
			err := db.Select(
				&sources,
				db.Rebind(db.BuildSelectStmt(sourceSeverity, sourceSeverity)+` WHERE "incident_id" = ? AND "severity" != ?`),
				ir.ID, event.SeverityOK,
			)
			if err != nil {
				return nil, false, fmt.Errorf("failed to fetch incident sources Severity: %s", err)
			}

			for _, source := range sources {
				currentIncident.SeverityBySource[sourceSeverity.SourceID] = source.Severity
			}
		}

		currentIncidents[obj] = currentIncident
	}

	return currentIncident, created, nil
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
		return fmt.Errorf("failed to update current incident: %s", err)
	}

	_, err = currentIncident.AddHistory(history, &HistoryRow{Type: Closed}, false)
	if err != nil {
		return fmt.Errorf("failed to add incident closed history: %s", err)
	}

	return nil
}

var (
	currentIncidents   = make(map[*object.Object]*Incident)
	currentIncidentsMu sync.Mutex
)
