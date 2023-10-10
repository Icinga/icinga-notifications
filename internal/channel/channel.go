package channel

import (
	"errors"
	"fmt"
	"github.com/icinga/icinga-notifications/internal/contracts"
	"github.com/icinga/icinga-notifications/internal/event"
	"github.com/icinga/icinga-notifications/internal/recipient"
	"github.com/icinga/icinga-notifications/pkg/plugin"
	"go.uber.org/zap"
	"net/url"
)

type Channel struct {
	ID     int64  `db:"id"`
	Name   string `db:"name"`
	Type   string `db:"type"`
	Config string `db:"config" json:"-"` // excluded from JSON config dump as this may contain sensitive information

	Logger *zap.SugaredLogger

	newConfigCh    chan struct{}
	stopPluginCh   chan struct{}
	notificationCh chan Req
}

// Req is a wrapper for plugin.NotificationRequest. It provides an errCh channel for Req state.
// Any error that occurs while processing plugin.NotificationRequest should be sent to this channel
// to mark Req as failed.If errCh receives nil, Req was successful.
type Req struct {
	req   *plugin.NotificationRequest
	errCh chan<- error
}

// Start initializes the channel and starts the plugin in the background
func (c *Channel) Start(logger *zap.SugaredLogger) {
	c.Logger = logger
	c.newConfigCh = make(chan struct{})
	c.stopPluginCh = make(chan struct{})
	c.notificationCh = make(chan Req, 1)

	go c.runPlugin()
}

// initPlugin returns a new Plugin or nil if an error occurred during initialization
func (c *Channel) initPlugin() *Plugin {
	c.Logger.Debug("Initializing channel plugin")

	p, err := NewPlugin(c.Type, c.Logger)
	if err != nil {
		c.Logger.Errorw("Failed to initialize channel plugin", zap.Error(err))
		return nil
	}

	if err := p.SetConfig(c.Config); err != nil {
		c.Logger.Errorw("Failed to set channel plugin config", zap.Error(err))
		p.Stop()
		return nil
	}

	return p
}

// runPlugin is called as go routine to initialize and maintain the plugin by receiving signals on given chan(s)
func (c *Channel) runPlugin() {
	var currentlyRunningPlugin *Plugin

	// Helper function for the following loop to stop a running plugin. Does nothing if no plugin is running.
	stopIfRunning := func() {
		if currentlyRunningPlugin != nil {
			currentlyRunningPlugin.Stop()
			currentlyRunningPlugin = nil
		}
	}

	// Helper function for the following loop to receive from rpc.Done
	rpcDone := func() <-chan struct{} {
		if currentlyRunningPlugin != nil {
			return currentlyRunningPlugin.rpc.Done()
		}

		return nil
	}

	for {
		if currentlyRunningPlugin == nil {
			currentlyRunningPlugin = c.initPlugin()
		}

		select {
		case <-rpcDone():
			c.Logger.Debug("rpc.Done(): Restarting plugin")
			stopIfRunning()

			continue
		case <-c.newConfigCh:
			c.Logger.Debug("Plugin.ReloadConfig() triggered")
			stopIfRunning()

			continue
		case <-c.stopPluginCh:
			c.Logger.Debug("Stopping the channel plugin")
			stopIfRunning()

			return
		case req := <-c.notificationCh:
			if currentlyRunningPlugin == nil {
				currentlyRunningPlugin = c.initPlugin()
			}

			if currentlyRunningPlugin == nil {
				c.Logger.Debug("Cannot send notification, plugin could not be started")
				req.errCh <- errors.New("plugin could not be started")
			} else {
				go func(p *Plugin) {
					// the return can take time
					req.errCh <- p.SendNotification(req.req)
				}(currentlyRunningPlugin)
			}
		}
	}
}

// Stop ends the lifecycle of its plugin.
// This should only be called when the channel is not more required.
// Multiple calls on same channel cause panic
func (c *Channel) Stop() {
	close(c.stopPluginCh)
}

// ReloadConfig sends a signal to reload the channel plugin config
func (c *Channel) ReloadConfig() {
	c.newConfigCh <- struct{}{}
}

// Notify prepares and sends the notification request, returns a non-error on fails, nil on success
func (c *Channel) Notify(contact *recipient.Contact, i contracts.Incident, ev *event.Event, icingaweb2Url string) error {
	contactStruct := &plugin.Contact{FullName: contact.FullName}
	for _, addr := range contact.Addresses {
		contactStruct.Addresses = append(contactStruct.Addresses, &plugin.Address{Type: addr.Type, Address: addr.Address})
	}

	icingaweb2Url, _ = url.JoinPath(icingaweb2Url, "/")
	req := &plugin.NotificationRequest{
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

	errCh := make(chan error, 1)
	c.notificationCh <- Req{req: req, errCh: errCh}

	return <-errCh
}
