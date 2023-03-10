package rule

import (
	"github.com/icinga/noma/internal/contact"
	"github.com/icinga/noma/internal/schedule"
)

type Escalation struct {
	Name      string
	Condition *Condition
	Fallbacks []*Escalation

	ChannelType   string
	Contacts      []*contact.Contact
	ContactGroups []*contact.Group
	Schedules     []*schedule.Schedule
}
