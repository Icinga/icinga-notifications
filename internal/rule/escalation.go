package rule

import (
	"github.com/icinga/noma/internal/contact"
	"github.com/icinga/noma/internal/schedule"
	"strings"
	"time"
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

func (e *Escalation) DisplayName() string {
	if e.Name != "" {
		return e.Name
	}

	var recipients []string

	for _, c := range e.Contacts {
		recipients = append(recipients, "[C] "+c.FullName)
	}
	for _, g := range e.ContactGroups {
		recipients = append(recipients, "[G] "+g.Name)
	}
	for _, s := range e.Schedules {
		recipients = append(recipients, "[S] "+s.Name)
	}

	if len(recipients) == 0 {
		return "(no recipients)"
	}

	return strings.Join(recipients, ", ")
}

func (e *Escalation) GetContactsAt(t time.Time) []*contact.Contact {
	var contacts []*contact.Contact

	contacts = append(contacts, e.Contacts...)

	for _, g := range e.ContactGroups {
		contacts = append(contacts, g.Members...)
	}

	for _, s := range e.Schedules {
		contacts = append(contacts, s.GetContactsAt(t)...)
	}

	return contacts
}
