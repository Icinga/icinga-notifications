package channel

import (
	"encoding/json"
	"github.com/icinga/noma/internal/event"
	"github.com/icinga/noma/internal/incident"
	"github.com/icinga/noma/internal/recipient"
	"log"
)

type RocketChat struct {
	config struct {
		URL    string `json:"url"`
		UserID string `json:"user_id"`
		Token  string `json:"token"`
	}
}

func NewRocketChat(config string) (Plugin, error) {
	r := new(RocketChat)

	err := json.Unmarshal([]byte(config), &r.config)
	if err != nil {
		return nil, err
	}

	return r, nil
}

func (r *RocketChat) Send(contact *recipient.Contact, incident *incident.Incident, event *event.Event) error {
	log.Printf("rocketchat: contact=%v incident=%v event=%v", contact, incident, event)
	return nil
}
