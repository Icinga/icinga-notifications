package config

import (
	"context"
	"fmt"
	"github.com/icinga/icinga-notifications/internal/timeperiod"
	"github.com/jmoiron/sqlx"
	"go.uber.org/zap"
)

func (r *RuntimeConfig) fetchTimePeriods(ctx context.Context, tx *sqlx.Tx) error {
	var timePeriodPtr *timeperiod.TimePeriod
	stmt := r.db.BuildSelectStmt(timePeriodPtr, timePeriodPtr)
	r.logger.Debugf("Executing query %q", stmt)

	var timePeriods []*timeperiod.TimePeriod
	if err := tx.SelectContext(ctx, &timePeriods, stmt); err != nil {
		r.logger.Errorln(err)
		return err
	}
	timePeriodsById := make(map[int64]*timeperiod.TimePeriod)
	for _, period := range timePeriods {
		timePeriodsById[period.ID] = period
	}

	var entryPtr *timeperiod.Entry
	stmt = r.db.BuildSelectStmt(entryPtr, entryPtr)
	r.logger.Debugf("Executing query %q", stmt)

	var entries []*timeperiod.Entry
	if err := tx.SelectContext(ctx, &entries, stmt); err != nil {
		r.logger.Errorln(err)
		return err
	}

	for _, entry := range entries {
		p := timePeriodsById[entry.TimePeriodID]
		if p == nil {
			r.logger.Warnw("ignoring entry for unknown timeperiod_id",
				zap.Int64("timeperiod_entry_id", entry.ID),
				zap.Int64("timeperiod_id", entry.TimePeriodID))
			continue
		}

		if p.Name == "" {
			p.Name = fmt.Sprintf("Time Period #%d", entry.TimePeriodID)
		}

		err := entry.Init()
		if err != nil {
			r.logger.Warnw("ignoring time period entry",
				zap.Object("entry", entry),
				zap.Error(err))
			continue
		}

		p.Entries = append(p.Entries, entry)

		r.logger.Debugw("loaded time period entry",
			zap.Object("timeperiod", p),
			zap.Object("entry", entry))
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

func (r *RuntimeConfig) applyPendingTimePeriods() {
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
