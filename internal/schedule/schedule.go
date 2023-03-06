package schedule

import (
	"github.com/icinga/noma/internal/contact"
	"github.com/icinga/noma/internal/timeperiod"
)

type Schedule struct {
	Name    string
	Members []*Member
}

type Member struct {
	TimePeriod   *timeperiod.TimePeriod
	Contact      *contact.Contact
	ContactGroup *contact.Group
}
