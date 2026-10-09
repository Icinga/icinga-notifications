package channel

import (
	"context"
	"database/sql"
	"time"

	"github.com/icinga/icinga-go-library/backoff"
	"github.com/icinga/icinga-go-library/database"
	"github.com/icinga/icinga-go-library/notifications/plugin"
	"github.com/icinga/icinga-go-library/retry"
	"github.com/icinga/icinga-go-library/types"
	"github.com/pkg/errors"
)

const (
	// maxStateValueLen defines the maximum length of a single state value in the database.
	maxStateValueLen = 4096

	// MaxDeliveryResultSize is the maximum size in bytes of the serialized delivery result details a plugin can return.
	//
	// Any delivery result details exceeding this limit will be dropped completely and not stored in the database.
	MaxDeliveryResultSize = 1024 * 1024
)

// State represents a key-value pair associated with a channel, incident and contact in the database.
//
// The plugin will only receive the Key and Value fields, while the other fields are fully managed by the plugin
// supervisor of that specific channel. Thus, the plugins won't be able to see or modify the ChannelID field,
// which prevents accidental or malicious tampering with the state of other channels.
type State struct {
	ChannelID    int64 `db:"channel_id" json:"-"`
	IncidentID   int64 `db:"incident_id" json:"-"`
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

// DeleteStateByIncidentID deletes all states associated with a specific incident ID from the database.
func DeleteStateByIncidentID(ctx context.Context, db *database.DB, incidentID int64) error {
	return retry.WithBackoff(
		ctx,
		func(ctx context.Context) error {
			_, err := db.ExecContext(ctx, db.Rebind(`DELETE FROM channel_state WHERE incident_id = ?`), incidentID)
			return err
		},
		retry.Retryable,
		backoff.DefaultBackoff,
		retry.Settings{Timeout: 10 * time.Second})
}

// getStateByKey retrieves the state associated with a specific key from the database.
func getStateByKey(ctx context.Context, db *database.DB, key types.UUID) (State, error) {
	query := db.Rebind(`SELECT * FROM channel_state WHERE state_key = ?`)
	var state State
	err := retry.WithBackoff(
		ctx,
		func(ctx context.Context) error { return db.GetContext(ctx, &state, query, key) },
		retry.Retryable,
		backoff.DefaultBackoff,
		retry.Settings{Timeout: 10 * time.Second})
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return State{}, errors.Wrap(err, "failed to retrieve channel state by key")
	}
	return state, nil
}

// upsertState upserts (inserts or updates) the provided state into the database.
func upsertState(ctx context.Context, db *database.DB, state *State) error {
	query, _ := db.BuildUpsertStmt(state)
	return retry.WithBackoff(
		ctx,
		func(ctx context.Context) error {
			_, err := db.NamedExecContext(ctx, query, state)
			return err
		},
		retry.Retryable,
		backoff.DefaultBackoff,
		retry.Settings{Timeout: 10 * time.Second})
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
		retry.Settings{Timeout: 10 * time.Second})
}
