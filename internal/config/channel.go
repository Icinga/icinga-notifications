package config

import (
	"context"
	"github.com/icinga/icinga-notifications/internal/channel"
	"github.com/jmoiron/sqlx"
	"go.uber.org/zap"
	"regexp"
)

// channelTypeNameRegex is to filter allowed channel type name
var channelTypeNameRegex = regexp.MustCompile("^[a-zA-Z0-9]+$")

func (r *RuntimeConfig) fetchChannels(ctx context.Context, tx *sqlx.Tx) error {
	var channelPtr *channel.Channel
	stmt := r.db.BuildSelectStmt(channelPtr, channelPtr)
	r.logger.Debugf("Executing query %q", stmt)

	var channels []*channel.Channel
	if err := tx.SelectContext(ctx, &channels, stmt); err != nil {
		r.logger.Errorln(err)
		return err
	}

	channelsById := make(map[int64]*channel.Channel)
	for _, c := range channels {
		channelLogger := r.logger.With(
			zap.Int64("id", c.ID),
			zap.String("name", c.Name),
			zap.String("type", c.Type),
		)
		if channelsById[c.ID] != nil {
			channelLogger.Warnw("ignoring duplicate config for channel type")
		} else if !channelTypeNameRegex.MatchString(c.Type) {
			channelLogger.Errorf("Channel type must only contain a-zA-Z0-9, %q given", c.Type)
		} else {
			channelsById[c.ID] = c

			channelLogger.Debugw("loaded channel config")
		}
	}

	if r.Channels != nil {
		// mark no longer existing channels for deletion
		for id := range r.Channels {
			if _, ok := channelsById[id]; !ok {
				channelsById[id] = nil
			}
		}
	}

	r.pending.Channels = channelsById

	return nil
}

func (r *RuntimeConfig) applyPendingChannels() {
	if r.Channels == nil {
		r.Channels = make(map[int64]*channel.Channel)
	}

	for id, pendingChannel := range r.pending.Channels {
		if pendingChannel == nil {
			r.Channels[id].Logger.Info("Channel has been removed, stopping channel plugin")
			r.Channels[id].Stop()

			delete(r.Channels, id)
		} else if currentChannel := r.Channels[id]; currentChannel != nil {
			// Currently, the whole config is reloaded from the database frequently, replacing everything.
			// Prevent restarting the plugin processes every time by explicitly checking for config changes.
			// The if condition should no longer be necessary when https://github.com/Icinga/icinga-notifications/issues/5
			// is solved properly.
			if currentChannel.ID != pendingChannel.ID || currentChannel.Name != pendingChannel.Name || currentChannel.Config != pendingChannel.Config {
				currentChannel.ID = pendingChannel.ID
				currentChannel.Name = pendingChannel.Name
				currentChannel.Config = pendingChannel.Config

				currentChannel.Logger.Info("New config detected, reloading the channel plugin config")
				currentChannel.ReloadConfig()
			}
		} else {
			pendingChannel.Start(r.logs.GetChildLogger("channel").With(
				zap.Int64("id", pendingChannel.ID),
				zap.String("name", pendingChannel.Name)))

			r.Channels[id] = pendingChannel
		}
	}

	r.pending.Channels = nil
}
