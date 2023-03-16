package rule

import (
	"github.com/icinga/noma/internal/event"
	"time"
)

type Condition struct {
	MinDuration time.Duration
	MinSeverity event.Severity
}
