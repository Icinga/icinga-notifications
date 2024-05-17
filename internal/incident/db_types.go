package incident

import (
	"context"
	"github.com/icinga/icinga-go-library/database"
	"github.com/icinga/icinga-go-library/notifications/event"
	"github.com/icinga/icinga-go-library/types"
	"github.com/icinga/icinga-notifications/internal/recipient"
	"github.com/jmoiron/sqlx"
)

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
	IncidentID    int64 `db:"incident_id"`
	recipient.Key `db:",inline"`
	Role          ContactRole `db:"role"`
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

// PgsqlOnConflictConstraint implements the database.PgsqlOnConflictConstrainter interface.
func (c *ContactRow) PgsqlOnConflictConstraint() string {
	if c.ContactID.Valid {
		return "uk_incident_contact_incident_id_contact_id"
	}

	if c.GroupID.Valid {
		return "uk_incident_contact_incident_id_contactgroup_id"
	}

	return "uk_incident_contact_incident_id_schedule_id"
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
	ID                    int64     `db:"id"`
	IncidentID            int64     `db:"incident_id"`
	RuleEntryID           types.Int `db:"rule_entry_id"`
	EventID               types.Int `db:"event_id"`
	recipient.Key         `db:",inline"`
	RuleID                types.Int        `db:"rule_id"`
	NotificationHistoryID types.Int        `db:"notification_history_id"`
	Time                  types.UnixMilli  `db:"time"`
	Type                  HistoryEventType `db:"type"`
	NewSeverity           event.Severity   `db:"new_severity"`
	OldSeverity           event.Severity   `db:"old_severity"`
	NewRecipientRole      ContactRole      `db:"new_recipient_role"`
	OldRecipientRole      ContactRole      `db:"old_recipient_role"`
	Message               types.String     `db:"message"` // Is only used to store Incident (un)mute reason.
}

// TableName implements the contracts.TableNamer interface.
func (h *HistoryRow) TableName() string {
	return "incident_history"
}

// Sync persists the current state of this history to the database and retrieves the just inserted history ID.
// Returns error when failed to execute the query.
func (h *HistoryRow) Sync(ctx context.Context, db *database.DB, tx *sqlx.Tx) error {
	historyId, err := database.InsertObtainID(ctx, tx, database.BuildInsertStmtWithout(db, h, "id"), h)
	if err != nil {
		return err
	}

	h.ID = historyId

	return nil
}
