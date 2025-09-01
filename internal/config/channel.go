package config

import (
	"context"
	"github.com/icinga/icinga-notifications/internal/channel"
)

// applyPendingChannels synchronizes changed channels.
func (r *RuntimeConfig) applyPendingChannels() {
	incrementalApplyPending(
		r,
		&r.Channels, &r.configChange.Channels,
		func(newElement *channel.Channel) error {
			newElement.Start(context.TODO(), r.logs.GetChildLogger("channel").SugaredLogger)
			return nil
		},
		func(curElement, update *channel.Channel) error {
			curElement.ChangedAt = update.ChangedAt
			curElement.Name = update.Name
			curElement.Type = update.Type
			curElement.Config = update.Config
			curElement.Restart(r.logs.GetChildLogger("channel").SugaredLogger)
			return nil
		},
		func(delElement *channel.Channel) error {
			delElement.Stop()
			return nil
		})
}
