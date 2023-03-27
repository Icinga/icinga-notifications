package schedule

import (
	"github.com/icinga/noma/internal/contact"
	"github.com/icinga/noma/internal/timeperiod"
	"time"
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

// GetContactsAt returns the contacts that are active in the schedule at the given time.
func (s *Schedule) GetContactsAt(t time.Time) []*contact.Contact {
	var contacts []*contact.Contact

	for _, m := range s.Members {
		if m.TimePeriod.Contains(t) {
			if m.Contact != nil {
				contacts = append(contacts, m.Contact)
			}

			if m.ContactGroup != nil {
				contacts = append(contacts, m.ContactGroup.Members...)
			}
		}
	}

	return contacts
}

var _ contact.Recipient = (*Schedule)(nil)
