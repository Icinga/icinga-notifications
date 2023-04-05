package config

import (
	"context"
	"database/sql"
	"github.com/icinga/icingadb/pkg/icingadb"
	"github.com/icinga/icingadb/pkg/logging"
	"github.com/icinga/noma/internal/channel"
	"go.uber.org/zap"
	"log"
)

// RuntimeConfig stores the runtime representation of the configuration present in the database.
type RuntimeConfig struct {
	Channels      []*channel.Channel
	ChannelByType map[string]*channel.Channel
}

func (r *RuntimeConfig) UpdateFromDatabase(ctx context.Context, db *icingadb.DB, logger *logging.Logger) error {
	tx, err := db.BeginTxx(ctx, &sql.TxOptions{
		Isolation: sql.LevelRepeatableRead,
		ReadOnly:  true,
	})
	if err != nil {
		return err
	}
	// The transaction is only used for reading, never has to be committed.
	defer func() { _ = tx.Rollback() }()

	var channelPtr *channel.Channel
	stmt := db.BuildSelectStmt(channelPtr, channelPtr)
	log.Println(stmt)

	var channels []*channel.Channel
	if err := tx.SelectContext(ctx, &channels, stmt); err != nil {
		log.Println(err)
		return err
	}

	channelsByType := make(map[string]*channel.Channel)
	for _, c := range channels {
		channelLogger := logger.With(
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

	r.Channels = channels
	r.ChannelByType = channelsByType

	return nil
}
