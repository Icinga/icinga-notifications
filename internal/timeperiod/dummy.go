package timeperiod

import (
	"time"
)

var (
	OfficeHours = &TimePeriod{
		Name: "Office Hours",
		// Monday-Friday 9:00-17:00
		Entries: []*Entry{{
			Start:          time.Date(2023, 2, 27, 9, 0, 0, 0, time.Local),
			End:            time.Date(2023, 2, 27, 17, 0, 0, 0, time.Local),
			RecurrenceRule: "FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR",
		}},
	}
	EveryEvenHour = &TimePeriod{
		Name: "Every Even Hour",
		Entries: []*Entry{{
			Start:          time.Date(2023, 3, 1, 0, 0, 0, 0, time.Local),
			End:            time.Date(2023, 3, 1, 1, 0, 0, 0, time.Local),
			RecurrenceRule: "FREQ=HOURLY;BYHOUR=0,2,4,6,8,10,12,14,16,18,20,22",
		}},
	}
	EveryOddHour = &TimePeriod{
		Name: "Every Odd Hour",
		Entries: []*Entry{{
			Start:          time.Date(2023, 3, 1, 1, 0, 0, 0, time.Local),
			End:            time.Date(2023, 3, 1, 2, 0, 0, 0, time.Local),
			RecurrenceRule: "FREQ=HOURLY;BYHOUR=1,3,5,7,9,11,13,15,17,19,21,23",
		}},
	}
	EveryEvenMinute = &TimePeriod{
		Name: "Every Even Minute",
		Entries: []*Entry{{
			Start:          time.Date(2023, 3, 1, 0, 0, 0, 0, time.Local),
			End:            time.Date(2023, 3, 1, 0, 1, 0, 0, time.Local),
			RecurrenceRule: "FREQ=MINUTELY;BYMINUTE=0,2,4,6,8,10,12,14,16,18,20,22,24,26,28,30,32,34,36,38,40,42,44,46,48,50,52,54,56,58",
		}},
	}
	EveryOddMinute = &TimePeriod{
		Name: "Every Odd Minute",
		Entries: []*Entry{{
			Start:          time.Date(2023, 3, 1, 0, 1, 0, 0, time.Local),
			End:            time.Date(2023, 3, 1, 0, 2, 0, 0, time.Local),
			RecurrenceRule: "FREQ=MINUTELY;BYMINUTE=1,3,5,7,9,11,13,15,17,19,21,23,25,27,29,31,33,35,37,39,41,43,45,47,49,51,53,55,57,59",
		}},
	}
	Always = &TimePeriod{
		Name: "Always",
		Entries: []*Entry{{
			Start:          time.Date(2023, 3, 1, 0, 0, 0, 0, time.Local),
			End:            time.Date(2023, 3, 2, 0, 0, 0, 0, time.Local),
			RecurrenceRule: "FREQ=DAILY",
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
