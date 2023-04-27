package config

import (
	"context"
	"database/sql"
	"github.com/icinga/icingadb/pkg/icingadb"
	"github.com/icinga/icingadb/pkg/logging"
	"github.com/icinga/noma/internal/recipient"
	"github.com/jmoiron/sqlx"
	"go.uber.org/zap"
	"log"
)

func (r *RuntimeConfig) UpdateSchedulesFromDatabase(ctx context.Context, db *icingadb.DB, tx *sqlx.Tx, logger *logging.Logger) error {
	var schedulePtr *recipient.Schedule
	stmt := db.BuildSelectStmt(schedulePtr, schedulePtr)
	log.Println(stmt)

	var schedules []*recipient.Schedule
	if err := tx.SelectContext(ctx, &schedules, stmt); err != nil {
		log.Println(err)
		return err
	}

	schedulesById := make(map[int64]*recipient.Schedule)
	for _, g := range schedules {
		schedulesById[g.ID] = g

		logger.Debugw("loaded schedule config",
			zap.Int64("id", g.ID),
			zap.String("name", g.Name))
	}

	type ScheduleMember struct {
		ScheduleID   int64         `db:"schedule_id"`
		TimePeriodID int64         `db:"timeperiod_id"`
		ContactID    sql.NullInt64 `db:"contact_id"`
		GroupID      sql.NullInt64 `db:"contactgroup_id"`
	}

	var memberPtr *ScheduleMember
	stmt = db.BuildSelectStmt(memberPtr, memberPtr)
	log.Println(stmt)

	var members []*ScheduleMember
	if err := tx.SelectContext(ctx, &members, stmt); err != nil {
		log.Println(err)
		return err
	}

	for _, member := range members {
		memberLogger := logger.With(
			zap.Int64("schedule_id", member.ScheduleID),
			zap.Int64("timeperiod_id", member.TimePeriodID),
			zap.Int64("contact_id", member.ContactID.Int64),
			zap.Int64("contactgroup_id", member.GroupID.Int64),
		)

		if s := schedulesById[member.ScheduleID]; s == nil {
			memberLogger.Warnw("ignoring schedule member for unknown schedule_id")
		} else if p := r.TimePeriodsById[member.TimePeriodID]; p == nil {
			memberLogger.Warnw("ignoring schedule member for unknown timeperiod_id")
		} else if c := r.ContactsByID[member.ContactID.Int64]; member.ContactID.Valid && p == nil {
			memberLogger.Warnw("ignoring schedule member for unknown contact_id")
		} else if g := r.GroupsByID[member.GroupID.Int64]; member.GroupID.Valid && p == nil {
			memberLogger.Warnw("ignoring schedule member for unknown contactgroup_id")
		} else {
			s.Members = append(s.Members, &recipient.Member{
				TimePeriod:   p,
				Contact:      c,
				ContactGroup: g,
			})

			memberLogger.Debugw("member")
		}
	}

	r.SchedulesByID = schedulesById

	return nil
}
