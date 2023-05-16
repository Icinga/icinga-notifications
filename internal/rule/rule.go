package rule

import (
	"github.com/icinga/icinga-notifications/internal/filter"
	"github.com/icinga/icinga-notifications/internal/timeperiod"
	"github.com/icinga/icingadb/pkg/types"
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
