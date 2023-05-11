package incident

import (
	"fmt"
	"github.com/icinga/icinga-notifications/internal/contracts"
	"github.com/icinga/icinga-notifications/internal/event"
	"github.com/icinga/icinga-notifications/internal/object"
	"github.com/icinga/icinga-notifications/internal/recipient"
	"github.com/icinga/icingadb/pkg/icingadb"
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

	db *icingadb.DB

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
