package channel

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/icinga/icinga-go-library/database"
	"github.com/icinga/icinga-go-library/notifications/plugin"
	"github.com/icinga/icinga-go-library/types"
	"github.com/icinga/icinga-notifications/internal/daemon"
	"github.com/icinga/icinga-notifications/internal/testutils"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestPlugin(t *testing.T) {
	t.Parallel()

	db := testutils.GetTestDB(t.Context(), t, func(dc *daemon.ConfigFile) *database.Config {
		daemon.SetTestConfig(dc)
		return &dc.Database
	})
	logs := testutils.GetTestLogging(t)

	UpsertPlugins(t.Context(), daemon.Config().ChannelsDir, logs.GetChildLogger("channel"), db)

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cleanupDB(ctx, db, t)
	})

	// getPluginS is a helper function to retrieve the pluginSupervisor from the channel's pluginCh channel.
	getPluginS := func(ch *Channel) *pluginSupervisor {
		select {
		case ps := <-ch.pluginCh:
			return ps
		default:
			return nil
		}
	}

	t.Run("Config Reload", func(t *testing.T) {
		t.Parallel()

		ch := makeTestChannel(t, db, logs.GetChildLogger("channel").Desugar(), "sleepy1", "sleep", `{"duration": "2s"}`)

		var plugin1 *pluginSupervisor
		require.Eventually(t, func() bool { plugin1 = getPluginS(ch); return plugin1 != nil }, 5*time.Second, 100*time.Millisecond)
		require.NotNil(t, plugin1)

		// checkSleepy is a helper function to test the plugin's sleep duration by sending a notification request and measuring the time taken.
		checkSleepy := func(timeout time.Duration, expectedDuration time.Duration) {
			ctx, cancel := context.WithTimeout(t.Context(), timeout)
			defer cancel()
			assert.ErrorContains(t,
				plugin1.SendNotification(ctx, makeTestRequest("sleep", false, false)),
				fmt.Sprintf("plugin slept for %s", expectedDuration))
		}
		checkSleepy(3*time.Second, 2*time.Second)

		var wg sync.WaitGroup
		// Update the channel's configuration to simulate a DB config change and trigger a plugin reload.
		ch.Config = `{"duration": "3s"}`
		ch.restartCh <- newConfig{ctype: ch.Type, config: ch.Config}
		require.Eventually(t, func() bool { return getPluginS(ch) == plugin1 }, 5*time.Second, 100*time.Millisecond)
		wg.Go(func() { checkSleepy(4*time.Second, 3*time.Second) })

		// Now, let's change the config again, but this with invalid config to simulate a plugin
		// config change that fails validation and doesn't trigger a restart.
		ch.Config = `{"duration": "invalid"}`
		ch.restartCh <- newConfig{ctype: ch.Type, config: ch.Config}
		require.Eventually(t, func() bool { return getPluginS(ch) == plugin1 }, 5*time.Second, 100*time.Millisecond)
		// Nothing should have changed, so the plugin should still sleep for 3 seconds.
		wg.Go(func() { checkSleepy(4*time.Second, 3*time.Second) })
		wg.Wait()

		// Now, let's change the type to simulate a plugin type change and trigger a full restart.
		ch.Type = "webhook"
		ch.restartCh <- newConfig{ctype: ch.Type, config: `{}`}
		var plugin2 *pluginSupervisor
		require.Eventually(t, func() bool {
			plugin2 = getPluginS(ch)
			return plugin2 != nil && plugin2 != plugin1
		}, 5*time.Second, 100*time.Millisecond, "new plugin should be started after type change")
		require.Error(t, plugin2.SendNotification(t.Context(), makeTestRequest("sleep", false, false)))
	})

	t.Run("Plugin Crash Recovery", func(t *testing.T) {
		t.Parallel()

		ch := makeTestChannel(t, db, logs.GetChildLogger("channel").Desugar(), "sleepy2", "sleep", `{"duration": "1s"}`)

		var plugin1 *pluginSupervisor
		require.Eventually(t, func() bool { plugin1 = getPluginS(ch); return plugin1 != nil }, 5*time.Second, 100*time.Millisecond)
		require.NotNil(t, plugin1)
		require.NoError(t, plugin1.rpc.Conn().Close()) // Simulate a plugin crash by closing the RPC connection.

		var plugin2 *pluginSupervisor
		require.Eventually(t, func() bool { plugin2 = getPluginS(ch); return plugin2 != nil && plugin2 != plugin1 }, 5*time.Second, 100*time.Millisecond)
		require.ErrorContains(t,
			plugin2.SendNotification(t.Context(), makeTestRequest("sleep", false, false)),
			"plugin slept for 1s")
	})

	t.Run("Plugin State Management", func(t *testing.T) {
		t.Parallel()

		ch := makeTestChannel(t, db, logs.GetChildLogger("channel").Desugar(), "sleepy3", "sleep", `{"duration": "1s", "persist_state": true}`)

		var plugin1 *pluginSupervisor
		require.Eventually(t, func() bool { plugin1 = getPluginS(ch); return plugin1 != nil }, 5*time.Second, 100*time.Millisecond)
		require.NotNil(t, plugin1)

		// Simulate sending a notification and persisting state.
		req := makeTestRequest("sleep", false, false)
		require.ErrorContains(t, plugin1.SendNotification(t.Context(), req), "plugin slept for 1s")
		stateBefore, err := getStateByChannelID(t.Context(), db, ch.ID)
		require.NoError(t, err)
		require.Len(t, stateBefore, 1)

		// Now, let's simulate a recovery scenario by sending a recovered notification and checking if the state is cleaned up.
		req.Incident.IsRecovered = true
		require.ErrorContains(t, plugin1.SendNotification(t.Context(), req), "plugin slept for 1s")
		stateAfter, err := getStateByChannelID(t.Context(), db, ch.ID)
		require.NoError(t, err)
		require.Len(t, stateAfter, 0)

		req1 := makeTestRequest("sleep", true, false)
		require.ErrorContains(t, plugin1.SendNotification(t.Context(), req1), "plugin slept for 1s")
		req1.Incident.IsMuted = false
		require.ErrorContains(t, plugin1.SendNotification(t.Context(), req1), "plugin slept for 1s")

		states, err := getStateByChannelID(t.Context(), db, ch.ID)
		require.NoError(t, err)
		require.Len(t, states, 1)

		req2 := makeTestRequest("sleep", false, false)
		require.ErrorContains(t, plugin1.SendNotification(t.Context(), req2), "plugin slept for 1s")

		req3 := makeTestRequest("sleep", false, false)
		require.ErrorContains(t, plugin1.SendNotification(t.Context(), req3), "plugin slept for 1s")

		states, err = getStateByChannelID(t.Context(), db, ch.ID)
		require.NoError(t, err)
		require.Len(t, states, 3)

		req1.Incident.IsRecovered = true
		require.ErrorContains(t, plugin1.SendNotification(t.Context(), req1), "plugin slept for 1s")

		states, err = getStateByChannelID(t.Context(), db, ch.ID)
		require.NoError(t, err)
		require.Len(t, states, 2)

		// Now, let's simulate a plugin type change and ensure that the state is cleaned up in the database.
		ch.Type = "webhook"
		ch.restartCh <- newConfig{ctype: ch.Type, config: `{}`}
		var plugin2 *pluginSupervisor
		require.Eventually(t, func() bool { plugin2 = getPluginS(ch); return plugin2 != nil && plugin2 != plugin1 }, 5*time.Second, 100*time.Millisecond)

		states, err = getStateByChannelID(t.Context(), db, ch.ID)
		require.NoError(t, err)
		require.Len(t, states, 0)
	})

	t.Run("Spamming Stderr", func(t *testing.T) {
		t.Parallel()

		// Use a noop logger here, otherwise the test output will be spammed with the plugin's stderr output.
		ch := makeTestChannel(t, db, zap.NewNop(), "sleepy4", "sleep", `{"duration": "1s", "spam_stderr": true}`)

		var ps *pluginSupervisor
		require.Eventually(t, func() bool { ps = getPluginS(ch); return ps != nil }, 5*time.Second, 100*time.Millisecond)
		req := makeTestRequest("sleep", false, false)
		require.ErrorContains(t, ps.SendNotification(t.Context(), req), "plugin slept for 1s")
	})

	t.Run("Stderr Read Timeout", func(t *testing.T) {
		t.Parallel()

		// The read timeout used for reading the plugin's stderr is 10 seconds, so we set the plugin's
		// sleep duration to 12 seconds to trigger a timeout.
		ch := makeTestChannel(t, db, zap.NewNop(), "sleepy5", "sleep", `{"duration": "12s"}`)

		var ps *pluginSupervisor
		require.Eventually(t, func() bool { ps = getPluginS(ch); return ps != nil }, 5*time.Second, 100*time.Millisecond)
		req := makeTestRequest("sleep", false, false)
		require.ErrorContains(t, ps.SendNotification(t.Context(), req), "plugin slept for 12s")
	})

	t.Run("Plugin Context Cancellation", func(t *testing.T) {
		t.Parallel()

		ch := makeTestChannel(t, db, zap.NewNop(), "sleepy6", "sleep", `{"duration": "1s", "persist_state": true}`)

		var ps *pluginSupervisor
		require.Eventually(t, func() bool { ps = getPluginS(ch); return ps != nil }, 5*time.Second, 100*time.Millisecond)

		assert.ErrorContains(t,
			ps.SendNotification(t.Context(), makeTestRequest("sleep", false, false)),
			"plugin slept for 1s")
		assert.ErrorContains(t,
			ps.SendNotification(t.Context(), makeTestRequest("sleep", false, false)),
			"plugin slept for 1s")

		states, err := getStateByChannelID(t.Context(), db, ch.ID)
		require.NoError(t, err)
		require.Len(t, states, 2)

		ch.Stop(true) // Simulate channel deletion, which cancels the plugin context with ErrChannelDeleted.
		<-ch.pluginCh
		states, err = getStateByChannelID(t.Context(), db, ch.ID)
		require.NoError(t, err)
		require.Len(t, states, 0, "plugin state should be cleaned up after channel deletion")
	})

	t.Run("Invalid State", func(t *testing.T) {
		t.Parallel()

		config := `{"duration": "1s", "persist_state": true, "use_invalid_state_key": true}`
		ch := makeTestChannel(t, db, logs.GetChildLogger("channel").Desugar(), "sleepy7", "sleep", config)
		var ps *pluginSupervisor
		require.Eventually(t, func() bool { ps = getPluginS(ch); return ps != nil }, 5*time.Second, 100*time.Millisecond)
		// The sleep plugins sends any errors received from Icinga Notifications back to the SendNotification caller.
		assert.ErrorContains(t,
			ps.SendNotification(t.Context(), makeTestRequest("sleep", false, false)),
			"jsonrpc2: code -32602 message: invalid state key, must be non-empty and at most 255 chars")

		config = `{"duration": "1s", "persist_state": true, "use_invalid_state_value": true}`
		ch.restartCh <- newConfig{ctype: ch.Type, config: config}
		require.Eventually(t, func() bool { ps = getPluginS(ch); return ps != nil }, 5*time.Second, 100*time.Millisecond)
		assert.ErrorContains(t,
			ps.SendNotification(t.Context(), makeTestRequest("sleep", false, false)),
			"jsonrpc2: code -32602 message: invalid state value, must be non-empty and at most 4096 chars")
	})

	t.Run("Type Validation", func(t *testing.T) {
		t.Parallel()

		assert.Error(t, ValidateType("Üinvalid"))
		assert.Error(t, ValidateType(strings.Repeat("a", 256)))
		assert.NoError(t, ValidateType(strings.Repeat("a", 255)))
		assert.NoError(t, ValidateType("valid_type"))
		assert.NoError(t, ValidateType("valid-type"))
		assert.NoError(t, ValidateType("valid"))
		assert.NoError(t, ValidateType("valid125"))
	})
}

