package channel

import (
	"github.com/icinga/noma/internal/event"
	"github.com/icinga/noma/internal/incident"
	"github.com/icinga/noma/internal/recipient"
	"log"
)

type EMail struct {
}

func NewEMail(config string) (Plugin, error) {
	return new(EMail), nil
}

func (e *EMail) Send(contact *recipient.Contact, incident *incident.Incident, event *event.Event) error {
	log.Printf("email: contact=%v incident=%v event=%v", contact, incident, event)
	return nil
}
