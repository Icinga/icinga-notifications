package config

import (
	"context"
	"github.com/icinga/icinga-notifications/internal/recipient"
	"github.com/icinga/icinga-notifications/internal/timeperiod"
	"github.com/jmoiron/sqlx"
	"go.uber.org/zap"
)

func (r *RuntimeConfig) fetchSchedules(ctx context.Context, tx *sqlx.Tx) error {
	var schedulePtr *recipient.Schedule
	stmt := r.db.BuildSelectStmt(schedulePtr, schedulePtr)
	r.logger.Debugf("Executing query %q", stmt)

	var schedules []*recipient.Schedule
	if err := tx.SelectContext(ctx, &schedules, stmt); err != nil {
		r.logger.Errorln(err)
		return err
	}

	schedulesById := make(map[int64]*recipient.Schedule)
	for _, g := range schedules {
		schedulesById[g.ID] = g

		r.logger.Debugw("loaded schedule config",
			zap.Int64("id", g.ID),
			zap.String("name", g.Name))
	}

	var rotationPtr *recipient.Rotation
	stmt = r.db.BuildSelectStmt(rotationPtr, rotationPtr)
	r.logger.Debugf("Executing query %q", stmt)

	var rotations []*recipient.Rotation
	if err := tx.SelectContext(ctx, &rotations, stmt); err != nil {
		r.logger.Errorln(err)
		return err
	}

	rotationsById := make(map[int64]*recipient.Rotation)
	for _, rotation := range rotations {
		rotationLogger := r.logger.With(zap.Object("rotation", rotation))

		if schedule := schedulesById[rotation.ScheduleID]; schedule == nil {
			rotationLogger.Warnw("ignoring schedule rotation for unknown schedule_id")
		} else {
			rotationsById[rotation.ID] = rotation
			schedule.Rotations = append(schedule.Rotations, rotation)

			rotationLogger.Debugw("loaded schedule rotation")
		}
	}

	var rotationMemberPtr *recipient.RotationMember
	stmt = r.db.BuildSelectStmt(rotationMemberPtr, rotationMemberPtr)
	r.logger.Debugf("Executing query %q", stmt)

	var members []*recipient.RotationMember
	if err := tx.SelectContext(ctx, &members, stmt); err != nil {
		r.logger.Errorln(err)
		return err
	}

	rotationMembersById := make(map[int64]*recipient.RotationMember)
	for _, member := range members {
		memberLogger := r.logger.With(zap.Object("rotation_member", member))

		if rotation := rotationsById[member.RotationID]; rotation == nil {
			memberLogger.Warnw("ignoring rotation member for unknown rotation_member_id")
		} else {
			member.TimePeriodEntries = make(map[int64]*timeperiod.Entry)
			rotation.Members = append(rotation.Members, member)
			rotationMembersById[member.ID] = member

			memberLogger.Debugw("loaded schedule rotation member")
		}
	}

	var entryPtr *timeperiod.Entry
	stmt = r.db.BuildSelectStmt(entryPtr, entryPtr) + " WHERE rotation_member_id IS NOT NULL"
	r.logger.Debugf("Executing query %q", stmt)

	var entries []*timeperiod.Entry
	if err := tx.SelectContext(ctx, &entries, stmt); err != nil {
		r.logger.Errorln(err)
		return err
	}

	for _, entry := range entries {
		var member *recipient.RotationMember
		if entry.RotationMemberID.Valid {
			member = rotationMembersById[entry.RotationMemberID.Int64]
		}

		if member == nil {
			r.logger.Warnw("ignoring entry for unknown rotation_member_id",
				zap.Int64("timeperiod_entry_id", entry.ID),
				zap.Int64("timeperiod_id", entry.TimePeriodID))
			continue
		}

		err := entry.Init()
		if err != nil {
			r.logger.Warnw("ignoring time period entry",
				zap.Object("entry", entry),
				zap.Error(err))
			continue
		}

		member.TimePeriodEntries[entry.ID] = entry
	}

	for _, schedule := range schedulesById {
		schedule.RefreshRotations()
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

func (r *RuntimeConfig) applyPendingSchedules() {
	if r.Schedules == nil {
		r.Schedules = make(map[int64]*recipient.Schedule)
	}

	for id, pendingSchedule := range r.pending.Schedules {
		if pendingSchedule == nil {
			delete(r.Schedules, id)
		} else {
			for _, rotation := range pendingSchedule.Rotations {
				for _, member := range rotation.Members {
					memberLogger := r.logger.With(
						zap.Object("rotation", rotation),
						zap.Object("rotation_member", member))

					if member.ContactID.Valid {
						member.Contact = r.Contacts[member.ContactID.Int64]
						if member.Contact == nil {
							memberLogger.Warnw("rotation member has an unknown contact_id")
						}
					}

					if member.ContactGroupID.Valid {
						member.ContactGroup = r.Groups[member.ContactGroupID.Int64]
						if member.ContactGroup == nil {
							memberLogger.Warnw("rotation member has an unknown contactgroup_id")
						}
					}
				}
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
