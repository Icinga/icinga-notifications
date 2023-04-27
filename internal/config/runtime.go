package config

import (
	"context"
	"database/sql"
	"github.com/icinga/icingadb/pkg/icingadb"
	"github.com/icinga/icingadb/pkg/logging"
	"github.com/icinga/noma/internal/channel"
	"github.com/icinga/noma/internal/recipient"
	"github.com/icinga/noma/internal/rule"
	"github.com/icinga/noma/internal/timeperiod"
	"github.com/jmoiron/sqlx"
)

// RuntimeConfig stores the runtime representation of the configuration present in the database.
type RuntimeConfig struct {
	// ConfigSet is the current live config. It is embedded to allow direct access to its members.
	ConfigSet

	// pending contains changes to config objects that are to be applied to the embedded live config.
	pending ConfigSet
}

type ConfigSet struct {
	ChannelByType   map[string]*channel.Channel
	ContactsByID    map[int64]*recipient.Contact
	GroupsByID      map[int64]*recipient.Group
	TimePeriodsById map[int64]*timeperiod.TimePeriod
	SchedulesByID   map[int64]*recipient.Schedule
	RulesByID       map[int64]*rule.Rule
}

func (r *RuntimeConfig) UpdateFromDatabase(ctx context.Context, db *icingadb.DB, logger *logging.Logger) error {
	err := r.fetchFromDatabase(ctx, db, logger)
	if err != nil {
		return err
	}

	r.applyPending(logger)

	return nil
}

func (r *RuntimeConfig) fetchFromDatabase(ctx context.Context, db *icingadb.DB, logger *logging.Logger) error {
	tx, err := db.BeginTxx(ctx, &sql.TxOptions{
		Isolation: sql.LevelRepeatableRead,
		ReadOnly:  true,
	})
	if err != nil {
		return err
	}
	// The transaction is only used for reading, never has to be committed.
	defer func() { _ = tx.Rollback() }()

	updateFuncs := []func(ctx context.Context, db *icingadb.DB, tx *sqlx.Tx, logger *logging.Logger) error{
		r.fetchChannels,
		r.fetchContacts,
		r.fetchGroups,
		r.fetchTimePeriods,
		r.fetchSchedules,
		r.fetchRules,
	}
	for _, f := range updateFuncs {
		if err := f(ctx, db, tx, logger); err != nil {
			return err
		}
	}

	return nil
}

func (r *RuntimeConfig) applyPending(logger *logging.Logger) {
	r.ConfigSet = r.pending
}
