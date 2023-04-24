package rule

import (
	"database/sql"
	"github.com/icinga/icingadb/pkg/types"
	"github.com/icinga/noma/internal/object"
	"github.com/icinga/noma/internal/timeperiod"
)

type Rule struct {
	ID               int64      `db:"id"`
	IsActive         types.Bool `db:"is_active"`
	Name             string     `db:"name"`
	TimePeriod       *timeperiod.TimePeriod
	TimePeriodID     sql.NullInt64  `db:"timeperiod_id"`
	ObjectFilter     *object.Filter `db:"-"`
	ObjectFilterExpr types.String   `db:"object_filter"`
	Escalations      []*Escalation
}
