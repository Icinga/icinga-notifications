package channel

import (
	"context"
	"errors"

	"github.com/icinga/icinga-go-library/notifications/event"
	"github.com/icinga/icinga-go-library/notifications/plugin"
	"github.com/icinga/icinga-notifications/internal/config/baseconf"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type Channel struct {
	baseconf.IncrementalPkDbEntry[int64] `db:",inline"`

	Name   string `db:"name"`
	Type   string `db:"type"`
	Config string `db:"config" json:"-"` // excluded from JSON config dump as this may contain sensitive information

	Logger *zap.SugaredLogger `db:"-"`

	restartCh chan newConfig
	pluginCh  chan *Plugin

	pluginCtx       context.Context
	pluginCtxCancel func()
}

// MarshalLogObject implements the zapcore.ObjectMarshaler interface.
func (c *Channel) MarshalLogObject(encoder zapcore.ObjectEncoder) error {
	encoder.AddInt64("id", c.ID)
	encoder.AddString("name", c.Name)
	encoder.AddString("type", c.Type)
	return nil
}

// IncrementalInitAndValidate implements the config.IncrementalConfigurableInitAndValidatable interface.
func (c *Channel) IncrementalInitAndValidate() error {
	return ValidateType(c.Type)
}

// newConfig helps to store the channel's updated properties
type newConfig struct {
	ctype  string
	config string
}

// Start initializes the channel and starts the plugin in the background
func (c *Channel) Start(ctx context.Context, logger *zap.SugaredLogger) {
	c.Logger = logger.With(zap.Object("channel", c))
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
func (c *Channel) Restart(logger *zap.SugaredLogger) {
	c.Logger = logger.With(zap.Object("channel", c))
	c.Logger.Info("Restarting the channel plugin due to a config change")
	c.restartCh <- newConfig{c.Type, c.Config}
}

// Notify sends the provided notification request using the channel's plugin.
//
// Returns an error if the provided request is invalid or if sending the notification failed.
func (c *Channel) Notify(req *plugin.NotificationRequest) error {
	if req.Event == nil {
		return errors.New("invalid notification request: Event is nil")
	}
	if req.Object == nil {
		return errors.New("invalid notification request: Object is nil")
	}
	if req.Contact == nil {
		return errors.New("invalid notification request: Contact is nil")
	}
	if req.Incident == nil && req.Event.Type == event.TypeState {
		return errors.New("invalid notification request: cannot send state notification without an incident")
	}

	if p := c.getPlugin(); p != nil {
		return p.SendNotification(req)
	}
	return errors.New("plugin could not be started")
}
