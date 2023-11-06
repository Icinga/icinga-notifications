package channel

import (
	"context"
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

	restartCh chan newConfig
	pluginCh  chan *Plugin

	pluginCtx       context.Context
	pluginCtxCancel func()
}

// newConfig helps to store the channel's updated properties
type newConfig struct {
	ctype  string
	config string
}

// Start initializes the channel and starts the plugin in the background
func (c *Channel) Start(ctx context.Context, logger *zap.SugaredLogger) {
	c.Logger = logger
	c.restartCh = make(chan newConfig)
	c.pluginCh = make(chan *Plugin)
	c.pluginCtx, c.pluginCtxCancel = context.WithCancel(ctx)

	go c.runPlugin(c.Type, c.Config)
}

// initPlugin returns a new Plugin or nil if an error occurred during initialization
func (c *Channel) initPlugin(cType string, config string) *Plugin {
	c.Logger.Debug("Initializing channel plugin")

	p, err := NewPlugin(cType, c.Logger)
	if err != nil {
		c.Logger.Errorw("Failed to initialize channel plugin", zap.Error(err))
		return nil
	}

	if err := p.SetConfig(config); err != nil {
		c.Logger.Errorw("Failed to set channel plugin config, terminating the plugin", zap.Error(err))
		p.Stop()
		return nil
	}

	p.logger.Info("Successfully started channel plugin")

	return p
}

// runPlugin is called as go routine to initialize and maintain the plugin by receiving signals on given chan(s)
func (c *Channel) runPlugin(initType string, initConfig string) {
	var currentlyRunningPlugin *Plugin
	cType, config := initType, initConfig
	// Helper function for the following loop to stop a running plugin. Does nothing if no plugin is running.
	stopIfRunning := func() (int, bool) {
		if currentlyRunningPlugin != nil {
			pid := currentlyRunningPlugin.Pid()
			currentlyRunningPlugin.Stop()
			currentlyRunningPlugin = nil
			return pid, true
		}

		return 0, false
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
			currentlyRunningPlugin = c.initPlugin(cType, config)
		}

		select {
		case <-rpcDone():
			if pid, stopped := stopIfRunning(); stopped {
				c.Logger.Warnw("Channel plugin crashed", zap.Int("pid", pid))
			}

			continue
		case reload := <-c.restartCh:
			cType, config = reload.ctype, reload.config
			stopIfRunning()

			continue
		case <-c.pluginCtx.Done():
			if pid, stopped := stopIfRunning(); stopped {
				c.Logger.Infow("Successfully stopped channel plugin", zap.Int("pid", pid))
			}

			return
		case c.pluginCh <- currentlyRunningPlugin:
		}
	}
}

// getPlugin returns a fully initialized plugin that can be used for sending notifications. If there
// currently is no such plugin, for example because starting it failed, nil is returned instead.
func (c *Channel) getPlugin() *Plugin {
	p := <-c.pluginCh
	if p == nil {
		// The above receive might have woken runPlugin after the select was blocked for a long time.
		// In that case, a second receive gives it another chance to successfully start the plugin.
		p = <-c.pluginCh
	}

	return p
}

// Stop ends the lifecycle of its plugin.
// This should only be called when the channel is not more required.
func (c *Channel) Stop() {
	c.pluginCtxCancel()
}

// Restart signals to restart the channel plugin with the updated channel config
func (c *Channel) Restart() {
	c.Logger.Info("Restarting the channel plugin due to a config change")
	c.restartCh <- newConfig{c.Type, c.Config}
}

// Notify prepares and sends the notification request, returns a non-error on fails, nil on success
func (c *Channel) Notify(contact *recipient.Contact, i contracts.Incident, ev *event.Event, icingaweb2Url string) error {
	p := c.getPlugin()
	if p == nil {
		return errors.New("plugin could not be started")
	}

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

	return p.SendNotification(req)
}
