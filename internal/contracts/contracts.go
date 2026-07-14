package contracts

import (
	"fmt"
	"github.com/icinga/icinga-go-library/notifications/event"
)

type Incident interface {
	fmt.Stringer

	ID() int64
	IncidentSeverity() event.Severity
}
