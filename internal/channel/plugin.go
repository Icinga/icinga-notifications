package channel

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/icinga/icinga-notifications/internal/contracts"
	"github.com/icinga/icinga-notifications/internal/event"
	"github.com/icinga/icinga-notifications/internal/recipient"
	"github.com/icinga/icinga-notifications/pkg/plugin"
	"os/exec"
)

type Plugin struct {
	cmd *exec.Cmd
	rpc *RPC
}

func (p *Plugin) GetInfo() (*plugin.Info, error) {
	result, err := p.rpc.Call("GetInfo", nil)
	if err != nil {
		if errors.Is(err, ErrRpcFailed) {
			// reset and resart plugin
		}

		return nil, fmt.Errorf("call failed: %w", err)
	}

	info := &plugin.Info{}
	err = json.Unmarshal(result, info)
	if err != nil {
		return nil, err
	}

	return info, nil
}

func (p *Plugin) SetConfig(config string) error {
	if _, err := p.rpc.Call("SetConfig", json.RawMessage(config)); err != nil {
		return fmt.Errorf("call failed: %w", err)
	}

	return nil
}

func (p *Plugin) SendNotification(contact *recipient.Contact, i contracts.Incident, ev *event.Event, icingaweb2Url string) error {
	c := &plugin.Contact{FullName: contact.FullName}
	for _, addr := range contact.Addresses {
		c.Addresses = append(c.Addresses, &plugin.Address{Type: addr.Type, Address: addr.Address})
	}

	req := plugin.NotificationRequest{
		Contact:       c,
		Incident:      plugin.Incident{Id: i.ID(), ObjectDisplayName: i.ObjectDisplayName()},
		Event:         &plugin.Event{Time: ev.Time, URL: ev.URL, Type: ev.Type, Severity: ev.Severity.String(), Username: ev.Username, Message: ev.Message},
		IcingaWeb2Url: icingaweb2Url,
	}

	params, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to encode request into json: %w", err)
	}

	_, err = p.rpc.Call("SendNotification", params)
	if err != nil {
		return fmt.Errorf("call failed: %w", err)
	}

	return nil
}
