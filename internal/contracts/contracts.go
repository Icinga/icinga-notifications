package contracts

import (
	"fmt"
	"github.com/icinga/icinga-notifications/internal/object"
)

type Incident interface {
	fmt.Stringer

	ID() int64
	IncidentObject() *object.Object
	SeverityString() string
}
