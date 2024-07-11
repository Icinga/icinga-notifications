package config

import (
	"fmt"
	"github.com/icinga/icinga-notifications/internal/timeperiod"
	"slices"
)

// applyPendingTimePeriods synchronizes changed time periods.
func (r *RuntimeConfig) applyPendingTimePeriods() {
	incrementalApplyPending(
		r,
		&r.TimePeriods, &r.configChange.TimePeriods,
		nil,
		func(curElement, update *timeperiod.TimePeriod) error {
			curElement.ChangedAt = update.ChangedAt
			curElement.Name = update.Name
			return nil
		},
		nil)

	incrementalApplyPending(
		r,
		&r.timePeriodEntries, &r.configChange.timePeriodEntries,
		func(newElement *timeperiod.Entry) error {
			period, ok := r.TimePeriods[newElement.TimePeriodID]
			if !ok {
				return fmt.Errorf("time period entry refers unknown time period %d", newElement.TimePeriodID)
			}

			period.Entries = append(period.Entries, newElement)

			// rotation_member_id is nullable for future standalone timeperiods
			if newElement.RotationMemberID.Valid {
				rotationMember, ok := r.scheduleRotationMembers[newElement.RotationMemberID.Int64]
				if !ok {
					return fmt.Errorf("time period entry refers unknown rotation member %d", newElement.RotationMemberID.Int64)
				}

				rotationMember.TimePeriodEntries[newElement.ID] = newElement
			}

			return nil
		},
		nil,
		func(delElement *timeperiod.Entry) error {
			period, ok := r.TimePeriods[delElement.TimePeriodID]
			if ok {
				period.Entries = slices.DeleteFunc(period.Entries, func(entry *timeperiod.Entry) bool {
					return entry.ID == delElement.ID
				})
			}

			if delElement.RotationMemberID.Valid {
				rotationMember, ok := r.scheduleRotationMembers[delElement.RotationMemberID.Int64]
				if ok {
					delete(rotationMember.TimePeriodEntries, delElement.ID)
				}
			}

			return nil
		})
}
