package config

import (
	"fmt"
	"github.com/icinga/icinga-notifications/internal/timeperiod"
	"slices"
)

func (r *RuntimeConfig) applyPendingTimePeriods() {
	incrementalApplyPending(
		r,
		&r.TimePeriods, &r.configChange.TimePeriods,
		func(newElement *timeperiod.TimePeriod) error {
			if newElement.Name == "" {
				newElement.Name = fmt.Sprintf("Time Period #%d", newElement.ID)
			}
			return nil
		},
		func(element, update *timeperiod.TimePeriod) error {
			if update.Name != "" {
				element.Name = update.Name
			}
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
			return nil
		},
		nil,
		func(delElement *timeperiod.Entry) error {
			period, ok := r.TimePeriods[delElement.TimePeriodID]
			if !ok {
				return fmt.Errorf("time period entry refers unknown time period %d", delElement.TimePeriodID)
			}

			period.Entries = slices.DeleteFunc(period.Entries, func(entry *timeperiod.Entry) bool {
				return entry.ID == delElement.ID
			})
			return nil
		})
}
