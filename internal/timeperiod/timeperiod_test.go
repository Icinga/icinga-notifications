package timeperiod_test

import (
	"database/sql"
	"fmt"
	"github.com/icinga/icinga-go-library/types"
	"github.com/icinga/icinga-notifications/internal/timeperiod"
	"github.com/stretchr/testify/assert"
	"github.com/teambition/rrule-go"
	"testing"
	"time"
)

func TestEntry(t *testing.T) {
	t.Parallel()

	t.Run("Contains", func(t *testing.T) {
		t.Parallel()

		start := berlinTime("2023-03-01 09:00:00")
		end := berlinTime("2023-03-01 11:00:00")
		until := berlinTime("2023-03-03 09:00:00")
		e := &timeperiod.Entry{
			StartTime: types.UnixMilli(start),
			EndTime:   types.UnixMilli(end),
			Timezone:  berlin,
			RRule: sql.NullString{
				String: fmt.Sprintf("FREQ=DAILY;UNTIL=%s", until.UTC().Format(rrule.DateTimeFormat)),
				Valid:  true,
			},
		}

		t.Run("TimeAtFirstRecurrenceStart", func(t *testing.T) {
			assert.True(t, e.Contains(start))
		})

		t.Run("TimeAtFirstRecurrenceEnd", func(t *testing.T) {
			assert.True(t, e.Contains(end.Add(-1*time.Second)))
			assert.True(t, e.Contains(end.Add(-1*time.Nanosecond)))
			assert.False(t, e.Contains(end))
		})

		t.Run("TimeBeforeFirstRecurrenceStart", func(t *testing.T) {
			assert.False(t, e.Contains(start.Add(-1*time.Hour)))
			assert.False(t, e.Contains(start.Add(-1*time.Second)))
			assert.False(t, e.Contains(start.Add(-1*time.Nanosecond)))
		})

		t.Run("TimeBeforeRecurrenceStart", func(t *testing.T) {
			// Start event is always 09:00:00 AM
			assert.False(t, e.Contains(berlinTime("2023-03-02 08:00:00")))
			assert.False(t, e.Contains(berlinTime("2023-03-02 08:59:59")))
		})

		t.Run("TimeAfterRecurrenceEnd", func(t *testing.T) {
			assert.False(t, e.Contains(berlinTime("2023-03-02 12:30:00")))
			assert.False(t, e.Contains(berlinTime("2023-03-02 11:00:00")))
		})

		t.Run("TimeWithinARecurrence", func(t *testing.T) {
			assert.True(t, e.Contains(berlinTime("2023-03-01 10:30:00")))
			assert.True(t, e.Contains(berlinTime("2023-03-02 09:30:00")))

			// Interval end is 10:59:59, so this should be inside
			assert.True(t, e.Contains(berlinTime("2023-03-02 10:59:00")))
			assert.True(t, e.Contains(berlinTime("2023-03-02 10:59:59")))
		})

		t.Run("TimeAfterLastRecurrenceEnd", func(t *testing.T) {
			// 2023-03-03 09:00:00 is the last event start time and 2023-03-03 11:59:59 is the last event end.
			// So this shouldn't be covered!
			assert.False(t, e.Contains(berlinTime("2023-03-03 12:00:00")))
			assert.False(t, e.Contains(berlinTime("2023-03-03 16:30:00")))
			assert.False(t, e.Contains(berlinTime("2023-03-04 06:00:00")))
			assert.False(t, e.Contains(berlinTime("2023-03-04 10:00:00")))
		})

		t.Run("DST", func(t *testing.T) {
			start := berlinTime("2023-03-25 01:00:00")
			end := berlinTime("2023-03-25 02:30:00")
			e := &timeperiod.Entry{
				StartTime: types.UnixMilli(start),
				EndTime:   types.UnixMilli(end),
				Timezone:  berlin,
				RRule:     sql.NullString{String: "FREQ=DAILY", Valid: true},
			}

			assert.True(t, e.Contains(start))

			tz := time.FixedZone("CET", 60*60)
			tm := time.Date(2023, time.March, 26, 1, 30, 0, 0, tz)
			assert.True(t, e.Contains(tm))
			assert.True(t, e.Contains(tm.Add(time.Hour/2)))

			assert.False(t, e.Contains(tm.Add(time.Hour)))
		})
	})

	t.Run("Transitions", func(t *testing.T) {
		t.Parallel()

		start := berlinTime("2023-03-01 08:00:00")
		end := berlinTime("2023-03-01 12:30:00")
		e := &timeperiod.Entry{
			StartTime: types.UnixMilli(start),
			EndTime:   types.UnixMilli(end),
			Timezone:  berlin,
			RRule:     sql.NullString{String: "FREQ=DAILY", Valid: true},
		}

		t.Run("TimeAtFirstRecurrenceStart", func(t *testing.T) {
			assert.Equal(t, end, e.NextTransition(start))
		})

		t.Run("TimeAtFirstRecurrenceEnd", func(t *testing.T) {
			assert.Equal(t, berlinTime("2023-03-02 08:00:00"), e.NextTransition(end))
			assert.Equal(t, end, e.NextTransition(end.Add(-1*time.Second)))
			assert.Equal(t, end, e.NextTransition(end.Add(-1*time.Nanosecond)))
		})

		t.Run("TimeAfterRecurrenceStart", func(t *testing.T) {
			assert.Equal(t, berlinTime("2023-03-15 12:30:00"), e.NextTransition(berlinTime("2023-03-15 11:00:00")))
			assert.Equal(t, berlinTime("2023-04-01 12:30:00"), e.NextTransition(berlinTime("2023-04-01 09:00:00")))
		})

		t.Run("TimeAfterRecurrenceEnd", func(t *testing.T) {
			assert.Equal(t, berlinTime("2023-03-16 08:00:00"), e.NextTransition(berlinTime("2023-03-15 13:00:00")))
			assert.Equal(t, berlinTime("2023-04-01 08:00:00"), e.NextTransition(berlinTime("2023-03-31 18:00:00")))
		})

		t.Run("DST", func(t *testing.T) {
			start := berlinTime("2023-03-25 01:00:00")
			end := berlinTime("2023-03-25 02:30:00")
			e := &timeperiod.Entry{
				StartTime: types.UnixMilli(start),
				EndTime:   types.UnixMilli(end),
				Timezone:  berlin,
				RRule:     sql.NullString{String: "FREQ=DAILY", Valid: true},
			}

			assert.Equal(t, end, e.NextTransition(start), "next transition should match the first recurrence end")

			tz := time.FixedZone("CET", 60*60)
			tm := time.Date(2023, time.March, 26, 1, 30, 0, 0, tz)
			// 02:30 is skipped on this day, so end is shifted automatically to the next hour.
			assert.Equal(t, berlinTime("2023-03-26 03:30:00"), e.NextTransition(tm))
			assert.Equal(t, berlinTime("2023-03-26 03:30:00"), e.NextTransition(tm.Add(time.Hour/2)))

			// valid transition next to "2023-03-26 02:30:00 +1" is the start event on the next day.
			assert.Equal(t, berlinTime("2023-03-27 01:00:00"), e.NextTransition(tm.Add(time.Hour)))
		})
	})
}

