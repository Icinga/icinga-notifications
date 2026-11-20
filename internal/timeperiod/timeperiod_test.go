package timeperiod_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/icinga/icinga-go-library/types"
	"github.com/icinga/icinga-notifications/internal/timeperiod"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teambition/rrule-go"
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
			RRule:     types.MakeString(fmt.Sprintf("FREQ=DAILY;UNTIL=%s", until.UTC().Format(rrule.DateTimeFormat))),
		}

		require.NoError(t, e.Init())

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
			t.Parallel()

			t.Run("SpringForward", func(t *testing.T) {
				t.Parallel()

				// The daily 01:00–02:30 interval lasts 90 minutes. On the spring-forward day, its end shifts
				// to 03:30 CEST accordingly, but the following day's occurrence ends at 02:30 CEST again.
				e := dailyBerlinEntry(t, "2023-03-25 01:00:00", "2023-03-25 02:30:00")

				assert.False(t, e.Contains(mar(25, 23, 59, 59)), "00:59:59 CET, just before transition day start")
				assert.True(t, e.Contains(mar(26, 0, 0, 0)), "01:00 CET, transition day start")
				assert.True(t, e.Contains(mar(26, 0, 59, 59)), "01:59:59 CET, last second before the gap")
				assert.True(t, e.Contains(mar(26, 1, 0, 0)), "03:00 CEST, right after the gap")
				assert.True(t, e.Contains(mar(26, 1, 29, 59)), "03:29:59 CEST")
				assert.False(t, e.Contains(mar(26, 1, 30, 0)), "03:30 CEST, exclusive end")

				assert.False(t, e.Contains(mar(26, 22, 59, 59)), "00:59:59 CEST, just before next start")
				assert.True(t, e.Contains(mar(26, 23, 0, 0)), "next day's 01:00 CEST")
				assert.True(t, e.Contains(mar(27, 0, 29, 59)), "next day's 02:29:59 CEST")
				assert.False(t, e.Contains(mar(27, 0, 30, 0)), "next day's 02:30 CEST, exclusive end")
			})

			t.Run("SpringForwardStartInGap", func(t *testing.T) {
				t.Parallel()

				// 02:30 does not exist on 2023-03-26. Per RFC 5545 section 3.3.10 (as amended by Erratum 4271[^1]),
				// such instances are handled as specified in section 3.3.5[^2], i.e. interpreted using the UTC
				// offset before the gap, and 02:30 CET becomes 03:30 CEST. The following day must resume at 02:30
				// and not drift to 03:30.
				//
				// [^1]: https://www.rfc-editor.org/errata/eid4271
				// [^2]: https://datatracker.ietf.org/doc/html/rfc5545#section-3.3.5
				e := dailyBerlinEntry(t, "2023-03-25 02:30:00", "2023-03-25 03:00:00")

				assert.False(t, e.Contains(mar(26, 0, 30, 0)), "01:30 CET, must not be shifted backwards")
				assert.False(t, e.Contains(mar(26, 1, 29, 59)), "03:29:59 CEST, before shifted start")
				assert.True(t, e.Contains(mar(26, 1, 30, 0)), "03:30 CEST, shifted start")
				assert.True(t, e.Contains(mar(26, 1, 59, 59)), "03:59:59 CEST")
				assert.False(t, e.Contains(mar(26, 2, 0, 0)), "04:00 CEST, exclusive end")

				assert.True(t, e.Contains(mar(27, 0, 30, 0)), "next day's 02:30 CEST, start")
				assert.False(t, e.Contains(mar(27, 1, 0, 0)), "next day's 03:00 CEST, exclusive end")
				assert.False(t, e.Contains(mar(27, 1, 30, 0)), "next day's 03:30 CEST, must not drift")

				assert.WithinDuration(t, mar(26, 1, 30, 0), e.NextTransition(mar(25, 12, 0, 0)), 0, "start at 03:30 CEST")
				assert.WithinDuration(t, mar(26, 2, 0, 0), e.NextTransition(mar(26, 1, 30, 0)), 0, "end at 04:00 CEST")
				assert.WithinDuration(t, mar(27, 0, 30, 0), e.NextTransition(mar(26, 2, 0, 0)), 0, "next day's 02:30 CEST")
				assert.WithinDuration(t, mar(27, 1, 0, 0), e.NextTransition(mar(27, 0, 30, 0)), 0, "next day's end at 03:00 CEST")
			})

			t.Run("FallBack", func(t *testing.T) {
				t.Parallel()

				// 2023-10-29: 03:00 CEST falls back to 02:00 CET (01:00 UTC), so 02:00-02:59 occurs twice.
				// The transition day occurrence starts at 00:30 CEST and ends 2h later at the *first* 02:30.
				e := dailyBerlinEntry(t, "2023-10-28 00:30:00", "2023-10-28 02:30:00")

				assert.False(t, e.Contains(oct(28, 22, 29, 59)), "00:29:59 CEST, just before transition day start")
				assert.True(t, e.Contains(oct(28, 22, 30, 0)), "00:30 CEST, transition day start")
				assert.True(t, e.Contains(oct(29, 0, 29, 59)), "first 02:29:59 CEST")
				assert.False(t, e.Contains(oct(29, 0, 30, 0)), "first 02:30 CEST, exclusive end")
				assert.False(t, e.Contains(oct(29, 1, 0, 0)), "second 02:00 CET")
				assert.False(t, e.Contains(oct(29, 1, 29, 59)), "second 02:29:59 CET")

				assert.True(t, e.Contains(oct(29, 23, 30, 0)), "next day's 00:30 CET, start")
				assert.True(t, e.Contains(oct(30, 1, 29, 59)), "next day's 02:29:59 CET")
				assert.False(t, e.Contains(oct(30, 1, 30, 0)), "next day's 02:30 CET, exclusive end")
			})

			t.Run("FallBackSpanningRepeatedHour", func(t *testing.T) {
				t.Parallel()

				// Configured as 01:30-03:30 (2h). On the transition day the 02:00-03:00 hour repeats,
				// so the fixed duration ends at 02:30 CET and 02:30-03:30 CET is NOT covered.
				e := dailyBerlinEntry(t, "2023-10-28 01:30:00", "2023-10-28 03:30:00")

				assert.True(t, e.Contains(oct(28, 23, 30, 0)), "01:30 CEST, start")
				assert.True(t, e.Contains(oct(29, 0, 30, 0)), "first 02:30 CEST")
				assert.True(t, e.Contains(oct(29, 1, 29, 59)), "second 02:29:59 CET")
				assert.False(t, e.Contains(oct(29, 1, 30, 0)), "second 02:30 CET, exclusive end (start + 2h)")
				assert.False(t, e.Contains(oct(29, 2, 0, 0)), "03:00 CET, beyond exclusive end")

				assert.False(t, e.Contains(oct(30, 0, 29, 59)), "next day's 01:29:59 CET, just before start")
				assert.True(t, e.Contains(oct(30, 0, 30, 0)), "next day's 01:30 CET, start")
				assert.True(t, e.Contains(oct(30, 2, 29, 59)), "next day's 03:29:59 CET")
				assert.False(t, e.Contains(oct(30, 2, 30, 0)), "next day's 03:30 CET, exclusive end")
			})

			t.Run("FallBackStartInRepeatedHour", func(t *testing.T) {
				t.Parallel()

				// 02:30 occurs twice on 2023-10-29. RFC 5545 3.3.5[^1] mandates the first (CEST) occurrence.
				// Note that time.Date() does not guarantee which of the two it returns for ambiguous times,
				// so the RRULE library has to disambiguate explicitly rather than rely on it.
				//
				// [^1]: https://datatracker.ietf.org/doc/html/rfc5545#section-3.3.5 (see FORM #3)
				e := dailyBerlinEntry(t, "2023-10-28 02:30:00", "2023-10-28 03:00:00")

				assert.False(t, e.Contains(oct(29, 0, 29, 59)), "first 02:29:59 CEST, just before start")
				assert.True(t, e.Contains(oct(29, 0, 30, 0)), "first 02:30 CEST, should be actual start")
				assert.True(t, e.Contains(oct(29, 0, 59, 59)), "first 02:59:59 CEST, before exclusive end")
				assert.False(t, e.Contains(oct(29, 1, 0, 0)), "second 02:00 CET, exclusive end (start + 30m)")
				assert.False(t, e.Contains(oct(29, 1, 30, 0)), "second 02:30 CET, must not drift")

				// The interval recurs daily from 02:30 to 03:00 Berlin time. From noon the previous day, the next
				// transition is its start at the first 02:30 (00:30 UTC, CEST). From that start, the next transition
				// is 30 minutes later at 01:00 UTC, which is 02:00 CET after the clock falls back.
				assert.WithinDuration(t, oct(29, 0, 30, 0), e.NextTransition(oct(28, 12, 0, 0)), 0, "first 02:30 CEST, should be the next transition")
				assert.WithinDuration(t, oct(29, 1, 0, 0), e.NextTransition(oct(29, 0, 30, 0)), 0, "second 02:00 CET, is the end of the first transition")

				assert.True(t, e.Contains(oct(30, 1, 30, 0)), "next day's 02:30 CET, actual start")
				assert.True(t, e.Contains(oct(30, 1, 59, 59)), "next day's 02:59:59 CET")
				assert.False(t, e.Contains(oct(30, 2, 0, 0)), "next day's 03:00 CET, exclusive end")
			})
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
			RRule:     types.MakeString("FREQ=DAILY"),
		}

		require.NoError(t, e.Init())

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

		t.Run("DSTSpringForward", func(t *testing.T) {
			t.Parallel()

			// 2023-03-26: 02:00 CET jumps to 03:00 CEST (01:00 UTC). The end is computed as start + duration
			// (1h30m), so on the transition day the wall-clock end shifts from 02:30 to 03:30 CEST and the next
			// transition should be shifted accordingly.
			e := dailyBerlinEntry(t, "2023-03-25 01:00:00", "2023-03-25 02:30:00")

			assert.WithinDuration(t, berlinTime("2023-03-25 02:30:00"), e.NextTransition(berlinTime("2023-03-25 01:00:00")), 0, "first recurrence end")
			assert.WithinDuration(t, mar(26, 1, 30, 0), e.NextTransition(mar(26, 0, 30, 0)), 0, "from 01:30 CET, shifted end at 03:30 CEST")
			assert.WithinDuration(t, mar(26, 1, 30, 0), e.NextTransition(mar(26, 1, 0, 0)), 0, "from 03:00 CEST, shifted end at 03:30 CEST")
			// At the shifted end (03:30 CEST), the next transition is the start on the following day.
			assert.WithinDuration(t, mar(26, 23, 0, 0), e.NextTransition(mar(26, 1, 30, 0)), 0, "next day's 01:00 CEST")
		})

		t.Run("DSTFallBack", func(t *testing.T) {
			t.Parallel()

			// 2023-10-29: 03:00 CEST falls back to 02:00 CET (01:00 UTC), so 02:00-02:59 occurs twice.
			// The transition day occurrence starts at 00:30 CEST and ends 2h later at the *first* 02:30.
			//
			// After the transition day end, the repeated hour must not produce another transition, so the
			// next one is the start on the following day.
			e := dailyBerlinEntry(t, "2023-10-28 00:30:00", "2023-10-28 02:30:00")

			assert.WithinDuration(t, oct(28, 22, 30, 0), e.NextTransition(oct(28, 12, 0, 0)), 0, "from 14:00 CEST the day before, transition day start at 00:30 CEST")
			assert.WithinDuration(t, oct(29, 0, 30, 0), e.NextTransition(oct(28, 22, 30, 0)), 0, "from 00:30 CEST, transition day end at first 02:30 CEST")
			assert.WithinDuration(t, oct(29, 0, 30, 0), e.NextTransition(oct(29, 0, 29, 59)), 0, "from first 02:29:59 CEST, transition day end at first 02:30 CEST")

			assert.WithinDuration(t, oct(29, 23, 30, 0), e.NextTransition(oct(29, 0, 30, 0)), 0, "from first 02:30 CEST, next day's start at 00:30 CET")
			assert.WithinDuration(t, oct(29, 23, 30, 0), e.NextTransition(oct(29, 1, 30, 0)), 0, "from second 02:30 CET, next day's start at 00:30 CET")
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
				RRule:     types.MakeString("FREQ=DAILY"),
			}},
		}

		for _, e := range p.Entries {
			require.NoError(t, e.Init())
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
					RRule:     types.MakeString("FREQ=HOURLY;BYHOUR=1,3,5,7,9,11,13,15"),
				},
				{
					StartTime: types.UnixMilli(berlinTime("2023-03-27 08:00:00")),
					EndTime:   types.UnixMilli(berlinTime("2023-03-27 08:30:00")),
					Timezone:  berlin,
					RRule:     types.MakeString("FREQ=HOURLY;BYHOUR=0,2,4,6,8,10,12,14"),
				},
			},
		}

		for _, e := range p.Entries {
			require.NoError(t, e.Init())
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

// dailyBerlinEntry returns an initialized FREQ=DAILY entry in Europe/Berlin with the given first start and end time.
func dailyBerlinEntry(t *testing.T, start, end string) *timeperiod.Entry {
	e := &timeperiod.Entry{
		StartTime: types.UnixMilli(berlinTime(start)),
		EndTime:   types.UnixMilli(berlinTime(end)),
		Timezone:  berlin,
		RRule:     types.MakeString("FREQ=DAILY"),
	}
	require.NoError(t, e.Init())

	return e
}

// mar returns the given time on a day in March 2023 in UTC, i.e. around the Europe/Berlin spring-forward transition.
func mar(day, hour, minute, second int) time.Time {
	return time.Date(2023, time.March, day, hour, minute, second, 0, time.UTC)
}

// oct returns the given time on a day in October 2023 in UTC, i.e. around the Europe/Berlin fall-back transition.
func oct(day, hour, minute, second int) time.Time {
	return time.Date(2023, time.October, day, hour, minute, second, 0, time.UTC)
}

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
