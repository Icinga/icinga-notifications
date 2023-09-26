package channel

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/icinga/icinga-notifications/internal/contracts"
	"github.com/icinga/icinga-notifications/internal/event"
	"github.com/icinga/icinga-notifications/internal/recipient"
	"github.com/icinga/icinga-notifications/pkg/plugin"
)

func (c *Channel) GetInfo() (*plugin.Info, error) {
	result, callErr := c.rpc.Call("GetInfo", nil)
	if err := c.handleRpcCallErr(callErr); err != nil {
		return nil, err
	}

	info := &plugin.Info{}
	err := json.Unmarshal(result, info)
	if err != nil {
		return nil, err
	}

	return info, nil
}

func (c *Channel) SetConfig(config string) error {
	_, err := c.rpc.Call("SetConfig", json.RawMessage(config))

	return c.handleRpcCallErr(err)
}

func (c *Channel) SendNotification(contact *recipient.Contact, i contracts.Incident, ev *event.Event, icingaweb2Url string) error {
	contactStruct := &plugin.Contact{FullName: contact.FullName}
	for _, addr := range contact.Addresses {
		contactStruct.Addresses = append(contactStruct.Addresses, &plugin.Address{Type: addr.Type, Address: addr.Address})
	}

	req := plugin.NotificationRequest{
		Contact:       contactStruct,
		Incident:      plugin.Incident{Id: i.ID(), ObjectDisplayName: i.ObjectDisplayName()},
		Event:         &plugin.Event{Time: ev.Time, URL: ev.URL, Type: ev.Type, Severity: ev.Severity.String(), Username: ev.Username, Message: ev.Message},
		IcingaWeb2Url: icingaweb2Url,
	}

	params, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to prepare request params: %w", err)
	}

	_, err = c.rpc.Call("SendNotification", params)

	return c.handleRpcCallErr(err)
}

func (c *Channel) handleRpcCallErr(err error) error {
	if err != nil {
		var rpcErr *RPCError

		if errors.As(err, &rpcErr) {
			c.ResetPlugin()
		}

		return fmt.Errorf("call failed: %w", err)
	}

	return nil
}
