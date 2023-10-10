package channel

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/icinga/icinga-notifications/internal/contracts"
	"github.com/icinga/icinga-notifications/internal/event"
	"github.com/icinga/icinga-notifications/internal/recipient"
	"github.com/icinga/icinga-notifications/pkg/plugin"
	"github.com/icinga/icinga-notifications/pkg/rpc"
	"strings"
)

func (c *Channel) GetInfo() (*plugin.Info, error) {
	result, err := c.rpcCall(plugin.MethodGetInfo, nil)
	if err != nil {
		return nil, err
	}

	info := &plugin.Info{}
	err = json.Unmarshal(result, info)
	if err != nil {
		return nil, err
	}

	return info, nil
}

func (c *Channel) SetConfig(config string) error {
	_, err := c.rpcCall(plugin.MethodSetConfig, json.RawMessage(config))

	return err
}

func (c *Channel) SendNotification(contact *recipient.Contact, i contracts.Incident, ev *event.Event, icingaweb2Url string) error {
	contactStruct := &plugin.Contact{FullName: contact.FullName}
	for _, addr := range contact.Addresses {
		contactStruct.Addresses = append(contactStruct.Addresses, &plugin.Address{Type: addr.Type, Address: addr.Address})
	}

	if !strings.HasSuffix(icingaweb2Url, "/") {
		icingaweb2Url += "/"
	}

	req := plugin.NotificationRequest{
		Contact: contactStruct,
		Object: &plugin.Object{
			Name:      i.ObjectDisplayName(),
			Url:       ev.URL,
			Tags:      ev.Tags,
			ExtraTags: ev.ExtraTags,
		},
		Incident: &plugin.Incident{
			Id:  i.ID(),
			Url: fmt.Sprintf("%snotifications/incident?id=%d", icingaweb2Url, i.ID()),
		},
		Event: &plugin.Event{
			Time:     ev.Time,
			Type:     ev.Type,
			Severity: ev.Severity.String(),
			Username: ev.Username,
			Message:  ev.Message,
		},
	}

	params, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to prepare request params: %w", err)
	}

	_, err = c.rpcCall(plugin.MethodSendNotification, params)

	return err
}

func (c *Channel) rpcCall(method string, params json.RawMessage) (json.RawMessage, error) {
	result, err := c.rpc.Call(method, params)

	var rpcErr *rpc.Error
	if errors.As(err, &rpcErr) {
		c.Stop()
	}

	return result, err
}
