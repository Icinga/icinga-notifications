package incident

import (
	"database/sql"
	"database/sql/driver"
	"fmt"
	"github.com/icinga/icinga-notifications/internal/event"
	"github.com/icinga/icinga-notifications/internal/object"
	"github.com/icinga/icinga-notifications/internal/recipient"
	"github.com/icinga/icinga-notifications/internal/rule"
	"github.com/icinga/icinga-notifications/internal/utils"
	"github.com/icinga/icingadb/pkg/icingadb"
	"github.com/icinga/icingadb/pkg/types"
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

	EscalationState map[escalationID]*EscalationState
	Rules           map[ruleID]struct{}
	Recipients      map[recipient.Key]*RecipientState

	incidentRowID int64

	db *icingadb.DB

	sync.Mutex
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

func (i *Incident) AddHistory(historyRow *HistoryRow, fetchId bool) (types.Int, error) {
	historyRow.IncidentID = i.incidentRowID

	stmt := utils.BuildInsertStmtWithout(i.db, historyRow, "id")
	if fetchId {
		historyId, err := utils.InsertAndFetchId(i.db, stmt, historyRow)
		if err != nil {
			return types.Int{}, err
		}

		return utils.ToDBInt(historyId), nil
	} else {
		_, err := i.db.NamedExec(stmt, historyRow)
		if err != nil {
			return types.Int{}, fmt.Errorf("failed to insert incident history: %w", err)
		}
	}

	return types.Int{}, nil
}

func (i *Incident) AddEscalationTriggered(state *EscalationState, hr *HistoryRow) (types.Int, error) {
	state.IncidentID = i.incidentRowID

	stmt, _ := i.db.BuildUpsertStmt(state)
	_, err := i.db.NamedExec(stmt, state)
	if err != nil {
		return types.Int{}, fmt.Errorf("failed to insert incident rule escalation state: %w", err)
	}

	return i.AddHistory(hr, true)
}

// AddEvent Inserts incident history record to the database and returns an error on db failure.
func (i *Incident) AddEvent(db *icingadb.DB, ev *event.Event) error {
	ie := &EventRow{IncidentID: i.incidentRowID, EventID: ev.ID}
	stmt, _ := db.BuildInsertStmt(ie)
	_, err := db.NamedExec(stmt, ie)
	if err != nil {
		return fmt.Errorf("failed to insert incident event: %w", err)
	}

	return nil
}

// AddRecipient adds recipient from the given *rule.Escalation to this incident.
// Syncs also all the recipients with the database and returns an error on db failure.
func (i *Incident) AddRecipient(escalation *rule.Escalation, eventId int64) error {
	newRole := RoleRecipient
	if i.HasManager() {
		newRole = RoleSubscriber
	}

	for _, escalationRecipient := range escalation.Recipients {
		r := escalationRecipient.Recipient
		cr := &ContactRow{IncidentID: i.incidentRowID, Role: newRole}

		recipientKey := recipient.ToKey(r)
		cr.Key = recipientKey

		state, ok := i.Recipients[recipientKey]
		if !ok {
			i.Recipients[recipientKey] = &RecipientState{Role: newRole}
		} else {
			if state.Role < newRole {
				oldRole := state.Role
				state.Role = newRole

				log.Printf("[%s %s] contact %q role changed from %s to %s", i.Object.DisplayName(), i.String(), r, state.Role.String(), newRole.String())

				hr := &HistoryRow{
					IncidentID:       i.incidentRowID,
					EventID:          utils.ToDBInt(eventId),
					Key:              cr.Key,
					Time:             types.UnixMilli(time.Now()),
					Type:             RecipientRoleChanged,
					NewRecipientRole: newRole,
					OldRecipientRole: oldRole,
				}

				_, err := i.AddHistory(hr, false)
				if err != nil {
					return err
				}
			}
			cr.Role = state.Role
		}

		stmt, _ := i.db.BuildUpsertStmt(cr)
		_, err := i.db.NamedExec(stmt, cr)
		if err != nil {
			return fmt.Errorf("failed to upsert incident contact %s: %w", r, err)
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
func (i *Incident) AddRuleMatchedHistory(r *rule.Rule, hr *HistoryRow) (types.Int, error) {
	rr := &RuleRow{IncidentID: i.incidentRowID, RuleID: r.ID}
	stmt, _ := i.db.BuildUpsertStmt(rr)
	_, err := i.db.NamedExec(stmt, rr)
	if err != nil {
		return types.Int{}, fmt.Errorf("failed to insert incident rule: %w", err)
	}

	return i.AddHistory(hr, true)
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
	Role ContactRole
}

func GetCurrent(db *icingadb.DB, obj *object.Object, create bool) (*Incident, bool, error) {
	currentIncidentsMu.Lock()
	defer currentIncidentsMu.Unlock()

	created := false
	currentIncident := currentIncidents[obj]

	if currentIncident == nil {
		ir := &IncidentRow{}
		incident := &Incident{Object: obj, db: db}
		err := db.QueryRowx(db.Rebind(db.BuildSelectStmt(ir, ir)+` WHERE "object_id" = ? AND "recovered_at" IS NULL`), obj.ID).StructScan(ir)
		if err != nil && err != sql.ErrNoRows {
			return nil, false, fmt.Errorf("incident query failed with: %w", err)
		} else if err == nil {
			incident.SeverityBySource = make(map[int64]event.Severity)
			incident.EscalationState = make(map[escalationID]*EscalationState)
			incident.Recipients = make(map[recipient.Key]*RecipientState)
			incident.incidentRowID = ir.ID
			incident.StartedAt = ir.StartedAt.Time()

			sourceSeverity := &SourceSeverity{IncidentID: ir.ID}
			var sources []SourceSeverity
			err := db.Select(
				&sources,
				db.Rebind(db.BuildSelectStmt(sourceSeverity, sourceSeverity)+` WHERE "incident_id" = ? AND "severity" != ?`),
				ir.ID, event.SeverityOK,
			)
			if err != nil {
				return nil, false, fmt.Errorf("failed to fetch incident sources Severity: %w", err)
			}

			for _, source := range sources {
				incident.SeverityBySource[source.SourceID] = source.Severity
			}

			state := &EscalationState{}
			var states []*EscalationState
			err = db.Select(&states, db.Rebind(db.BuildSelectStmt(state, state)+` WHERE "incident_id" = ?`), ir.ID)
			if err != nil {
				return nil, false, fmt.Errorf("failed to fetch incident rule escalation state: %w", err)
			}

			for _, state := range states {
				incident.EscalationState[state.RuleEscalationID] = state
			}

			currentIncident = incident
		}

		if create && currentIncident == nil {
			created = true
			currentIncident = incident
		}

		if currentIncident != nil {
			currentIncidents[obj] = currentIncident
		}
	}

	if !created && currentIncident != nil {
		currentIncident.Lock()
		defer currentIncident.Unlock()

		contact := &ContactRow{}
		var contacts []*ContactRow
		err := db.Select(&contacts, db.Rebind(db.BuildSelectStmt(contact, contact)+` WHERE "incident_id" = ?`), currentIncident.ID())
		if err != nil {
			return nil, false, fmt.Errorf("failed to fetch incident recipients: %w", err)
		}

		recipients := make(map[recipient.Key]*RecipientState)
		for _, contact := range contacts {
			recipients[contact.Key] = &RecipientState{Role: contact.Role}
		}

		currentIncident.Recipients = recipients
	}

	return currentIncident, created, nil
}

func RemoveCurrent(obj *object.Object, hr *HistoryRow) error {
	currentIncidentsMu.Lock()
	defer currentIncidentsMu.Unlock()

	currentIncident := currentIncidents[obj]

	if currentIncident != nil {
		delete(currentIncidents, obj)
	}

	incidentRow := &IncidentRow{ID: currentIncident.incidentRowID, RecoveredAt: types.UnixMilli(currentIncident.RecoveredAt)}
	_, err := currentIncident.db.NamedExec(`UPDATE "incident" SET "recovered_at" = :recovered_at WHERE id = :id`, incidentRow)
	if err != nil {
		return fmt.Errorf("failed to update current incident: %w", err)
	}

	_, err = currentIncident.AddHistory(hr, false)

	return nil
}

// GetCurrentIncidents returns a map of all incidents for debugging purposes.
func GetCurrentIncidents() map[int64]*Incident {
	currentIncidentsMu.Lock()
	defer currentIncidentsMu.Unlock()

	m := make(map[int64]*Incident)
	for _, incident := range currentIncidents {
		m[incident.incidentRowID] = incident
	}
	return m
}

var (
	currentIncidents   = make(map[*object.Object]*Incident)
	currentIncidentsMu sync.Mutex
)