// makeTestChannel creates a new Channel instance with the provided name, type, and config for testing purposes.
func makeTestChannel(t *testing.T, db *database.DB, logger *zap.Logger, name, ctype, config string) *Channel {
	ch := &Channel{Name: name, Type: ctype, Config: config, Uuid: uuid.New().String()}
	ch.ChangedAt = types.UnixMilli(time.Date(2009, time.November, 10, 23, 0, 0, 0, time.UTC))
	ch.Deleted = types.MakeBool(false)

	err := db.ExecTx(t.Context(), nil, func(ctx context.Context, tx *sqlx.Tx) error {
		id, err := database.InsertObtainID(ctx, tx, database.BuildInsertStmtWithout(db, ch, "id"), ch)
		require.NoError(t, err, "populating channel table should not fail")
		ch.ID = id
		return nil
	})
	require.NoError(t, err, "db.ExecTx should not fail")
	ch.Start(t.Context(), db, logger.Sugar())
	return ch
}

// makeTestRequest creates a new [plugin.NotificationRequest] instance for testing purposes.
func makeTestRequest(addrType string, muted, recovered bool) *plugin.NotificationRequest {
	return &plugin.NotificationRequest{
		Contact:  &plugin.Contact{FullName: "Sleepy User", Addresses: []*plugin.Address{{Type: addrType, Address: "john@doe.com"}}},
		Object:   &plugin.Object{Name: "Sleepy Object"},
		Incident: &plugin.Incident{Id: makeRandomNumber(), IsMuted: muted, IsRecovered: recovered},
		Event:    &plugin.Event{Time: time.Now(), Message: "Test event message"},
	}
}

// makeRandomNumber generates a random number using cryptographic randomness and returns it as an int64.
//
// Using time.Now().UnixNano() is not suitable here as the tests are run in parallel and can lead to collisions.
func makeRandomNumber() int64 {
	var b [4]byte
	_, _ = rand.Read(b[:]) // crypto/rand.Read never returns an error, so we can ignore it here.
	return int64(binary.LittleEndian.Uint32(b[:]))
}

// cleanupDB cleans up the database by truncating or deleting all rows from the relevant tables,
// depending on the database driver being used.
func cleanupDB(ctx context.Context, db *database.DB, t *testing.T) {
	switch db.DriverName() {
	case database.PostgreSQL, database.MySQL:
		tables := []string{
			"channel_state",
			"channel",
			"available_channel_type",
		}

		for _, table := range tables {
			_, err := db.ExecContext(ctx, "DELETE FROM "+table)
			require.NoErrorf(t, err, "failed to clean up table %s", table)
		}
	default:
		t.Fatalf("unsupported database driver: %s", db.DriverName())
	}
}
