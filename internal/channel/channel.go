package channel

import (
	"fmt"
	"github.com/icinga/noma/internal/event"
	"github.com/icinga/noma/internal/incident"
	"github.com/icinga/noma/internal/recipient"
)

type Channel struct {
	Name   string
	Type   string
	Config string

	plugin Plugin
}

func (c *Channel) GetPlugin() (Plugin, error) {
	if c.plugin == nil {
		f := Channels[c.Type]
		if f == nil {
			return nil, fmt.Errorf("unknown channel type %q", c.Type)
		}

		p, err := f(c.Config)
		if err != nil {
			return nil, fmt.Errorf("error initializing channel type %q: %w", c.Type, err)
		}

		c.plugin = p
	}

	return c.plugin, nil
}

type Plugin interface {
	Send(contact *recipient.Contact, incident *incident.Incident, event *event.Event) error
}

type NewFunc func(config string) (Plugin, error)

var Channels = map[string]NewFunc{
	"email":      NewEMail,
	"rocketchat": NewRocketChat,
}