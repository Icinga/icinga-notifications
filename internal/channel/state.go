package channel

import (
	"context"

	"github.com/icinga/icinga-go-library/backoff"
	"github.com/icinga/icinga-go-library/database"
	"github.com/icinga/icinga-go-library/notifications/plugin"
	"github.com/icinga/icinga-go-library/retry"
	"github.com/jmoiron/sqlx"
	"github.com/pkg/errors"
)

const (
	// maxStateKeyLen defines the maximum length of a state key in the database.
	maxStateKeyLen = 255

	// maxStateValueLen defines the maximum length of a single state value in the database.
	maxStateValueLen = 4096
)

// State represents a key-value pair associated with a channel, used to store the state of a plugin in the database.
//
// The plugin will only receive the Key and Value fields, while the other fields are fully managed by the plugin
// supervisor of that specific channel. Thus, the plugins won't be able to see or modify the ChannelID field,
// which prevents accidental or malicious tampering with the state of other channels.
type State struct {
	ChannelID    int64 `db:"channel_id" json:"-"`
	plugin.State `db:",inline"`
}

// TableName implements the [database.TableNamer] interface.
func (s *State) TableName() string { return "channel_state" }

// Upsert implements the [database.Upserter] interface.
func (s *State) Upsert() any {
	return struct {
		Value string `db:"value"`
	}{}
}

// getStateByChannelID retrieves the state of a plugin for a specific channel from the database,
// matching the provided channel ID.
func getStateByChannelID(ctx context.Context, db *database.DB, channelID int64) ([]*State, error) {
	query := db.Rebind(`SELECT * FROM channel_state WHERE channel_id = ?`)
	var states []*State
	err := retry.WithBackoff(
		ctx,
		func(ctx context.Context) error {
			return db.SelectContext(ctx, &states, query, channelID)
		},
		retry.Retryable,
		backoff.DefaultBackoff,
		db.GetDefaultRetrySettings(),
	)
	if err != nil {
		return nil, errors.Wrap(err, "failed to retrieve channel state by channel ID")
	}
	return states, nil
}

// upsertStates upserts (inserts or updates) the provided states into the database.
func upsertStates(ctx context.Context, db *database.DB, states []*State) error {
	query, _ := db.BuildUpsertStmt(new(State))
	return retry.WithBackoff(
		ctx,
		func(ctx context.Context) error {
			return db.ExecTx(ctx, nil, func(ctx context.Context, tx *sqlx.Tx) error {
				// Since we don't have a control over what the channel plugin will send, we need to use prepared stmt for this.
				stmt, err := tx.PrepareNamedContext(ctx, query)
				if err != nil {
					return errors.Wrap(err, "failed to prepare upsert statement for channel state")
				}
				defer func() { _ = stmt.Close() }()

				for _, state := range states {
					if _, err := stmt.ExecContext(ctx, state); err != nil {
						return errors.Wrap(err, "failed to upsert channel state")
					}
				}
				return nil
			})
		},
		retry.Retryable,
		backoff.DefaultBackoff,
		db.GetDefaultRetrySettings(),
	)
}

// deleteStates deletes the provides states from the database, matching the channel ID and state key for each state.
func deleteStates(ctx context.Context, db *database.DB, states []*State) error {
	return retry.WithBackoff(
		ctx,
		func(ctx context.Context) error {
			return db.ExecTx(ctx, nil, func(ctx context.Context, tx *sqlx.Tx) error {
				stmt, err := tx.PrepareNamedContext(ctx, `DELETE FROM channel_state WHERE channel_id = :channel_id AND state_key = :state_key`)
				if err != nil {
					return errors.Wrap(err, "failed to prepare delete statement for channel state")
				}
				defer func() { _ = stmt.Close() }()

				for _, state := range states {
					if _, err := stmt.ExecContext(ctx, state); err != nil {
						return errors.Wrap(err, "failed to delete channel state")
					}
				}
				return nil
			})
		},
		retry.Retryable,
		backoff.DefaultBackoff,
		db.GetDefaultRetrySettings(),
	)
}

// deleteByChannelID deletes all states associated with a specific channel ID from the database.
func deleteByChannelID(ctx context.Context, db *database.DB, channelID int64) error {
	return retry.WithBackoff(
		ctx,
		func(ctx context.Context) error {
			_, err := db.ExecContext(ctx, db.Rebind(`DELETE FROM channel_state WHERE channel_id = ?`), channelID)
			if err != nil {
				return errors.Wrap(err, "failed to delete channel state")
			}
			return nil
		},
		retry.Retryable,
		backoff.DefaultBackoff,
		db.GetDefaultRetrySettings(),
	)
}