func TestTimePeriodTransitions(t *testing.T) {
	t.Parallel()

	t.Run("WithOneEntry", func(t *testing.T) {
		start := berlinTime("2023-03-27 09:00:00")
		end := berlinTime("2023-03-27 17:00:00")
		p := &timeperiod.TimePeriod{
			Name: "Transition Test",
			Entries: []*timeperiod.Entry{{
				StartTime: types.UnixMilli(start),
				EndTime:   types.UnixMilli(end),
				Timezone:  berlin,
				RRule:     sql.NullString{String: "FREQ=DAILY", Valid: true},
			}},
		}

		assert.Equal(t, end, p.NextTransition(start), "next transition should match the interval end")
	})

	t.Run("WithMultipleEntries", func(t *testing.T) {
		t.Parallel()

		start := berlinTime("2023-03-27 09:00:00")
		end := berlinTime("2023-03-27 09:30:00")
		p := &timeperiod.TimePeriod{
			Name: "Transition Test",
			Entries: []*timeperiod.Entry{
				{
					StartTime: types.UnixMilli(start),
					EndTime:   types.UnixMilli(end),
					Timezone:  berlin,
					RRule:     sql.NullString{String: "FREQ=HOURLY;BYHOUR=1,3,5,7,9,11,13,15", Valid: true},
				},
				{
					StartTime: types.UnixMilli(berlinTime("2023-03-27 08:00:00")),
					EndTime:   types.UnixMilli(berlinTime("2023-03-27 08:30:00")),
					Timezone:  berlin,
					RRule:     sql.NullString{String: "FREQ=HOURLY;BYHOUR=0,2,4,6,8,10,12,14", Valid: true},
				},
			},
		}

		assert.Equal(t, end, p.NextTransition(start), "next transition should match the interval end")

		t.Run("TimeAfterFirstIntervalEnd", func(t *testing.T) {
			// 09:00 - 09:30 is covered by the first entry and the next transition should be covered by the second one.
			assert.Equal(t, berlinTime("2023-03-27 10:00:00"), p.NextTransition(end))
		})

		t.Run("TimeAfterRandomIntervalStart", func(t *testing.T) {
			assert.Equal(t, berlinTime("2023-03-28 04:30:00"), p.NextTransition(berlinTime("2023-03-28 04:00:00")))
			assert.Equal(t, berlinTime("2023-03-30 11:30:00"), p.NextTransition(berlinTime("2023-03-30 11:00:00")))
		})

		t.Run("TimeAfterRandomIntervalEnd", func(t *testing.T) {
			// Transition of the second entry ends at 14:30, so the next one should be 15:00 covered by the first one.
			assert.Equal(t, berlinTime("2023-03-27 15:00:00"), p.NextTransition(berlinTime("2023-03-27 14:30:00")))
			assert.Equal(t, berlinTime("2023-03-27 12:00:00"), p.NextTransition(berlinTime("2023-03-27 11:30:00")))

			// Transition of the first entry ends at 15:30 -> next start event 00:00 on the next day
			assert.Equal(t, berlinTime("2023-03-28 00:00:00"), p.NextTransition(berlinTime("2023-03-27 15:30:00")))
			assert.Equal(t, berlinTime("2023-03-28 00:00:00"), p.NextTransition(berlinTime("2023-03-27 18:00:00")))
		})
	})
}

const berlin = "Europe/Berlin"

func berlinTime(value string) time.Time {
	loc, err := time.LoadLocation(berlin)
	if err != nil {
		panic(err)
	}

	t, err := time.ParseInLocation(time.DateTime, value, loc)
	if err != nil {
		panic(err)
	}

	return t
}
