package rule

import (
	"github.com/icinga/icinga-go-library/types"
	"github.com/icinga/icinga-notifications/internal/filter"
	"github.com/icinga/icinga-notifications/internal/timeperiod"
)

type Rule struct {
	ID               int64
	IsActive         types.Bool
	Name             string
	TimePeriod       *timeperiod.TimePeriod `db:"-"`
	TimePeriodID     types.Int              `db:"timeperiod_id"`
	ObjectFilter     filter.Filter          `db:"-"`
	ObjectFilterExpr types.String           `db:"object_filter"`
	Escalations      map[int64]*Escalation  `db:"-"`
}
