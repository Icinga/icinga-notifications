package config

import (
	"fmt"
	"github.com/icinga/icinga-notifications/internal/recipient"
	"github.com/icinga/icinga-notifications/internal/timeperiod"
	"go.uber.org/zap"
	"slices"
)

// applyPendingSchedules synchronizes changed schedules.
func (r *RuntimeConfig) applyPendingSchedules() {
	// Set of schedules (by id) which Rotation was altered and where RefreshRotations must be called.
	updatedScheduleIds := make(map[int64]struct{})

	incrementalApplyPending(
		r,
		&r.Schedules, &r.configChange.Schedules,
		nil,
		func(curElement, update *recipient.Schedule) error {
			curElement.ChangedAt = update.ChangedAt
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
			if curElement.ScheduleID != update.ScheduleID {
				return errRemoveAndAddInstead
			}

			curElement.ChangedAt = update.ChangedAt
			curElement.ActualHandoff = update.ActualHandoff
			curElement.Priority = update.Priority
			curElement.Name = update.Name

			updatedScheduleIds[curElement.ScheduleID] = struct{}{}
			return nil
		},
		func(delElement *recipient.Rotation) error {
			schedule, ok := r.Schedules[delElement.ScheduleID]
			if !ok {
				return nil
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
			rotation, ok := r.scheduleRotations[newElement.RotationID]
			if !ok {
				return fmt.Errorf("schedule rotation member refers unknown rotation %d", newElement.RotationID)
			}

			rotation.Members = append(rotation.Members, newElement)
			updatedScheduleIds[rotation.ScheduleID] = struct{}{}

			if newElement.ContactID.Valid {
				newElement.Contact, ok = r.Contacts[newElement.ContactID.Int64]
				if !ok {
					return fmt.Errorf("schedule rotation member refers unknown contact %d", newElement.ContactID.Int64)
				}
			}

			if newElement.ContactGroupID.Valid {
				newElement.ContactGroup, ok = r.Groups[newElement.ContactGroupID.Int64]
				if !ok {
					return fmt.Errorf("schedule rotation member refers unknown contact group %d", newElement.ContactGroupID.Int64)
				}
			}

			newElement.TimePeriodEntries = make(map[int64]*timeperiod.Entry)
			return nil
		},
		nil,
		func(delElement *recipient.RotationMember) error {
			rotation, ok := r.scheduleRotations[delElement.RotationID]
			if !ok {
				return nil
			}

			rotation.Members = slices.DeleteFunc(rotation.Members, func(member *recipient.RotationMember) bool {
				return member.ID == delElement.ID
			})
			updatedScheduleIds[rotation.ScheduleID] = struct{}{}
			return nil
		})

	for id := range updatedScheduleIds {
		schedule := r.Schedules[id]
		r.logger.Debugw("Refreshing schedule rotations", zap.Inline(schedule))
		schedule.RefreshRotations()
	}
}
