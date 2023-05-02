package rule

import (
	"github.com/icinga/icingadb/pkg/types"
	"github.com/icinga/noma/internal/filter"
	"github.com/icinga/noma/internal/timeperiod"
)

type Rule struct {
	ID               int64      `db:"id"`
	IsActive         types.Bool `db:"is_active"`
	Name             string     `db:"name"`
	TimePeriod       *timeperiod.TimePeriod
	TimePeriodID     types.Int     `db:"timeperiod_id"`
	ObjectFilter     filter.Filter `db:"-"`
	ObjectFilterExpr types.String  `db:"object_filter"`
	Escalations      map[int64]*Escalation
}
