package timeperiod

import (
	"time"
)

var (
	OfficeHours = &TimePeriod{
		Name: "Office Hours",
		// Monday-Friday 9:00-17:00
		Entries: func() []*Entry {
			monday := time.Date(2023, 2, 27, 0, 0, 0, 0, time.Local)
			var entries []*Entry
			for i := 0; i < 5; i++ {
				entries = append(entries, &Entry{
					Start:       monday.Add(time.Duration(i)*24*time.Hour + 9*time.Hour),
					End:         monday.Add(time.Duration(i)*24*time.Hour + 17*time.Hour),
					RepeatEvery: 7 * 24 * time.Hour, // DST says no, but close enough, that's why we'll use RRULE later
				})
			}
			return entries
		}(),
	}
	EveryEvenHour = &TimePeriod{
		Name: "Every Even Hour",
		Entries: []*Entry{{
			Start:       time.Date(2023, 3, 1, 0, 0, 0, 0, time.Local),
			End:         time.Date(2023, 3, 1, 1, 0, 0, 0, time.Local),
			RepeatEvery: 2 * time.Hour,
		}},
	}
	EveryOddHour = &TimePeriod{
		Name: "Every Odd Hour",
		Entries: []*Entry{{
			Start:       time.Date(2023, 3, 1, 1, 0, 0, 0, time.Local),
			End:         time.Date(2023, 3, 1, 2, 0, 0, 0, time.Local),
			RepeatEvery: 2 * time.Hour,
		}},
	}
	EveryEvenMinute = &TimePeriod{
		Name: "Every Even Minute",
		Entries: []*Entry{{
			Start:       time.Date(2023, 3, 1, 0, 0, 0, 0, time.Local),
			End:         time.Date(2023, 3, 1, 0, 1, 0, 0, time.Local),
			RepeatEvery: 2 * time.Minute,
		}},
	}
	EveryOddMinute = &TimePeriod{
		Name: "Every Odd Minute",
		Entries: []*Entry{{
			Start:       time.Date(2023, 3, 1, 0, 1, 0, 0, time.Local),
			End:         time.Date(2023, 3, 1, 0, 2, 0, 0, time.Local),
			RepeatEvery: 2 * time.Minute,
		}},
	}
	Always = &TimePeriod{
		Name: "Always",
		Entries: []*Entry{{
			Start:       time.Date(2023, 3, 1, 0, 0, 0, 0, time.Local),
			End:         time.Date(2023, 3, 2, 0, 0, 0, 0, time.Local),
			RepeatEvery: 24 * time.Hour,
		}},
	}
	Never = &TimePeriod{
		Name:    "Never",
		Entries: nil,
	}
	FarInThePast = &TimePeriod{
		Name: "Far in the Past",
		Entries: []*Entry{{
			Start: time.Date(2000, 1, 1, 0, 0, 0, 0, time.Local),
			End:   time.Date(2000, 1, 2, 0, 0, 0, 0, time.Local),
		}},
	}
	FarInTheFuture = &TimePeriod{
		Name: "Far in the Future",
		Entries: []*Entry{{
			Start: time.Date(2070, 1, 1, 0, 0, 0, 0, time.Local),
			End:   time.Date(2070, 1, 2, 0, 0, 0, 0, time.Local),
		}},
	}

	TimePeriods = []*TimePeriod{
		OfficeHours,
		EveryEvenHour,
		EveryOddHour,
		EveryEvenMinute,
		EveryOddMinute,
		Always,
		Never,
		FarInThePast,
		FarInTheFuture,
	}
)
