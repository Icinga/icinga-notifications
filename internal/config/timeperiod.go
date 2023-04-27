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

func (r *RuntimeConfig) fetchTimePeriods(ctx context.Context, db *icingadb.DB, tx *sqlx.Tx, logger *logging.Logger) error {
	var timePeriodPtr *timeperiod.TimePeriod
	stmt := db.BuildSelectStmt(timePeriodPtr, timePeriodPtr)
	log.Println(stmt)

	var timePeriods []*timeperiod.TimePeriod
	if err := tx.SelectContext(ctx, &timePeriods, stmt); err != nil {
		log.Println(err)
		return err
	}
	timePeriodsById := make(map[int64]*timeperiod.TimePeriod)
	for _, period := range timePeriods {
		timePeriodsById[period.ID] = period
	}

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
	stmt = db.BuildSelectStmt(entryPtr, entryPtr)
	log.Println(stmt)

	var entries []*TimeperiodEntry
	if err := tx.SelectContext(ctx, &entries, stmt); err != nil {
		log.Println(err)
		return err
	}

	for _, row := range entries {
		p := timePeriodsById[row.TimePeriodID]
		if p == nil {
			logger.Warnw("ignoring entry for unknown timeperiod_id",
				zap.Int64("timeperiod_entry_id", row.ID),
				zap.Int64("timeperiod_id", row.TimePeriodID))
			continue
		}

		if p.Name == "" {
			p.Name = fmt.Sprintf("Time Period #%d", row.TimePeriodID)
			if row.Description.Valid {
				p.Name += fmt.Sprintf(" (%s)", row.Description.String)
			}
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

		p.Entries = append(p.Entries, entry)

		logger.Debugw("loaded time period entry",
			zap.String("timeperiod", p.Name),
			zap.Time("start", entry.Start),
			zap.Time("end", entry.End),
			zap.String("rrule", entry.RecurrenceRule))
	}

	for _, p := range timePeriodsById {
		if p.Name == "" {
			p.Name = fmt.Sprintf("Time Period #%d (empty)", p.ID)
		}
	}

	if r.TimePeriods != nil {
		// mark no longer existing time periods for deletion
		for id := range r.TimePeriods {
			if _, ok := timePeriodsById[id]; !ok {
				timePeriodsById[id] = nil
			}
		}
	}

	r.pending.TimePeriods = timePeriodsById

	return nil
}

func (r *RuntimeConfig) applyPendingTimePeriods(logger *logging.Logger) {
	if r.TimePeriods == nil {
		r.TimePeriods = make(map[int64]*timeperiod.TimePeriod)
	}

	for id, pendingTimePeriod := range r.pending.TimePeriods {
		if pendingTimePeriod == nil {
			delete(r.TimePeriods, id)
		} else if currentTimePeriod := r.TimePeriods[id]; currentTimePeriod != nil {
			*currentTimePeriod = *pendingTimePeriod
		} else {
			r.TimePeriods[id] = pendingTimePeriod
		}
	}

	r.pending.TimePeriods = nil
}
