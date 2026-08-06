package channel

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/icinga/icinga-go-library/database"
	"github.com/icinga/icinga-go-library/notifications/jsonrpc"
	"github.com/icinga/icinga-go-library/notifications/plugin"
	"github.com/icinga/icinga-notifications/internal/config/baseconf"
	"github.com/icinga/icinga-notifications/internal/contracts"
	"github.com/icinga/icinga-notifications/internal/event"
	"github.com/icinga/icinga-notifications/internal/object"
	"github.com/icinga/icinga-notifications/internal/recipient"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	// ErrChannelDeleted is used as a cancellation cause when the channel is deleted, allowing the
	// plugin control loop to clean up its state in the database before exiting.
	ErrChannelDeleted = errors.New("channel deleted")
)

type Channel struct {
	baseconf.IncrementalPkDbEntry[int64] `db:",inline"`

	Name   string `db:"name"`
	Type   string `db:"type"`
	Config string `db:"config" json:"-"` // excluded from JSON config dump as this may contain sensitive information

	Logger *zap.SugaredLogger `db:"-"`
	db     *database.DB

	restartCh chan newConfig
	pluginCh  chan *pluginSupervisor

	pluginCtx       context.Context
	pluginCtxCancel context.CancelCauseFunc
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

// Start initializes the channel and starts the plugin control loop in a separate goroutine.
//
// It should be called after the channel has been created and its properties have been set.
// The provided context is used to manage the lifecycle of the plugin control loop, and the
// logger is used for logging messages related to the channel and its plugin.
func (c *Channel) Start(ctx context.Context, db *database.DB, logger *zap.SugaredLogger) {
	c.Logger = logger.With(zap.Object("channel", c))
	c.db = db
	c.restartCh = make(chan newConfig)
	c.pluginCh = make(chan *pluginSupervisor)
	c.pluginCtx, c.pluginCtxCancel = context.WithCancelCause(ctx)

	// #nosec G118 -- The goroutine uses a background ctx with timeout after c.pluginCtx is canceled to perform a DB cleanup.
	go c.pluginControlLoop(newConfig{c.Type, c.Config})
}

// instantiatePluginSupervisor initializes a new pluginSupervisor for the channel's plugin type and configuration.
func (c *Channel) instantiatePluginSupervisor(cType string, config string) *pluginSupervisor {
	c.Logger.Debug("Initializing channel plugin")

	p, err := newPluginSupervisor(c.pluginCtx, c.db, c.Logger, cType, c.ID)
	if err != nil {
		c.Logger.Errorw("Failed to initialize channel plugin", zap.Error(err))
		return nil
	}

	if err := p.SetConfig(c.pluginCtx, config); err != nil {
		c.Logger.Errorw("Failed to set channel plugin config, terminating the plugin", zap.Error(err))
		p.Stop()
		return nil
	}

	p.logger.Info("Successfully started channel plugin")

	return p
}

// pluginControlLoop runs in a separate goroutine and manages the lifecycle of the channel plugin.
//
// It handles plugin restarts, configuration reloads, and unexpected plugin crashes.
// Returns only when the plugin's context is canceled via Stop().
func (c *Channel) pluginControlLoop(currentConfig newConfig) {
	var current *pluginSupervisor
	rpcDone := func() <-chan struct{} {
		if current != nil {
			return current.rpc.Done()
		}
		return nil
	}

	stopReset := func() {
		if current != nil {
			current.Stop()
			current = nil
		}
	}
	defer stopReset()
	defer close(c.pluginCh)

	for {
		if current == nil {
			current = c.instantiatePluginSupervisor(currentConfig.ctype, currentConfig.config)
		}

		select {
		case c.pluginCh <- current:
		case <-c.pluginCtx.Done():
			if errors.Is(context.Cause(c.pluginCtx), ErrChannelDeleted) {
				c.Logger.Info("Channel has been deleted, purging its plugin state from the database")
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				if err := deleteByChannelID(ctx, c.db, c.ID); err != nil {
					c.Logger.Warnw("Failed to purge channel plugin state from the database", zap.Error(err))
				}
				cancel()
			}
			return

		case <-rpcDone(): // Plugin crashed??
			c.Logger.Warnw("Channel plugin stopped unexpectedly, restarting it", zap.Int("pid", current.cmd.Process.Pid))
			stopReset()

		case newConf := <-c.restartCh:
			// The RPC connection is safe for concurrent use, so we can try to reload the config without going
			// through the whole plugin restart process. Though, if the plugin type has changed entirely, then
			// we definitely need to stop the current plugin and start a new one.
			if current != nil && currentConfig.ctype == newConf.ctype {
				c.Logger.Infow("Reloading channel plugin config on the fly", zap.Int("pid", current.cmd.Process.Pid))
				if err := current.SetConfig(c.pluginCtx, newConf.config); err != nil {
					// If we got a JSON-RPC error, then it's because the plugin rejected the new config for some
					// reason, so we can just keep the plugin running with the old config. Otherwise, the plugin is
					// probably already gone, so call stopReset() to clean up and prepare for a new plugin instance.
					if _, ok := errors.AsType[*jsonrpc.Error](err); !ok {
						c.Logger.Warnw("Failed to reload plugin config, restarting the plugin", zap.Error(err))
						stopReset()
					} else {
						c.Logger.Warnw("Failed to reload plugin config, continuing with the old config", zap.Error(err))
					}
				}
			} else {
				stopReset()
				if currentConfig.ctype != newConf.ctype {
					c.Logger.Infow("Plugin type has changed, cleaning up plugin state in the database",
						zap.String("old_type", currentConfig.ctype),
						zap.String("new_type", newConf.ctype))

					// If the plugin type has changed, we need to clean up the plugin state in the database,
					// as it's no longer relevant for the new plugin type. It'll will the query internally
					// for 5m before giving up, so we don't need to retry here.
					if err := deleteByChannelID(c.pluginCtx, c.db, c.ID); err != nil {
						c.Logger.Warnw("Failed to clean up channel plugin state after plugin type change",
							zap.String("old_type", currentConfig.ctype),
							zap.String("new_type", newConf.ctype),
							zap.Error(err))
					}
				}
			}

			currentConfig = newConf
		}
	}
}

// getPlugin returns a fully initialized plugin that can be used for sending notifications. If there
// currently is no such plugin, for example because starting it failed, nil is returned instead.
func (c *Channel) getPlugin() *pluginSupervisor {
	p := <-c.pluginCh
	if p == nil {
		// The above receive might have woken pluginControlLoop after the select was blocked for a long time.
		// In that case, a second receive gives it another chance to successfully start the plugin.
		p = <-c.pluginCh
	}

	return p
}

// Stop cancels the plugin context, which will cause the plugin control loop to exit.
//
// If chDeleted is true, the cancellation cause will be set to [ErrChannelDeleted], which signals
// to the plugin control loop that the channel has been deleted, and it should clean up its state
// in the database before exiting.
func (c *Channel) Stop(chDeleted bool) {
	if chDeleted {
		c.pluginCtxCancel(ErrChannelDeleted)
	} else {
		c.pluginCtxCancel(nil)
	}
}

// Restart signals to restart the channel plugin with the updated channel config
func (c *Channel) Restart(logger *zap.SugaredLogger) {
	c.Logger = logger.With(zap.Object("channel", c))
	c.Logger.Info("Restarting the channel plugin due to a config change")
	c.restartCh <- newConfig{c.Type, c.Config}
}

// Notify prepares and sends the notification request, returns a non-error on fails, nil on success
func (c *Channel) Notify(contact *recipient.Contact, i contracts.Incident, o *object.Object, ev *event.Event, icingaweb2Url *url.URL) error {
	p := c.getPlugin()
	if p == nil {
		return errors.New("plugin could not be started")
	}

	contactStruct := &plugin.Contact{FullName: contact.FullName}
	for _, addr := range contact.Addresses {
		contactStruct.Addresses = append(contactStruct.Addresses, &plugin.Address{Type: addr.Type, Address: addr.Address})
	}

	incidentUrl := icingaweb2Url.JoinPath("/notifications/incident")
	incidentUrl.RawQuery = fmt.Sprintf("id=%d", i.ID())

	req := &plugin.NotificationRequest{
		Contact: contactStruct,
		Object: &plugin.Object{
			Name: o.DisplayName(),
			Url:  ev.URL,
			Tags: o.Tags,
		},
		Incident: &plugin.Incident{
			Id:       i.ID(),
			Url:      incidentUrl.String(),
			Severity: i.IncidentSeverity(),
		},
		Event: &plugin.Event{
			Time:    ev.Time,
			Message: ev.Message,
		},
	}

	return p.SendNotification(c.pluginCtx, req)
}
