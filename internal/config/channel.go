package config

import (
	"context"
	"github.com/icinga/icinga-notifications/internal/channel"
	"go.uber.org/zap"
)

// applyPendingChannels synchronizes changed channels.
func (r *RuntimeConfig) applyPendingChannels() {
	incrementalApplyPending(
		r,
		&r.Channels, &r.configChange.Channels,
		func(newElement *channel.Channel) error {
			newElement.Start(context.TODO(), r.logs.GetChildLogger("channel").With(
				zap.Int64("id", newElement.ID),
				zap.String("name", newElement.Name)))
			return nil
		},
		func(curElement, update *channel.Channel) error {
			curElement.ChangedAt = update.ChangedAt
			curElement.Name = update.Name
			curElement.Type = update.Type
			curElement.Config = update.Config
			curElement.Restart()
			return nil
		},
		func(delElement *channel.Channel) error {
			delElement.Stop()
			return nil
		})
}
