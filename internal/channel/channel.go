package channel

import (
	"fmt"
	"github.com/icinga/icinga-notifications/internal/event"
	"github.com/icinga/icinga-notifications/internal/incident"
	"github.com/icinga/icinga-notifications/internal/recipient"
)

type Channel struct {
	ID     int64  `db:"id"`
	Name   string `db:"name"`
	Type   string `db:"type"`
	Config string `db:"config" json:"-"` // excluded from JSON config dump as this may contain sensitive information

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

func (c *Channel) ResetPlugin() {
	c.plugin = nil
}

type Plugin interface {
	Send(contact *recipient.Contact, incident *incident.Incident, event *event.Event, icingaweb2Url string) error
}

type NewFunc func(config string) (Plugin, error)

var Channels = map[string]NewFunc{
	"email":      NewEMail,
	"rocketchat": NewRocketChat,
}
