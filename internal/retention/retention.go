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
// It uses a list of [Pruner] configurations to determine which tables to prune and how to maintain referential
// integrity with related tables. The retention process runs in a separate goroutine for each pruner, allowing
// for concurrent pruning of multiple tables without blocking one another. The retention intervals and limits
// are configurable, and the process can be gracefully stopped using the provided context.
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

		if pruner.TableName() == "incident" && conf.Options.Incident != nil {
			period = *conf.Options.Incident
		}

		if d := pruner.IntervalAndPeriodOverrides(); d != 0 {
			period = d
			interval = d
		}

		if period == 0 {
			r.logger.Debugf("Skipping retention for %s table because retention period is set to 0", pruner.TableName())
			continue
		}

		if errs == nil {
			errs = make(chan error, 1)
		}

		r.logger.Debugw("Starting retention",
			zap.String("table", pruner.TableName()),
			zap.Duration("interval", interval),
			zap.Duration("period", period))

		periodic.Start(ctx, interval, func(tick periodic.Tick) {
			olderThan := tick.Time.Add(-period)

			if _, ok := pruner.(*TimeBoundPruner); ok {
				r.logger.Debugf("Pruning data from %s table older than %s", pruner.TableName(), olderThan)
			} else {
				r.logger.Debugf("Pruning orphaned data from %s table", pruner.TableName())
			}

			deleted, err := pruner.Exec(ctx, r.db, r.logger, types.UnixMilli(olderThan), conf.BatchSize)
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
			r.logger.Logf(level, "Removed %d items from %s table", deleted, pruner.TableName())
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
var dbPruners = []Pruner{
	&OrphanRowPruner{
		prunerCommon: prunerCommon{
			Table:  "object",
			PKorFK: "id",
		},
		ReferencingTable: "incident",
		ReferencingFK:    "object_id",
		Interval:         4 * time.Hour,
		Referrers: []ReferencingRowPruner{
			{Table: "object_id_tag", PKorFK: "object_id"},
		},
	},
	&TimeBoundPruner{
		prunerCommon: prunerCommon{
			Table:  "incident",
			PKorFK: "id",
		},
		TimeColumn: "recovered_at",
		Referrers: []ReferencingRowPruner{
			{Table: "incident_contact", PKorFK: "incident_id"},
			{Table: "incident_rule", PKorFK: "incident_id"},
			// Incident history references `incident_rule_escalation_state` too, so must appear before it in the cascade.
			{Table: "incident_history", PKorFK: "incident_id"},
			{Table: "incident_rule_escalation_state", PKorFK: "incident_id"},
		},
	},
	// Extra pruners for the event_queue.
	&TimeBoundPruner{
		// Events being processed too long - implies crashed daemon.
		prunerCommon: prunerCommon{
			Table:  "event_queue",
			PKorFK: "id",
		},
		TimeColumn:                "time",
		ExtraCondition:            "state = 1",
		OverridePeriodAndInterval: 15 * time.Minute,
	},
	&TimeBoundPruner{
		// Successfully processed events.
		prunerCommon: prunerCommon{
			Table:  "event_queue",
			PKorFK: "id",
		},
		TimeColumn:                "time",
		ExtraCondition:            "state = 2",
		OverridePeriodAndInterval: 5 * time.Minute,
	},
	&TimeBoundPruner{
		// Events in the error state.
		prunerCommon: prunerCommon{
			Table:  "event_queue",
			PKorFK: "id",
		},
		TimeColumn:                "time",
		ExtraCondition:            "state = 64",
		OverridePeriodAndInterval: 24 * time.Hour,
	},
}
