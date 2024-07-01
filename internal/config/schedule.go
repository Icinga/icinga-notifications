package config

import (
	"fmt"
	"github.com/icinga/icinga-notifications/internal/recipient"
	"github.com/icinga/icinga-notifications/internal/timeperiod"
	"go.uber.org/zap"
	"slices"
)

func (r *RuntimeConfig) applyPendingSchedules() {
	// Set of schedules (by id) which Rotation was altered and where RefreshRotations must be called.
	updatedScheduleIds := make(map[int64]struct{})

	incrementalApplyPending(
		r,
		&r.Schedules, &r.configChange.Schedules,
		nil,
		func(curElement, update *recipient.Schedule) error {
			curElement.Name = update.Name
			return nil
		},
		nil)

	incrementalApplyPending(
		r,
		&r.scheduleRotations, &r.configChange.scheduleRotations,
		func(newElement *recipient.Rotation) error {
			schedule, ok := r.Schedules[newElement.ScheduleID]
			if !ok {
				return fmt.Errorf("rotation refers to unknown schedule %d", newElement.ScheduleID)
			}

			schedule.Rotations = append(schedule.Rotations, newElement)
			updatedScheduleIds[schedule.ID] = struct{}{}
			return nil
		},
		func(curElement, update *recipient.Rotation) error {
			curElement.ActualHandoff = update.ActualHandoff
			curElement.Priority = update.Priority
			curElement.Name = update.Name
			return nil
		},
		func(delElement *recipient.Rotation) error {
			schedule, ok := r.Schedules[delElement.ScheduleID]
			if !ok {
				return fmt.Errorf("rotation refers to unknown schedule %d", delElement.ScheduleID)
			}

			schedule.Rotations = slices.DeleteFunc(schedule.Rotations, func(rotation *recipient.Rotation) bool {
				return rotation.ID == delElement.ID
			})
			updatedScheduleIds[schedule.ID] = struct{}{}
			return nil
		})

	incrementalApplyPending(
		r,
		&r.scheduleRotationMembers, &r.configChange.scheduleRotationMembers,
		func(newElement *recipient.RotationMember) error {
			newElement.TimePeriodEntries = make(map[int64]*timeperiod.Entry)

			var schedule *recipient.Schedule
			for id, tmpSchedule := range r.Schedules {
				ok := slices.ContainsFunc(tmpSchedule.Rotations, func(rotation *recipient.Rotation) bool {
					return rotation.ID == newElement.RotationID
				})
				if ok {
					newElement.ScheduleID = id
					schedule = tmpSchedule
					break
				}
			}
			if schedule == nil {
				return fmt.Errorf("schedule rotation member cannot be mapped to a schedule")
			}

			rotationId := slices.IndexFunc(schedule.Rotations, func(rotation *recipient.Rotation) bool {
				return rotation.ID == newElement.RotationID
			})
			if rotationId < 0 {
				return fmt.Errorf("schedule rotation member refers unknown rotation %d", newElement.RotationID)
			}

			rotation := schedule.Rotations[rotationId]
			rotation.Members = append(rotation.Members, newElement)
			updatedScheduleIds[newElement.ScheduleID] = struct{}{}

			if newElement.ContactID.Valid {
				var ok bool
				newElement.Contact, ok = r.Contacts[newElement.ContactID.Int64]
				if !ok {
					return fmt.Errorf("schedule rotation member refers unknown contact %d", newElement.ContactID.Int64)
				}
			}

			if newElement.ContactGroupID.Valid {
				var ok bool
				newElement.ContactGroup, ok = r.Groups[newElement.ContactGroupID.Int64]
				if !ok {
					return fmt.Errorf("schedule rotation member refers unknown contact group %d", newElement.ContactGroupID.Int64)
				}
			}
			return nil
		},
		nil,
		func(delElement *recipient.RotationMember) error {
			schedule, ok := r.Schedules[delElement.ScheduleID]
			if !ok {
				return fmt.Errorf("schedule rotation member refers unknown schedule %d", delElement.ScheduleID)
			}

			rotationId := slices.IndexFunc(schedule.Rotations, func(rotation *recipient.Rotation) bool {
				return rotation.ID == delElement.RotationID
			})
			if rotationId < 0 {
				return fmt.Errorf("schedule rotation member refers unknown rotation %d", delElement.RotationID)
			}

			rotation := schedule.Rotations[rotationId]
			rotation.Members = slices.DeleteFunc(rotation.Members, func(member *recipient.RotationMember) bool {
				return member.ID == delElement.ID
			})
			updatedScheduleIds[delElement.ScheduleID] = struct{}{}
			return nil
		})

	// Link time period entries to rotation members. Those entries were fetched before and are already present.
	for _, timeperiodEntry := range r.timePeriodEntries {
		if !timeperiodEntry.RotationMemberID.Valid {
			r.logger.Warnw("Skipping time period entry without a rotation member", zap.Inline(timeperiodEntry))
			continue
		}

		rotationMember, ok := r.scheduleRotationMembers[timeperiodEntry.RotationMemberID.Int64]
		if !ok {
			r.logger.Errorw("Time period entry refers unknown rotation member", zap.Inline(timeperiodEntry))
			continue
		}

		err := timeperiodEntry.Init()
		if err != nil {
			// This shouldn't happen as every element should already be initialized.
			r.logger.Errorw("Cannot initialize time period entry", zap.Inline(timeperiodEntry), zap.Error(err))
			continue
		}

		rotationMember.TimePeriodEntries[timeperiodEntry.ID] = timeperiodEntry
	}

	for id := range updatedScheduleIds {
		schedule := r.Schedules[id]
		r.logger.Debugw("Refreshing schedule rotations", zap.Inline(schedule))
		schedule.RefreshRotations()
	}
}
