package events

import (
	"context"
	"testing"
	"time"

	"github.com/icinga/icinga-go-library/database"
	"github.com/icinga/icinga-go-library/logging"
	baseEv "github.com/icinga/icinga-go-library/notifications/event"
	"github.com/icinga/icinga-go-library/types"
	"github.com/icinga/icinga-notifications/internal/config"
	"github.com/icinga/icinga-notifications/internal/daemon"
	"github.com/icinga/icinga-notifications/internal/event"
	"github.com/icinga/icinga-notifications/internal/incident"
	"github.com/icinga/icinga-notifications/internal/object"
	"github.com/icinga/icinga-notifications/internal/testutils"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"
)

func TestProcessEvent(t *testing.T) {
	t.Parallel()

	require.NoError(t, daemon.InitTestConfig(), "mocking daemon.Config should not fail")

	source := &config.Source{
		Type: "notifications",
		Name: "Icinga Notifications",
	}
	source.ChangedAt = types.UnixMilli(time.Now())
	source.Deleted = types.MakeBool(false)

	ctx := t.Context()
	db := testutils.GetTestDB(ctx, t)

	err := db.ExecTx(ctx, func(ctx context.Context, tx *sqlx.Tx) error {
		id, err := database.InsertObtainID(ctx, tx, database.BuildInsertStmtWithout(db, source, "id"), source)
		require.NoError(t, err, "populating source table should not fail")
		source.ID = id
		return nil
	})
	require.NoError(t, err, "db.ExecTx() should not fail")

	logs := logging.NewLoggingWithFactory("testing", zapcore.DebugLevel, time.Hour, testutils.NewTestLoggerFactory(t))
	runtimeC := config.NewRuntimeConfig(logs, db)

	t.Run("Invalid", func(t *testing.T) {
		t.Parallel()

		assert.Nil(t, Process(ctx, runtimeC, makeEvent(t, source.ID, baseEv.TypeState, baseEv.SeverityNone)))
		assert.ErrorIs(t, Process(ctx, runtimeC, makeEvent(t, source.ID, baseEv.TypeState, baseEv.SeverityOK)), event.ErrSuperfluousStateChange)
		assert.ErrorIs(t, Process(ctx, runtimeC, makeEvent(t, source.ID, baseEv.TypeAcknowledgementSet, baseEv.SeverityOK)), event.ErrSuperfluousStateChange)
		assert.ErrorIs(t, Process(ctx, runtimeC, makeEvent(t, source.ID, baseEv.TypeAcknowledgementCleared, baseEv.SeverityOK)), event.ErrSuperfluousStateChange)

		ev := makeEvent(t, source.ID, baseEv.TypeState, baseEv.SeverityDebug)
		ev.Tags = nil // Let's make the event invalid by removing all tags.
		assert.Error(t, Process(ctx, runtimeC, ev))
	})

	t.Run("NonState", func(t *testing.T) {
		events := []*event.Event{
			makeEvent(t, source.ID, baseEv.TypeDowntimeStart, baseEv.SeverityNone),
			makeEvent(t, source.ID, baseEv.TypeDowntimeEnd, baseEv.SeverityNone),
			makeEvent(t, source.ID, baseEv.TypeDowntimeRemoved, baseEv.SeverityNone),
			makeEvent(t, source.ID, baseEv.TypeCustom, baseEv.SeverityNone),
			makeEvent(t, source.ID, baseEv.TypeFlappingStart, baseEv.SeverityNone),
			makeEvent(t, source.ID, baseEv.TypeFlappingEnd, baseEv.SeverityNone),
		}

		for _, ev := range events {
			assert.NoErrorf(t, Process(ctx, runtimeC, ev), ev.String())
			assert.Lenf(t, incident.GetCurrentIncidents(), 0, ev.String())
			assert.NotNil(t, object.GetFromCache(object.ID(source.ID, ev.Tags)))
		}

		ev := makeEvent(t, source.ID, baseEv.TypeUnmute, baseEv.SeverityNone)
		ev.SetMute(false, "You shall be unmuted!")
		// One can't unmute an object that is not muted.
		assert.ErrorIs(t, Process(ctx, runtimeC, ev), event.ErrSuperfluousMuteUnmuteEvent)

		ev = makeEvent(t, source.ID, baseEv.TypeMute, baseEv.SeverityNone)
		ev.SetMute(true, "You shall be muted!")
		assert.NoError(t, Process(ctx, runtimeC, ev))
		assert.True(t, object.GetFromCache(object.ID(source.ID, ev.Tags)).IsMuted())
		// Muting an already muted object should return event.ErrSuperfluousMuteUnmuteEvent.
		assert.ErrorIs(t, Process(ctx, runtimeC, ev), event.ErrSuperfluousMuteUnmuteEvent)

		ev.ID = 0
		ev.Time = time.Now()
		ev.Type = baseEv.TypeUnmute
		ev.SetMute(false, "You shall be unmuted!")
		assert.NoError(t, Process(ctx, runtimeC, ev))
		assert.False(t, object.GetFromCache(object.ID(source.ID, ev.Tags)).IsMuted())
		// Once again, unmuting an already unmuted object should return event.ErrSuperfluousMuteUnmuteEvent.
		assert.ErrorIs(t, Process(ctx, runtimeC, ev), event.ErrSuperfluousMuteUnmuteEvent)
	})

	t.Run("StateChange", func(t *testing.T) {
		events := []*event.Event{
			makeEvent(t, source.ID, baseEv.TypeState, baseEv.SeverityDebug),
			makeEvent(t, source.ID, baseEv.TypeState, baseEv.SeverityInfo),
			makeEvent(t, source.ID, baseEv.TypeState, baseEv.SeverityNotice),
			makeEvent(t, source.ID, baseEv.TypeState, baseEv.SeverityWarning),
			makeEvent(t, source.ID, baseEv.TypeState, baseEv.SeverityErr),
			makeEvent(t, source.ID, baseEv.TypeState, baseEv.SeverityCrit),
			makeEvent(t, source.ID, baseEv.TypeState, baseEv.SeverityAlert),
			makeEvent(t, source.ID, baseEv.TypeState, baseEv.SeverityEmerg),
		}

		for _, ev := range events {
			assert.NoError(t, Process(ctx, runtimeC, ev), ev.String())
			assert.ErrorIs(t, Process(ctx, runtimeC, ev), event.ErrSuperfluousStateChange, ev.String())

			obj := object.GetFromCache(object.ID(source.ID, ev.Tags))
			require.NotNil(t, obj)

			inc, err := incident.GetCurrent(ctx, obj, runtimeC, false)
			require.NoError(t, err)
			require.NotNil(t, inc)
			assert.Equal(t, ev.Severity, inc.Severity)
			assert.Equal(t, types.UnixMilli(ev.Time), inc.StartedAt)
		}

		// reloadIncidents clears the object and incident caches and reloads all open incidents from the database.
		// This simulates a daemon restart and ensures that incidents are properly restored from the database. It
		// also ensures that the incident loading process does not hang indefinitely by enforcing a timeout of 5s.
		reloadIncidents := func(ctx context.Context) {
			object.ClearCache()
			// Clear all existing incidents from the cache, as they are indexed with the pointer of
			// their object, which is going to change, since we cleared the object cache already.
			incident.ClearCache()

			// The incident loading process may hang due to unknown bugs or semaphore lock waits.
			// Therefore, give it maximum time of 5s to finish normally, otherwise give up and fail.
			ctx, cancelFunc := context.WithDeadline(ctx, time.Now().Add(5*time.Second))
			defer cancelFunc()
			require.NoError(t, incident.LoadOpenIncidents(ctx, db, runtimeC))
		}
		reloadIncidents(ctx)

		for _, ev := range events {
			obj, err := object.FromEvent(ctx, db, ev)
			assert.NoError(t, err, ev.String())

			i, err := incident.GetCurrent(ctx, obj, runtimeC, false)
			assert.NoErrorf(t, err, ev.String())
			assert.Equal(t, obj, i.Object)
			assert.Equal(t, i.Severity, ev.Severity)
		}

		// Now, close all incidents by sending OK state events and should remove all incidents from the cache.
		// Afterwards, reloading the incidents should not find any active incidents anymore.
		for _, ev := range events {
			ev.Time = time.Now()
			ev.Severity = baseEv.SeverityOK
			assert.NoErrorf(t, Process(ctx, runtimeC, ev), ev.String())
		}
		reloadIncidents(ctx)
		assert.Len(t, incident.GetCurrentIncidents(), 0)
	})
}

// makeEvent creates a new [event.Event] with random Name, Host and Service tags.
//
// Calling makeEvent multiple times (even with the same arguments) will yield distinct events,
// that target a different [object.Object] due to the random tags. Thus, the only way to submit
// multiple events for the same object is to copy an existing event and modify its state (e.g. Severity).
//
// The returned event.Event will have its ID set to 0 and its Time set to the current time.
func makeEvent(t *testing.T, sourceID int64, typ baseEv.Type, severity baseEv.Severity) *event.Event {
	return &event.Event{
		SourceId: sourceID,
		Time:     time.Now(),
		Event: baseEv.Event{
			Name:     testutils.MakeRandomString(t),
			URL:      "/icingadb",
			Type:     typ,
			Severity: severity,
			Username: "icingaadmin",
			Message:  "In loops we trust, in tests we pray till Friday deploys take it away :(",
			Tags: map[string]string{
				"Host":    testutils.MakeRandomString(t),
				"Service": testutils.MakeRandomString(t),
			},
		},
	}
}
