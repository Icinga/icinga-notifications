package incident

import (
	"github.com/icinga/icingadb/pkg/icingadb"
	"github.com/icinga/icingadb/pkg/types"
	"github.com/icinga/noma/internal/event"
	"github.com/icinga/noma/internal/utils"
)

type IncidentRow struct {
	ID          int64           `db:"id"`
	ObjectID    types.Binary    `db:"object_id"`
	StartedAt   types.UnixMilli `db:"started_at"`
	RecoveredAt types.UnixMilli `db:"recovered_at"`
	Severity    event.Severity  `db:"severity"`
}

// TableName implements the contracts.TableNamer interface.
func (i *IncidentRow) TableName() string {
	return "incident"
}

// Sync synchronizes incidents to the database.
// Fetches the last inserted incident id and modifies this incident's id.
// Returns an error on database failure.
func (i *IncidentRow) Sync(db *icingadb.DB) error {
	incidentId, err := utils.InsertAndFetchId(db, utils.BuildInsertStmtWithout(db, i, "id"), i)
	if err != nil {
		return err
	}

	i.ID = incidentId

	return nil
}

// EventRow represents a single incident event database entry.
type EventRow struct {
	IncidentID int64 `db:"incident_id"`
	EventID    int64 `db:"event_id"`
}

// TableName implements the contracts.TableNamer interface.
func (e *EventRow) TableName() string {
	return "incident_event"
}

// ContactRow represents a single incident contact database entry.
type ContactRow struct {
	IncidentID     int64       `db:"incident_id"`
	ContactID      types.Int   `db:"contact_id"`
	ContactGroupID types.Int   `db:"contactgroup_id"`
	ScheduleID     types.Int   `db:"schedule_id"`
	Role           ContactRole `db:"role"`
}

// TableName implements the contracts.TableNamer interface.
func (c *ContactRow) TableName() string {
	return "incident_contact"
}

// Upsert implements the contracts.Upserter interface.
func (c *ContactRow) Upsert() interface{} {
	return &struct {
		Role ContactRole `db:"role"`
	}{Role: c.Role}
}

// Constraint implements the contracts.Constrainter interface.
func (c *ContactRow) Constraint() string {
	if c.ContactID.Valid {
		return "key_incident_contact_contact"
	}

	if c.ContactGroupID.Valid {
		return "key_incident_contact_contactgroup"
	}

	return "key_incident_contact_schedule"
}

// RuleRow represents a single incident rule database entry.
type RuleRow struct {
	IncidentID int64 `db:"incident_id"`
	RuleID     int64 `db:"rule_id"`
}

// TableName implements the contracts.TableNamer interface.
func (r *RuleRow) TableName() string {
	return "incident_rule"
}

// HistoryRow represents a single incident history database entry.
type HistoryRow struct {
	ID               int64            `db:"id"`
	IncidentID       int64            `db:"incident_id"`
	RuleEscalationID types.Int        `db:"rule_escalation_id"`
	EventID          types.Int        `db:"event_id"`
	ContactID        types.Int        `db:"contact_id"`
	ContactGroupID   types.Int        `db:"contactgroup_id"`
	ScheduleID       types.Int        `db:"schedule_id"`
	RuleID           types.Int        `db:"rule_id"`
	Time             types.UnixMilli  `db:"time"`
	Type             HistoryEventType `db:"type"`
	ChannelType      types.String     `db:"channel_type"`
	NewSeverity      event.Severity   `db:"new_severity"`
	OldSeverity      event.Severity   `db:"old_severity"`
	NewRecipientRole ContactRole      `db:"new_recipient_role"`
	OldRecipientRole ContactRole      `db:"old_recipient_role"`
	Message          string           `db:"message"`
}

// TableName implements the contracts.TableNamer interface.
func (h *HistoryRow) TableName() string {
	return "incident_history"
}
