package config

import (
	"context"
	"github.com/icinga/icingadb/pkg/icingadb"
	"github.com/icinga/icingadb/pkg/logging"
	"github.com/icinga/noma/internal/recipient"
	"github.com/jmoiron/sqlx"
	"go.uber.org/zap"
	"log"
)

func (r *RuntimeConfig) fetchSchedules(ctx context.Context, db *icingadb.DB, tx *sqlx.Tx, logger *logging.Logger) error {
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

	var memberPtr *recipient.ScheduleMemberRow
	stmt = db.BuildSelectStmt(memberPtr, memberPtr)
	log.Println(stmt)

	var members []*recipient.ScheduleMemberRow
	if err := tx.SelectContext(ctx, &members, stmt); err != nil {
		log.Println(err)
		return err
	}

	for _, member := range members {
		memberLogger := makeScheduleMemberLogger(logger.SugaredLogger, member)

		if s := schedulesById[member.ScheduleID]; s == nil {
			memberLogger.Warnw("ignoring schedule member for unknown schedule_id")
		} else {
			s.MemberRows = append(s.MemberRows, member)

			memberLogger.Debugw("member")
		}
	}

	if r.Schedules != nil {
		// mark no longer existing schedules for deletion
		for id := range r.Schedules {
			if _, ok := schedulesById[id]; !ok {
				schedulesById[id] = nil
			}
		}
	}

	r.pending.Schedules = schedulesById

	return nil
}

func (r *RuntimeConfig) applyPendingSchedules(logger *logging.Logger) {
	if r.Schedules == nil {
		r.Schedules = make(map[int64]*recipient.Schedule)
	}

	for id, pendingSchedule := range r.pending.Schedules {
		if pendingSchedule == nil {
			delete(r.Schedules, id)
		} else {
			for _, memberRow := range pendingSchedule.MemberRows {
				memberLogger := makeScheduleMemberLogger(logger.SugaredLogger, memberRow)

				period := r.TimePeriods[memberRow.TimePeriodID]
				if period == nil {
					memberLogger.Warnw("ignoring schedule member for unknown timeperiod_id")
					continue
				}

				var contact *recipient.Contact
				if memberRow.ContactID.Valid {
					contact = r.Contacts[memberRow.ContactID.Int64]
					if contact == nil {
						memberLogger.Warnw("ignoring schedule member for unknown contact_id")
						continue
					}
				}

				var group *recipient.Group
				if memberRow.GroupID.Valid {
					group = r.Groups[memberRow.GroupID.Int64]
					if group == nil {
						memberLogger.Warnw("ignoring schedule member for unknown contactgroup_id")
						continue
					}
				}

				pendingSchedule.Members = append(pendingSchedule.Members, &recipient.Member{
					TimePeriod:   period,
					Contact:      contact,
					ContactGroup: group,
				})
			}

			if currentSchedule := r.Schedules[id]; currentSchedule != nil {
				*currentSchedule = *pendingSchedule
			} else {
				r.Schedules[id] = pendingSchedule
			}
		}
	}

	r.pending.Schedules = nil
}

func makeScheduleMemberLogger(logger *zap.SugaredLogger, member *recipient.ScheduleMemberRow) *zap.SugaredLogger {
	return logger.With(
		zap.Int64("schedule_id", member.ScheduleID),
		zap.Int64("timeperiod_id", member.TimePeriodID),
		zap.Int64("contact_id", member.ContactID.Int64),
		zap.Int64("contactgroup_id", member.GroupID.Int64),
	)
}
