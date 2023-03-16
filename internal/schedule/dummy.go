package schedule

import (
	"github.com/icinga/noma/internal/contact"
	"github.com/icinga/noma/internal/timeperiod"
)

var OnCall = &Schedule{
	Name: "On Call",
	Members: []*Member{{
		TimePeriod: timeperiod.EveryEvenHour,
		Contact:    contact.John,
	}, {
		TimePeriod: timeperiod.EveryOddHour,
		Contact:    contact.Jane,
	}},
}
