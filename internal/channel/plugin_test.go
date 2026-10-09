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
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/icinga/icinga-go-library/database"
	"github.com/icinga/icinga-go-library/notifications/plugin"
	"github.com/icinga/icinga-go-library/types"
	"github.com/icinga/icinga-notifications/internal/daemon"
	"github.com/icinga/icinga-notifications/internal/event"
	"github.com/icinga/icinga-notifications/internal/object"
	"github.com/icinga/icinga-notifications/internal/recipient"
	"github.com/icinga/icinga-notifications/internal/testutils"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestPlugin(t *testing.T) {
	t.Parallel()

	testutils.SkipTestIfDBConfigIsMissing(t)
	daemon.InjectTestConfig(func(configFile *daemon.ConfigFile) { testutils.LoadTestConfig(t, configFile) })

	db := testutils.GetTestDB(t.Context(), t, &daemon.Config().Database)
	logs := testutils.GetTestLogging(t)
	logger := logs.GetChildLogger("channel").Desugar()

	UpsertPlugins(t.Context(), daemon.Config().ChannelsDir, logs.GetChildLogger("channel"), db)

	cleaner := testutils.NewDBCleaner("channel_state", "channel", "incident", "object")
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cleaner.Clean(ctx, t, db)
	})

	// getPluginS is a helper function to retrieve the pluginSupervisor from the channel's pluginCh with a timeout.
	getPluginS := func(ch *Channel, timeout time.Duration) *pluginSupervisor {
		select {
		case ps := <-ch.pluginCh:
			return ps
		case <-time.After(timeout):
			return nil
		}
	}

	t.Run("Config Reload", func(t *testing.T) {
		t.Parallel()

		ch := makeTestChannel(t, db, cleaner, logger, "sleepy1", "sleep", `{"duration": "2s","success":true}`)

		plugin1 := getPluginS(ch, 5*time.Second)
		require.NotNil(t, plugin1)

		// checkSleepy is a helper function to test the plugin's sleep duration by sending a notification request and measuring the time taken.
		checkSleepy := func(timeout time.Duration, expectedDuration time.Duration) {
			ctx, cancel := context.WithTimeout(t.Context(), timeout)
			defer cancel()
			res, err := ch.Notify(ctx, makeContact("sleep"), makeIncident(false, false), makeObj(), makeEvent(t))
			assert.NoError(t, err)
			assert.NotNil(t, res)
			if res != nil {
				assert.True(t, res.State.IsZero())
				require.Len(t, res.Details, 1)
				assert.Equal(t, fmt.Sprintf("Slept for %s", expectedDuration), res.Details[0].Text)
			}
		}
		checkSleepy(3*time.Second, 2*time.Second)

		var wg sync.WaitGroup
		// Update the channel's configuration to simulate a DB config change and trigger a plugin reload.
		ch.Config = `{"duration": "3s","success":true}`
		ch.restartCh <- newConfig{ctype: ch.Type, config: ch.Config}
		require.Equal(t, plugin1, getPluginS(ch, 5*time.Second))
		wg.Go(func() { checkSleepy(4*time.Second, 3*time.Second) })

		// Now, let's change the config again, but this time with invalid config to simulate a plugin
		// config change that fails validation and doesn't trigger a restart.
		ch.Config = `{"duration": "invalid","success":true}`
		ch.restartCh <- newConfig{ctype: ch.Type, config: ch.Config}
		require.Equal(t, plugin1, getPluginS(ch, 5*time.Second))
		// Nothing should have changed, so the plugin should still sleep for 3 seconds.
		wg.Go(func() { checkSleepy(4*time.Second, 3*time.Second) })
		wg.Wait()

		// Now, let's change the type to simulate a plugin type change and trigger a full restart.
		ch.Type = "webhook"
		ch.restartCh <- newConfig{ctype: ch.Type, config: `{}`}
		p := getPluginS(ch, 5*time.Second)
		require.NotNil(t, p)
		require.NotEqual(t, plugin1, p, "new plugin should be started after type change")

		res, err := ch.Notify(t.Context(), makeContact("webhook"), makeIncident(false, false), makeObj(), makeEvent(t))
		require.Error(t, err)
		require.Nil(t, res)
	})

	t.Run("Plugin Crash Recovery", func(t *testing.T) {
		t.Parallel()

		ch := makeTestChannel(t, db, cleaner, logger, "sleepy2", "sleep", `{"duration": "1s","success":true}`)

		plugin1 := getPluginS(ch, 5*time.Second)
		require.NotNil(t, plugin1)
		require.NoError(t, plugin1.rpc.Conn().Close()) // Simulate a plugin crash by closing the RPC connection.

		time.Sleep(2 * time.Second) // Give the event loop some time to detect the crash and restart the plugin.

		p := getPluginS(ch, 5*time.Second)
		require.NotNil(t, p)
		require.NotEqual(t, plugin1, p, "new plugin should be started after crash recovery")

		res, err := ch.Notify(t.Context(), makeContact("sleep"), makeIncident(false, false), makeObj(), makeEvent(t))
		require.NoError(t, err)
		require.NotNil(t, res)
		require.Len(t, res.Details, 1)
		require.Equal(t, "Slept for 1s", res.Details[0].Text)
	})

	t.Run("Plugin State Management", func(t *testing.T) {
		t.Parallel()

		ch := makeTestChannel(t, db, cleaner, logger, "sleepy3", "sleep", `{"duration":"1s","persist_state":true,"success":true}`)

		plugin1 := getPluginS(ch, 5*time.Second)
		require.NotNil(t, plugin1)

		notify := func(ch *Channel, i *plugin.Incident, o *object.Object, ev *event.Event) {
			res, err := ch.Notify(t.Context(), makeContact("sleep"), i, o, ev)
			require.NoError(t, err)
			require.NotNil(t, res)
		}

		// Simulate sending a notification and persisting state.
		i, o, ev := makePersistedIncidentData(t, db, cleaner, false, false)
		notify(ch, i, o, ev)
		require.Len(t, getStateByChannelID(t, db, ch.ID), 1)

		// Test whether using a different channel with the same incident and recipient type will also persist state correctly.
		ch2 := makeTestChannel(t, db, cleaner, logger, "sleepy3.2", "sleep", `{"duration":"1s","persist_state":true,"success":true}`)
		require.NotNil(t, getPluginS(ch2, 5*time.Second))
		notify(ch2, i, o, ev)
		require.Len(t, getStateByChannelID(t, db, ch2.ID), 1)
		require.Len(t, getStateByChannelID(t, db, ch.ID), 1)

		// Send another notification with the same incident to ensure state is updated and not duplicated.
		i.IsMuted = true
		notify(ch, i, o, ev)
		require.Len(t, getStateByChannelID(t, db, ch.ID), 1)

		i2, o2, ev2 := makePersistedIncidentData(t, db, cleaner, false, false)
		notify(ch, i2, o2, ev2)

		i3, o3, ev3 := makePersistedIncidentData(t, db, cleaner, false, false)
		notify(ch, i3, o3, ev3)
		// At this point, we should have 3 states in the database.
		require.Len(t, getStateByChannelID(t, db, ch.ID), 3)

		// Sending a notification with a recovered incident should not persist a new state.
		i4, o4, ev4 := makePersistedIncidentData(t, db, cleaner, false, true)
		notify(ch, i4, o4, ev4)
		notify(ch2, i4, o4, ev4)
		require.Len(t, getStateByChannelID(t, db, ch.ID), 3)
		require.Len(t, getStateByChannelID(t, db, ch2.ID), 1)

		// Now, let's simulate a plugin type change and ensure that the state is cleaned up in the database.
		ch.Type = "webhook"
		ch.restartCh <- newConfig{ctype: ch.Type, config: `{}`}
		p := getPluginS(ch, 5*time.Second)
		require.NotNil(t, p)
		require.NotEqual(t, plugin1, p, "new plugin should be started after type change")
		require.Len(t, getStateByChannelID(t, db, ch.ID), 0)
	})

	t.Run("Spamming Stderr", func(t *testing.T) {
		t.Parallel()

		// Use a noop logger here, otherwise the test output will be spammed with the plugin's stderr output.
		ch := makeTestChannel(t, db, cleaner, zap.NewNop(), "sleepy4", "sleep", `{"duration":"1s","spam_stderr":true,"success":true}`)

		require.NotNil(t, getPluginS(ch, 5*time.Second))
		res, err := ch.Notify(t.Context(), makeContact("sleep"), makeIncident(false, false), makeObj(), makeEvent(t))
		require.NoError(t, err)
		require.NotNil(t, res)
		require.True(t, res.State.IsZero())
		require.Len(t, res.Details, 1)
		require.Equal(t, "Slept for 1s", res.Details[0].Text)
	})

	t.Run("Stderr Read Timeout", func(t *testing.T) {
		t.Parallel()

		// The read timeout used for reading the plugin's stderr is 10 seconds, so we set the plugin's
		// sleep duration to 12 seconds to trigger a timeout.
		ch := makeTestChannel(t, db, cleaner, zap.NewNop(), "sleepy5", "sleep", `{"duration": "12s","success":true}`)

		require.NotNil(t, getPluginS(ch, 5*time.Second))
		res, err := ch.Notify(t.Context(), makeContact("sleep"), makeIncident(false, false), makeObj(), makeEvent(t))
		require.NoError(t, err)
		require.NotNil(t, res)
		require.Len(t, res.Details, 1)
		require.Equal(t, "Slept for 12s", res.Details[0].Text)
	})

	t.Run("Plugin Context Cancellation", func(t *testing.T) {
		t.Parallel()

		ch := makeTestChannel(t, db, cleaner, zap.NewNop(), "sleepy6", "sleep", `{"duration":"1s","persist_state":true,"success":true}`)

		require.NotNil(t, getPluginS(ch, 5*time.Second))

		i, o, ev := makePersistedIncidentData(t, db, cleaner, false, false)
		res, err := ch.Notify(t.Context(), makeContact("sleep"), i, o, ev)
		require.NoError(t, err)
		require.NotNil(t, res)
		require.Len(t, res.Details, 1)
		require.Equal(t, "Slept for 1s", res.Details[0].Text)

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		res, err = ch.Notify(ctx, makeContact("sleep"), makeIncident(false, false), makeObj(), makeEvent(t))
		require.ErrorIs(t, err, context.Canceled)
		require.Nil(t, res)

		require.Len(t, getStateByChannelID(t, db, ch.ID), 1)

		ch.Stop(true) // Simulate channel deletion, which cancels the plugin context with ErrChannelDeleted.
		<-ch.pluginCh
		require.Len(t, getStateByChannelID(t, db, ch.ID), 0)
	})

	t.Run("Invalid State", func(t *testing.T) {
		t.Parallel()

		config := `{"duration": "1s", "persist_state": true, "use_invalid_state_value": true, "success": true}`
		ch := makeTestChannel(t, db, cleaner, logger, "sleepy7", "sleep", config)

		require.NotNil(t, getPluginS(ch, 5*time.Second))

		i, o, ev := makePersistedIncidentData(t, db, cleaner, false, false)
		res, err := ch.Notify(t.Context(), makeContact("sleep"), i, o, ev)
		require.NoError(t, err)
		require.NotNil(t, res)

		assert.True(t, utf8.RuneCountInString(res.State.Value) > maxStateValueLen) // Invalid!
		assert.Len(t, getStateByChannelID(t, db, ch.ID), 0)                        // The state should not be persisted due to invalid state value.

		ch.restartCh <- newConfig{ctype: ch.Type, config: `{"duration":"1s","persist_state":true,"success":true}`}
		require.NotNil(t, getPluginS(ch, 5*time.Second))

		res, err = ch.Notify(t.Context(), makeContact("sleep"), i, o, ev)
		require.NoError(t, err)
		require.NotNil(t, res)

		assert.Len(t, getStateByChannelID(t, db, ch.ID), 1) // The state should be persisted now with valid state value.
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
func makeTestChannel(t *testing.T, db *database.DB, cleaner *testutils.DBCleaner, logger *zap.Logger, name, ctype, config string) *Channel {
	ch := &Channel{Name: name, Type: ctype, Config: config, ExternalUUID: types.MakeUUID(uuid.New())}
	ch.ChangedAt = types.UnixMilli(time.Date(2009, time.November, 10, 23, 0, 0, 0, time.UTC))
	ch.Deleted = types.MakeBool(false)

	err := db.ExecTx(t.Context(), nil, func(ctx context.Context, tx *sqlx.Tx) error {
		id, err := database.InsertObtainID(ctx, tx, database.BuildInsertStmtWithout(db, ch, "id"), ch)
		require.NoError(t, err, "populating channel table should not fail")
		ch.ID = id
		return nil
	})
	require.NoError(t, err, "db.ExecTx should not fail")

	cleaner.Add("channel", fmt.Sprintf("id = %d", ch.ID))
	cleaner.Add("channel_state", fmt.Sprintf("channel_id = %d", ch.ID))
	ch.Start(t.Context(), db, logger.Sugar())
	return ch
}

// getStateByChannelID retrieves all states associated with a specific channel ID from the database.
func getStateByChannelID(t *testing.T, db *database.DB, channelID int64) []*State {
	var states []*State
	require.NoError(t, db.SelectContext(t.Context(), &states, db.Rebind(`SELECT * FROM channel_state WHERE channel_id = ?`), channelID))
	return states
}

// makeContact creates a new [recipient.Contact] instance with multiple addresses for testing purposes.
func makeContact(addrType string) *recipient.Contact {
	return &recipient.Contact{
		FullName: "Sleepy User",
		Addresses: []*recipient.Address{
			{Type: addrType, Address: "john@doe.com"},
			{Type: "sms", Address: "+1234567890"},
			{Type: "pager", Address: "+0987654321"},
			{Type: "slack", Address: "@sleepyuser"},
			{Type: addrType, Address: "doe@john.com"},
		},
	}
}

// makePersistedIncidentData creates a new incident, object, and event required for state management tests.
//
// It inserts the incident and object into the database and returns the created incident, object, and event.
func makePersistedIncidentData(t *testing.T, db *database.DB, cleaner *testutils.DBCleaner, muted, recovered bool) (*plugin.Incident, *object.Object, *event.Event) {
	ev := makeEvent(t)
	obj := makeObj()
	obj.ID = object.ID(ev.Tags)

	i := makeIncident(muted, recovered)
	err := db.ExecTx(t.Context(), nil, func(ctx context.Context, tx *sqlx.Tx) error {
		objInsertStmt := `INSERT INTO object (id, name) VALUES (?, ?)`
		_, err := tx.ExecContext(t.Context(), db.Rebind(objInsertStmt), obj.ID, obj.Name)
		require.NoError(t, err)

		var recoveredAt types.UnixMilli
		startedAt := types.UnixMilli(time.Now().Add(-1 * time.Hour))
		if recovered {
			recoveredAt = types.UnixMilli(time.Now())
		}

		incidentInsertStmt := `INSERT INTO incident (id, object_id, started_at, recovered_at, severity) VALUES (?, ?, ?, ?, ?)`
		_, err = tx.ExecContext(t.Context(), db.Rebind(incidentInsertStmt), i.Id, obj.ID, startedAt, recoveredAt, "crit")
		require.NoError(t, err)
		return nil
	})
	require.NoError(t, err)

	if db.DriverName() == database.PostgreSQL {
		cleaner.Add("object", fmt.Sprintf("id = DECODE('%s', 'HEX')", obj.ID))
	} else {
		cleaner.Add("object", fmt.Sprintf("id = UNHEX('%s')", obj.ID))
	}
	cleaner.Add("incident", fmt.Sprintf("id = %d", i.Id))

	return i, obj, ev
}

func makeIncident(muted, recovered bool) *plugin.Incident {
	return &plugin.Incident{
		Id:          makeRandomNumber(),
		IsMuted:     muted,
		IsRecovered: recovered,
	}
}

func makeObj() *object.Object { return &object.Object{Name: "Sleepy Object"} }

func makeEvent(t *testing.T) *event.Event {
	return &event.Event{
		Time:    time.Now(),
		Name:    "Sleepy Object",
		Message: "Test event message",
		Tags: map[string]string{
			"source": "unit-test",
			"random": testutils.MakeRandomString(t),
		},
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
