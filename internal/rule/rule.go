package rule

import (
	"github.com/icinga/noma/internal/object"
	"github.com/icinga/noma/internal/timeperiod"
)

type Rule struct {
	Name         string
	TimePeriod   *timeperiod.TimePeriod
	ObjectFilter *object.Filter
	Escalations  []*Escalation
}
