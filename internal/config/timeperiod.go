package config

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/icinga/icingadb/pkg/icingadb"
	"github.com/icinga/icingadb/pkg/logging"
	"github.com/icinga/noma/internal/timeperiod"
	"github.com/jmoiron/sqlx"
	"go.uber.org/zap"
	"log"
	"time"
)

func (r *RuntimeConfig) UpdateTimePeriodsFromDatabase(ctx context.Context, db *icingadb.DB, tx *sqlx.Tx, logger *logging.Logger) error {
	// TODO: At the moment, the timeperiod table contains no interesting fields for the daemon, therefore only
	// entries are fetched and TimePeriod instances are created on the fly.

	type TimeperiodEntry struct {
		ID           int64          `db:"id"`
		TimePeriodID int64          `db:"timeperiod_id"`
		StartTime    int64          `db:"start_time"`
		EndTime      int64          `db:"end_time"`
		Timezone     string         `db:"timezone"`
		RRule        sql.NullString `db:"rrule"`
		Description  sql.NullString `db:"description"`
	}

	var entryPtr *TimeperiodEntry
	stmt := db.BuildSelectStmt(entryPtr, entryPtr)
	log.Println(stmt)

	var entries []*TimeperiodEntry
	if err := tx.SelectContext(ctx, &entries, stmt); err != nil {
		log.Println(err)
		return err
	}

	timePeriodsById := make(map[int64]*timeperiod.TimePeriod)
	for _, row := range entries {
		p := timePeriodsById[row.TimePeriodID]
		if p == nil {
			p = &timeperiod.TimePeriod{
				Name: fmt.Sprintf("Time Period #%d", row.TimePeriodID),
			}
			if row.Description.Valid {
				p.Name += fmt.Sprintf(" (%s)", row.Description.String)
			}
			timePeriodsById[row.TimePeriodID] = p

			logger.Debugw("created time period",
				zap.Int64("id", row.TimePeriodID),
				zap.String("name", p.Name))
		}

		loc, err := time.LoadLocation(row.Timezone)
		if err != nil {
			logger.Warnw("ignoring time period entry with unknown timezone",
				zap.Int64("timeperiod_entry_id", row.ID),
				zap.String("timezone", row.Timezone),
				zap.Error(err))
			continue
		}

		entry := &timeperiod.Entry{
			Start:    time.Unix(row.StartTime, 0).In(loc),
			End:      time.Unix(row.EndTime, 0).In(loc),
			TimeZone: row.Timezone,
		}

		if row.RRule.Valid {
			entry.RecurrenceRule = row.RRule.String
		}

		err = entry.Init()
		if err != nil {
			logger.Warnw("ignoring time period entry",
				zap.Int64("timeperiod_entry_id", row.ID),
				zap.String("rrule", entry.RecurrenceRule),
				zap.Error(err))
			continue
		}

		logger.Debugw("loaded time period entry",
			zap.String("timeperiod", p.Name),
			zap.Time("start", entry.Start),
			zap.Time("end", entry.End),
			zap.String("rrule", entry.RecurrenceRule))
	}

	timePeriods := make([]*timeperiod.TimePeriod, len(timePeriodsById))
	for _, p := range timePeriodsById {
		timePeriods = append(timePeriods, p)
	}

	r.TimePeriodsById = timePeriodsById

	return nil
}
