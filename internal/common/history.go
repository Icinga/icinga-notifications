package common

import (
	"context"
	"database/sql/driver"
	"fmt"
	"github.com/icinga/icinga-notifications/internal/event"
	"github.com/icinga/icinga-notifications/internal/recipient"
	"github.com/icinga/icinga-notifications/internal/utils"
	"github.com/icinga/icingadb/pkg/icingadb"
	"github.com/icinga/icingadb/pkg/types"
	"github.com/jmoiron/sqlx"
)

type HistoryEventType int

const (
	HistoryEventTypeNull HistoryEventType = iota
	SeverityChanged
	RecipientRoleChanged
	EscalationTriggered
	RuleMatched
	Opened
	Closed
	Notified
	DowntimeStarted
	DowntimeEnded
	DowntimeCancelled
	Custom
	FlappingStarted
	FlappingEnded
	CommentAdded
	CommentRemoved
)

var historyTypeByName = map[string]HistoryEventType{
	"incident_severity_changed": SeverityChanged,
	"recipient_role_changed":    RecipientRoleChanged,
	"escalation_triggered":      EscalationTriggered,
	"rule_matched":              RuleMatched,
	"opened":                    Opened,
	"closed":                    Closed,
	"notified":                  Notified,
	"downtime_started":          DowntimeStarted,
	"downtime_ended":            DowntimeEnded,
	"downtime_cancelled":        DowntimeCancelled,
	"custom":                    Custom,
	"flapping_started":          FlappingStarted,
	"flapping_ended":            FlappingEnded,
	"comment_added":             CommentAdded,
	"comment_removed":           CommentRemoved,
}

var historyEventTypeToName = func() map[HistoryEventType]string {
	eventTypes := make(map[HistoryEventType]string)
	for name, eventType := range historyTypeByName {
		eventTypes[eventType] = name
	}
	return eventTypes
}()

// Scan implements the sql.Scanner interface.
// Supports SQL NULL.
func (h *HistoryEventType) Scan(src any) error {
	if src == nil {
		*h = HistoryEventTypeNull
		return nil
	}

	var name string
	switch val := src.(type) {
	case string:
		name = val
	case []byte:
		name = string(val)
	default:
		return fmt.Errorf("unable to scan type %T into HistoryEventType", src)
	}

	historyType, ok := historyTypeByName[name]
	if !ok {
		return fmt.Errorf("unknown history event type %q", name)
	}

	*h = historyType

	return nil
}

func (h HistoryEventType) Value() (driver.Value, error) {
	if h == HistoryEventTypeNull {
		return nil, nil
	}

	return h.String(), nil
}

func (h *HistoryEventType) String() string {
	return historyEventTypeToName[*h]
}

func GetHistoryEventType(eventType string) (HistoryEventType, error) {
	var historyEvType HistoryEventType
	switch eventType {
	case event.TypeDowntimeStart:
		historyEvType = DowntimeStarted
	case event.TypeDowntimeEnd:
		historyEvType = DowntimeEnded
	case event.TypeDowntimeCancelled:
		historyEvType = DowntimeCancelled
	case event.TypeFlappingStart:
		historyEvType = FlappingStarted
	case event.TypeFlappingEnd:
		historyEvType = FlappingEnded
	case event.TypeCustom:
		historyEvType = Custom
	case event.TypeCommentAdded:
		historyEvType = CommentAdded
	default:
		//TODO: other events
		return historyEvType, fmt.Errorf("type %s not implemented yet", eventType)
	}

	return historyEvType, nil
}

// HistoryRow represents a single history database entry.
type HistoryRow struct {
	ID                       int64        `db:"id"`
	ObjectID                 types.Binary `db:"object_id"`
	IncidentID               types.Int    `db:"incident_id"`
	RuleEscalationID         types.Int    `db:"rule_escalation_id"`
	RuleNonStateEscalationID types.Int    `db:"rule_non_state_escalation_id"`
	EventID                  types.Int    `db:"event_id"`
	recipient.Key            `db:",inline"`
	RuleID                   types.Int         `db:"rule_id"`
	CausedByHistoryID        types.Int         `db:"caused_by_history_id"`
	Time                     types.UnixMilli   `db:"time"`
	Type                     HistoryEventType  `db:"type"`
	ChannelID                types.Int         `db:"channel_id"`
	NewSeverity              event.Severity    `db:"new_severity"`
	OldSeverity              event.Severity    `db:"old_severity"`
	NewRecipientRole         ContactRole       `db:"new_recipient_role"`
	OldRecipientRole         ContactRole       `db:"old_recipient_role"`
	Message                  types.String      `db:"message"`
	NotificationState        NotificationState `db:"notification_state"`
	SentAt                   types.UnixMilli   `db:"sent_at"`
}

// TableName implements the contracts.TableNamer interface.
func (h *HistoryRow) TableName() string {
	return "history"
}

func AddHistory(db *icingadb.DB, ctx context.Context, tx *sqlx.Tx, historyRow *HistoryRow, fetchId bool) (types.Int, error) {
	stmt := utils.BuildInsertStmtWithout(db, historyRow, "id")
	if fetchId {
		historyId, err := utils.InsertAndFetchId(ctx, tx, stmt, historyRow)
		if err != nil {
			return types.Int{}, err
		}

		return utils.ToDBInt(historyId), nil
	} else {
		_, err := tx.NamedExecContext(ctx, stmt, historyRow)
		if err != nil {
			return types.Int{}, err
		}
	}

	return types.Int{}, nil
}
