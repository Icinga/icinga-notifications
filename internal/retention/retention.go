package retention

import (
	"context"
	"time"

	"github.com/icinga/icinga-go-library/database"
	"github.com/icinga/icinga-go-library/logging"
	"github.com/icinga/icinga-go-library/periodic"
	"github.com/icinga/icinga-go-library/types"
	"github.com/icinga/icinga-notifications/internal/daemon"
	"go.uber.org/zap"
)

// Retention is responsible for periodically pruning old data from the database.
//
// It uses a list of [TimeBoundPruner] configurations to determine which tables to prune and how to maintain
// referential integrity with related tables. The retention process runs in a separate goroutine for each pruner,
// allowing for concurrent pruning of multiple tables without blocking one another. The retention intervals and
// limits are configurable, and the process can be gracefully stopped using the provided context.
type Retention struct {
	db     *database.DB
	logger *logging.Logger
}

// New creates a new instance of the Retention struct with the provided database connection and logger.
func New(db *database.DB, logger *logging.Logger) *Retention {
	return &Retention{db: db, logger: logger}
}

// Run starts the retention process, which periodically prunes old data from the database.
//
// For each configured pruner, it sets up a periodic task that executes the pruning logic at the specified intervals.
func (r *Retention) Run(ctx context.Context) error {
	conf := daemon.Config().Retention

	var errs chan error
	for _, pruner := range dbPruners {
		period := conf.Period
		interval := conf.Interval

		if pruner.Table == "incident" && conf.Options.Incident != nil {
			period = *conf.Options.Incident
		}

		if pruner.OverridePeriodAndInterval != 0 {
			period = pruner.OverridePeriodAndInterval
			interval = pruner.OverridePeriodAndInterval
		}

		if period == 0 {
			r.logger.Debugf("Skipping retention for table %s because retention period is set to 0", pruner.Table)
			continue
		}

		if errs == nil {
			errs = make(chan error, 1)
		}

		r.logger.Debugw("Starting retention",
			zap.String("table", pruner.Table),
			zap.Duration("interval", interval),
			zap.Duration("period", period))

		periodic.Start(ctx, interval, func(tick periodic.Tick) {
			olderThan := tick.Time.Add(-period)

			r.logger.Debugf("Pruning data from table %s older than %s", pruner.Table, olderThan)

			deleted, err := pruner.Exec(ctx, r.db, types.UnixMilli(olderThan), conf.BatchSize)
			if err != nil {
				select {
				case errs <- err:
				case <-ctx.Done():
				}
			}

			level := zap.DebugLevel
			if deleted > 0 {
				level = zap.InfoLevel
			}
			r.logger.Logf(level, "Removed %d old items from table %s", deleted, pruner.Table)
		}, periodic.Immediate())
	}

	if errs != nil {
		select {
		case err := <-errs:
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// dbPruners defines the list of tables and their corresponding time columns to be pruned, along with any related
// tables that need to be pruned in a cascading manner to maintain referential integrity. Each entry specifies the
// main table to prune, its primary key, the time column used for determining which rows are old enough to delete,
// and any referrer tables that have foreign key relationships with the main table.
var dbPruners = []TimeBoundPruner{
	{
		Table:      "incident",
		PK:         "id",
		TimeColumn: "recovered_at",
		Referrers: []ReferencingRowPruner{
			{Table: "incident_contact", FK: "incident_id"},
			{Table: "incident_rule", FK: "incident_id"},
			// Incident history references `incident_rule_escalation_state` too, so must appear before it in the cascade.
			{Table: "incident_history", FK: "incident_id"},
			{Table: "incident_rule_escalation_state", FK: "incident_id"},
		},
	},
	// Extra pruners for the event_queue.
	{
		// Events being processed too long - implies crashed daemon.
		Table:                     "event_queue",
		PK:                        "id",
		TimeColumn:                "time",
		ExtraCondition:            "state = 1",
		OverridePeriodAndInterval: 15 * time.Minute,
	},
	{
		// Successfully processed events.
		Table:                     "event_queue",
		PK:                        "id",
		TimeColumn:                "time",
		ExtraCondition:            "state = 2",
		OverridePeriodAndInterval: 5 * time.Minute,
	},
	{
		// Events in the error state.
		Table:                     "event_queue",
		PK:                        "id",
		TimeColumn:                "time",
		ExtraCondition:            "state = 64",
		OverridePeriodAndInterval: 24 * time.Hour,
	},
}
