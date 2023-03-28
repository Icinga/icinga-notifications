package incident

import (
	"fmt"
	"github.com/icinga/noma/internal/event"
	"github.com/icinga/noma/internal/object"
	"github.com/icinga/noma/internal/recipient"
	"github.com/icinga/noma/internal/rule"
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

func (i *Incident) AddHistory(t time.Time, m string, args ...any) {
	if len(args) > 0 {
		m = fmt.Sprintf(m, args...)
	}
	log.Printf("[%s|%p] %s", i.Object.DisplayName(), i, m)
	i.History = append(i.History, &HistoryEntry{Time: t, Message: m})
}

type EscalationState struct {
	TriggeredAt time.Time
}

type HistoryEntry struct {
	Time    time.Time
	Message string
}

type ContactRole int

const (
	RoleRecipient ContactRole = iota
	RoleSubscriber
	RoleManager
)

type RecipientState struct {
	Role     ContactRole
	Channels map[string]struct{}
}

func GetCurrent(obj *object.Object, create bool) (*Incident, bool) {
	currentIncidentsMu.Lock()
	defer currentIncidentsMu.Unlock()

	created := false
	currentIncident := currentIncidents[obj]

	if create && currentIncident == nil {
		created = true
		currentIncident = &Incident{
			Object: obj,
		}
		currentIncidents[obj] = currentIncident
	}

	return currentIncident, created
}

func RemoveCurrent(obj *object.Object) *Incident {
	currentIncidentsMu.Lock()
	defer currentIncidentsMu.Unlock()

	currentIncident := currentIncidents[obj]

	if currentIncident != nil {
		delete(currentIncidents, obj)
	}

	return currentIncident
}

var (
	currentIncidents   = make(map[*object.Object]*Incident)
	currentIncidentsMu sync.Mutex
)
