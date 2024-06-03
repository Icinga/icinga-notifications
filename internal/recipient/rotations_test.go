package recipient

import (
	"database/sql"
	"github.com/icinga/icinga-go-library/types"
	"github.com/icinga/icinga-notifications/internal/timeperiod"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func Test_rotationResolver_getCurrentRotations(t *testing.T) {
	contactWeekday := &Contact{FullName: "Weekday Non-Noon"}
	contactWeekdayNoon := &Contact{FullName: "Weekday Noon"}
	contactWeekend2024a := &Contact{FullName: "Weekend 2024 A"}
	contactWeekend2024b := &Contact{FullName: "Weekend 2024 B"}
	contactWeekend2025a := &Contact{FullName: "Weekend 2025 A"}
	contactWeekend2025b := &Contact{FullName: "Weekend 2025 B"}

	// Helper function to parse strings into time.Time interpreted as UTC.
	// Accepts values like "2006-01-02 15:04:05" and "2006-01-02" (assuming 00:00:00 as time).
	parse := func(s string) time.Time {
		var format string

		switch len(s) {
		case len(time.DateTime):
			format = time.DateTime
		case len(time.DateOnly):
			format = time.DateOnly
		}

		t, err := time.ParseInLocation(format, s, time.UTC)
		if err != nil {
			panic(err)
		}
		return t
	}

	rotations := []*Rotation{
		// Weekend rotation starting 2024, alternating between contacts contactWeekend2024a and contactWeekend2024b
		{
			ActualHandoff: types.UnixMilli(parse("2024-01-01")),
			Priority:      0,
			Members: []*RotationMember{
				{
					Contact: contactWeekend2024a,
					TimePeriodEntries: map[int64]*timeperiod.Entry{
						1: {
							StartTime: types.UnixMilli(parse("2024-01-06")), // Saturday
							EndTime:   types.UnixMilli(parse("2024-01-07")), // Sunday
							Timezone:  "UTC",
							RRule:     sql.NullString{String: "FREQ=WEEKLY;INTERVAL=2;BYDAY=SA,SU", Valid: true},
						},
					},
				}, {
					Contact: contactWeekend2024b,
					TimePeriodEntries: map[int64]*timeperiod.Entry{
						2: {
							StartTime: types.UnixMilli(parse("2024-01-13")), // Saturday
							EndTime:   types.UnixMilli(parse("2024-01-14")), // Sunday
							Timezone:  "UTC",
							RRule:     sql.NullString{String: "FREQ=WEEKLY;INTERVAL=2;BYDAY=SA,SU", Valid: true},
						},
					},
				},
			},
		},

		// Weekend rotation starting 2025 and replacing the previous one,
		// alternating between contacts contactWeekend2025a and contactWeekend2025b
		{
			ActualHandoff: types.UnixMilli(parse("2025-01-01")),
			Priority:      0,
			Members: []*RotationMember{
				{
					Contact: contactWeekend2025a,
					TimePeriodEntries: map[int64]*timeperiod.Entry{
						3: {
							StartTime: types.UnixMilli(parse("2025-01-04")), // Saturday
							EndTime:   types.UnixMilli(parse("2025-01-05")), // Sunday
							Timezone:  "UTC",
							RRule:     sql.NullString{String: "FREQ=WEEKLY;INTERVAL=2;BYDAY=SA,SU", Valid: true},
						},
					},
				}, {
					Contact: contactWeekend2025b,
					TimePeriodEntries: map[int64]*timeperiod.Entry{
						4: {
							StartTime: types.UnixMilli(parse("2025-01-11")), // Saturday
							EndTime:   types.UnixMilli(parse("2025-01-12")), // Sunday
							Timezone:  "UTC",
							RRule:     sql.NullString{String: "FREQ=WEEKLY;INTERVAL=2;BYDAY=SA,SU", Valid: true},
						},
					},
				},
			},
		},

		// Weekday rotations starting 2024, one for contactWeekday every day from 8 to 20 o'clock,
		// with an override for 12 to 14 o'clock with contactWeekdayNoon.
		{
			ActualHandoff: types.UnixMilli(parse("2024-01-01")),
			Priority:      1,
			Members: []*RotationMember{
				{
					Contact: contactWeekdayNoon,
					TimePeriodEntries: map[int64]*timeperiod.Entry{
						5: {
							StartTime: types.UnixMilli(parse("2024-01-01 12:00:00")), // Monday
							EndTime:   types.UnixMilli(parse("2024-01-01 14:00:00")), // Monday
							Timezone:  "UTC",
							RRule:     sql.NullString{String: "FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR", Valid: true},
						},
					},
				},
			},
		}, {
			ActualHandoff: types.UnixMilli(parse("2024-01-01")),
			Priority:      2,
			Members: []*RotationMember{
				{
					Contact: contactWeekday,
					TimePeriodEntries: map[int64]*timeperiod.Entry{
						6: {
							StartTime: types.UnixMilli(parse("2024-01-01 08:00:00")), // Monday
							EndTime:   types.UnixMilli(parse("2024-01-01 20:00:00")), // Monday
							Timezone:  "UTC",
							RRule:     sql.NullString{String: "FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR", Valid: true},
						},
					},
				},
			},
		},
	}

	for _, r := range rotations {
		for _, m := range r.Members {
			for _, e := range m.TimePeriodEntries {
				require.NoError(t, e.Init())
			}
		}
	}

	var s rotationResolver
	s.update(rotations)

	for ts := parse("2023-01-01"); ts.Before(parse("2027-01-01")); ts = ts.Add(30 * time.Minute) {
		got := s.getContactsAt(ts)

		switch ts.Weekday() {
		case time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday:
			if y, h := ts.Year(), ts.Hour(); y >= 2024 && 12 <= h && h < 14 {
				if assert.Lenf(t, got, 1, "resolving rotations on %v should return one contact", ts) {
					assert.Equal(t, contactWeekdayNoon, got[0])
				}
			} else if y >= 2024 && 8 <= h && h < 20 {
				if assert.Lenf(t, got, 1, "resolving rotations on %v should return one contact", ts) {
					assert.Equal(t, contactWeekday, got[0])
				}
			} else {
				assert.Emptyf(t, got, "resolving rotations on %v should return no contacts", ts)
			}

		case time.Saturday, time.Sunday:
			switch y := ts.Year(); {
			case y == 2024:
				if assert.Lenf(t, got, 1, "resolving rotations on %v return one contact", ts) {
					assert.Contains(t, []*Contact{contactWeekend2024a, contactWeekend2024b}, got[0])
				}
			case y >= 2025:
				if assert.Lenf(t, got, 1, "resolving rotations on %v return one contact", ts) {
					assert.Contains(t, []*Contact{contactWeekend2025a, contactWeekend2025b}, got[0])
				}
			default:
				assert.Emptyf(t, got, "resolving rotations on %v should return no contacts", ts)
			}
		}
	}
}
