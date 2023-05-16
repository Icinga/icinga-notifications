package config

import (
	"context"
	"github.com/icinga/icinga-notifications/internal/channel"
	"github.com/jmoiron/sqlx"
	"go.uber.org/zap"
)

func (r *RuntimeConfig) fetchChannels(ctx context.Context, tx *sqlx.Tx) error {
	var channelPtr *channel.Channel
	stmt := r.db.BuildSelectStmt(channelPtr, channelPtr)
	r.logger.Debugf("Executing query %q", stmt)

	var channels []*channel.Channel
	if err := tx.SelectContext(ctx, &channels, stmt); err != nil {
		r.logger.Errorln(err)
		return err
	}

	channelsByType := make(map[string]*channel.Channel)
	for _, c := range channels {
		channelLogger := r.logger.With(
			zap.Int64("id", c.ID),
			zap.String("name", c.Name),
			zap.String("type", c.Type),
		)
		if channelsByType[c.Type] != nil {
			channelLogger.Warnw("ignoring duplicate config for channel type")
		} else {
			channelsByType[c.Type] = c

			channelLogger.Debugw("loaded channel config")
		}
	}

	if r.Channels != nil {
		// mark no longer existing channels for deletion
		for typ := range r.Channels {
			if _, ok := channelsByType[typ]; !ok {
				channelsByType[typ] = nil
			}
		}
	}

	r.pending.Channels = channelsByType

	return nil
}

func (r *RuntimeConfig) applyPendingChannels() {
	if r.Channels == nil {
		r.Channels = make(map[string]*channel.Channel)
	}

	for typ, pendingChannel := range r.pending.Channels {
		if pendingChannel == nil {
			delete(r.Channels, typ)
		} else if currentChannel := r.Channels[typ]; currentChannel != nil {
			currentChannel.ID = pendingChannel.ID
			currentChannel.Name = pendingChannel.Name
			currentChannel.Config = pendingChannel.Config
			currentChannel.ResetPlugin()
		} else {
			r.Channels[typ] = pendingChannel
		}
	}

	r.pending.Channels = nil
}
