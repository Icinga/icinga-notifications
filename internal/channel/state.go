package channel

import (
	"context"

	"github.com/icinga/icinga-go-library/database"
	"github.com/icinga/icinga-go-library/types"
	"github.com/jmoiron/sqlx"
	"github.com/pkg/errors"
)

// State represents a key-value pair associated with a channel, used to store the state of a plugin in the database.
//
// The plugin will only receive the Key and Value fields, while the other fields are fully managed by the plugin
// supervisor of that specific channel. Thus, the plugins won't be able to see or modify the ChannelID and ChangedAt
// fields, which prevents accidental or malicious tampering with the state of other channels.
type State struct {
	ChannelID int64           `db:"channel_id"`
	Key       string          `db:"state_key"`
	Value     string          `db:"value"`
	ChangedAt types.UnixMilli `db:"changed_at"`
}

// TableName implements the [database.TableNamer] interface.
func (s *State) TableName() string { return "channel_state" }

// Upsert implements the [database.Upserter] interface.
func (s *State) Upsert() any {
	return struct {
		Value     string          `db:"value"`
		ChangedAt types.UnixMilli `db:"changed_at"`
	}{}
}

// getStateByChannelID retrieves the state of a plugin for a specific channel from the database,
// matching the provided channel ID and changedAt timestamp.
func getStateByChannelID(ctx context.Context, db *database.DB, channelID int64, changedAt types.UnixMilli) ([]*State, error) {
	query := `SELECT * FROM channel_state WHERE channel_id = ? AND changed_at > ?`
	var states []*State
	if err := db.SelectContext(ctx, &states, db.Rebind(query), channelID, changedAt); err != nil {
		return nil, err
	}
	return states, nil
}

// upsertState inserts or updates the state of a plugin for a specific channel in the database.
func upsertState(ctx context.Context, db *database.DB, states ...*State) error {
	query, _ := db.BuildUpsertStmt(new(State))
	return db.ExecTx(ctx, nil, func(ctx context.Context, tx *sqlx.Tx) error {
		// Since we don't have a control over what the channel plugin will send, we need to use prepared stmt for this.
		stmt, err := tx.PrepareNamedContext(ctx, query)
		if err != nil {
			return errors.Wrap(err, "failed to prepare upsert statement for channel state")
		}
		defer func() { _ = stmt.Close() }()

		for _, state := range states {
			if _, err := stmt.ExecContext(ctx, state); err != nil {
				return errors.Wrap(err, "failed to upsert state for channel state")
			}
		}
		return nil
	})
}

// deleteStateByKey deletes the state of a plugin for a specific channel and key from the database.
func deleteStateByKey(ctx context.Context, db *database.DB, channelID int64, key string) error {
	stmt, err := db.PrepareContext(ctx, db.Rebind(`DELETE FROM channel_state WHERE channel_id = ? AND state_key = ?`))
	if err != nil {
		return errors.Wrap(err, "failed to prepare delete statement for channel state")
	}
	defer func() { _ = stmt.Close() }()

	if _, err := stmt.ExecContext(ctx, channelID, key); err != nil {
		return errors.Wrap(err, "failed to delete state for channel state")
	}
	return nil
}

// deleteByChannelID deletes all states associated with a specific channel ID from the database.
func deleteByChannelID(ctx context.Context, db *database.DB, channelID int64) error {
	_, err := db.ExecContext(ctx, db.Rebind(`DELETE FROM channel_state WHERE channel_id = ?`), channelID)
	if err != nil {
		return errors.Wrap(err, "failed to delete states for channel state")
	}
	return nil
}
