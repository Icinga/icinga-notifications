package incident

import (
	"github.com/icinga/icinga-notifications/internal/event"
	"github.com/icinga/icinga-notifications/internal/recipient"
	"github.com/icinga/icingadb/pkg/types"
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

// Constraint implements the contracts.Constrainter interface.
func (c *ContactRow) Constraint() string {
	if c.ContactID.Valid {
		return "key_incident_contact_contact"
	}

	if c.GroupID.Valid {
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
	ID                        int64     `db:"id"`
	IncidentID                int64     `db:"incident_id"`
	RuleEscalationID          types.Int `db:"rule_escalation_id"`
	EventID                   types.Int `db:"event_id"`
	recipient.Key             `db:",inline"`
	RuleID                    types.Int         `db:"rule_id"`
	CausedByIncidentHistoryID types.Int         `db:"caused_by_incident_history_id"`
	Time                      types.UnixMilli   `db:"time"`
	Type                      HistoryEventType  `db:"type"`
	ChannelID                 types.Int         `db:"channel_id"`
	NewSeverity               event.Severity    `db:"new_severity"`
	OldSeverity               event.Severity    `db:"old_severity"`
	NewRecipientRole          ContactRole       `db:"new_recipient_role"`
	OldRecipientRole          ContactRole       `db:"old_recipient_role"`
	Message                   types.String      `db:"message"`
	NotificationState         NotificationState `db:"notification_state"`
	SentAt                    types.UnixMilli   `db:"sent_at"`
}

// TableName implements the contracts.TableNamer interface.
func (h *HistoryRow) TableName() string {
	return "incident_history"
}

// NotificationEntry is used to cache a set of incident history fields of type Notified.
//
// The event processing workflow is performed in a separate transaction before trying to send the actual
// notifications. Thus, all resulting notification entries are marked as pending, and it creates a reference
// to them of this type. The cached entries are then used to actually notify the contacts and mark the pending
// notification entries as either NotificationStateSent or NotificationStateFailed.
type NotificationEntry struct {
	HistoryRowID int64 `db:"id"`
	ContactID    int64
	ChannelID    int64
	State        NotificationState `db:"notification_state"`
	SentAt       types.UnixMilli   `db:"sent_at"`
}

// TableName implements the contracts.TableNamer interface.
func (h *NotificationEntry) TableName() string {
	return "incident_history"
}
