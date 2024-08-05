package events

import (
	"context"
	"github.com/icinga/icinga-go-library/logging"
	"github.com/icinga/icinga-go-library/types"
	"github.com/icinga/icinga-notifications/internal/config"
	"github.com/icinga/icinga-notifications/internal/daemon"
	"github.com/icinga/icinga-notifications/internal/event"
	"github.com/icinga/icinga-notifications/internal/incident"
	"github.com/icinga/icinga-notifications/internal/object"
	"github.com/icinga/icinga-notifications/internal/testutils"
	"github.com/icinga/icinga-notifications/internal/utils"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"
	"testing"
	"time"
)

func TestProcess(t *testing.T) {
	ctx := context.Background()
	db := testutils.GetTestDB(ctx, t)

	require.NoError(t, daemon.InitTestConfig(), "mocking daemon.Config should not fail")

	// Insert a dummy source for our test cases!
	source := config.Source{Type: "notifications", Name: "Icinga Notifications", Icinga2InsecureTLS: types.Bool{Bool: false, Valid: true}}
	source.ChangedAt = types.UnixMilli(time.Now())
	source.Deleted = types.Bool{Bool: false, Valid: true}

	err := utils.RunInTx(ctx, db, func(tx *sqlx.Tx) error {
		id, err := utils.InsertAndFetchId(ctx, tx, utils.BuildInsertStmtWithout(db, source, "id"), source)
		require.NoError(t, err, "populating source table should not fail")

		source.ID = id
		return nil
	})
	require.NoError(t, err, "utils.RunInTx() should not fail")

	logs, err := logging.NewLogging("events-router", zapcore.DebugLevel, "console", nil, time.Hour)
	require.NoError(t, err, "logging initialisation should not fail")

	runtimeConfig := new(config.RuntimeConfig)

	t.Run("InvalidEvents", func(t *testing.T) {
		assert.Nil(t, Process(ctx, db, logs, runtimeConfig, makeEvent(t, source.ID, event.TypeState, event.SeverityNone)))
		assert.ErrorIs(t, Process(ctx, db, logs, runtimeConfig, makeEvent(t, source.ID, event.TypeState, event.SeverityOK)), event.ErrSuperfluousStateChange)
		assert.ErrorIs(t, Process(ctx, db, logs, runtimeConfig, makeEvent(t, source.ID, event.TypeAcknowledgementSet, event.SeverityOK)), event.ErrSuperfluousStateChange)
		assert.ErrorIs(t, Process(ctx, db, logs, runtimeConfig, makeEvent(t, source.ID, event.TypeAcknowledgementCleared, event.SeverityOK)), event.ErrSuperfluousStateChange)
	})

	t.Run("StateChangeEvents", func(t *testing.T) {
		states := map[string]*event.Event{
			"crit":  makeEvent(t, source.ID, event.TypeState, event.SeverityCrit),
			"warn":  makeEvent(t, source.ID, event.TypeState, event.SeverityWarning),
			"err":   makeEvent(t, source.ID, event.TypeState, event.SeverityErr),
			"alert": makeEvent(t, source.ID, event.TypeState, event.SeverityAlert),
		}

		for severity, ev := range states {
			assert.NoErrorf(t, Process(ctx, db, logs, runtimeConfig, ev), "state event with severity %q should open an incident", severity)
			assert.ErrorIsf(t, Process(ctx, db, logs, runtimeConfig, ev), event.ErrSuperfluousStateChange,
				"superfluous state event %q should be ignored", severity)

			obj := object.GetFromCache(object.ID(source.ID, ev.Tags))
			require.NotNil(t, obj, "there should be a cached object")

			i, err := incident.GetCurrent(ctx, db, obj, logs.GetLogger(), runtimeConfig, false)
			require.NoError(t, err, "retrieving current incident should not fail")
			require.NotNil(t, i, "there should be a cached incident")
			assert.Equal(t, ev.Severity, i.Severity, "severities should be equal")
		}

		reloadIncidents := func(ctx context.Context) {
			object.ClearCache()

			// Remove all existing incidents from the cache, as they are indexed with the
			// pointer of their object, which is going to change!
			for _, i := range incident.GetCurrentIncidents() {
				incident.RemoveCurrent(i.Object)
			}

			// The incident loading process may hang due to unknown bugs or semaphore lock waits.
			// Therefore, give it maximum time of 10s to finish normally, otherwise give up and fail.
			ctx, cancelFunc := context.WithDeadline(ctx, time.Now().Add(10*time.Second))
			defer cancelFunc()

			err := incident.LoadOpenIncidents(ctx, db, logging.NewLogger(zaptest.NewLogger(t).Sugar(), time.Hour), runtimeConfig)
			require.NoError(t, err, "loading active incidents should not fail")
		}
		reloadIncidents(ctx)

		for severity, ev := range states {
			obj, err := object.FromEvent(ctx, db, ev)
			assert.NoError(t, err)

			i, err := incident.GetCurrent(ctx, db, obj, logs.GetLogger(), runtimeConfig, false)
			assert.NoErrorf(t, err, "incident for event severity %q should be in cache", severity)

			assert.Equal(t, obj, i.Object, "incident and event object should be the same")
			assert.Equal(t, i.Severity, ev.Severity, "incident and event severity should be the same")
		}

		// Recover the incidents
		for _, ev := range states {
			ev.Time = time.Now()
			ev.Severity = event.SeverityOK

			assert.NoErrorf(t, Process(ctx, db, logs, runtimeConfig, ev), "state event with severity %q should close an incident", "ok")
		}
		reloadIncidents(ctx)
		assert.Len(t, incident.GetCurrentIncidents(), 0, "there should be no cached incidents")
	})

	t.Run("NonStateEvents", func(t *testing.T) {
		events := []*event.Event{
			makeEvent(t, source.ID, event.TypeDowntimeStart, event.SeverityNone),
			makeEvent(t, source.ID, event.TypeDowntimeEnd, event.SeverityNone),
			makeEvent(t, source.ID, event.TypeDowntimeRemoved, event.SeverityNone),
			makeEvent(t, source.ID, event.TypeCustom, event.SeverityNone),
			makeEvent(t, source.ID, event.TypeFlappingStart, event.SeverityNone),
			makeEvent(t, source.ID, event.TypeFlappingEnd, event.SeverityNone),
		}

		for _, ev := range events {
			assert.NoErrorf(t, Process(ctx, db, logs, runtimeConfig, ev), "processing non-state event %q should not fail", ev.Type)
			assert.Lenf(t, incident.GetCurrentIncidents(), 0, "non-state event %q should not open an incident", ev.Type)
			require.NotNil(t, object.GetFromCache(object.ID(source.ID, ev.Tags)), "there should be a cached object")
		}
	})
}

// makeEvent creates a fully initialised event.Event of the given type and severity.
func makeEvent(t *testing.T, sourceID int64, typ string, severity event.Severity) *event.Event {
	return &event.Event{
		SourceId: sourceID,
		Name:     testutils.MakeRandomString(t),
		URL:      "https://localhost/icingaweb2/icingadb",
		Type:     typ,
		Time:     time.Now(),
		Severity: severity,
		Username: "icingaadmin",
		Message:  "You will contract a rare disease :(",
		Tags: map[string]string{
			"Host":    testutils.MakeRandomString(t),
			"Service": testutils.MakeRandomString(t),
		},
		ExtraTags: map[string]string{
			"hostgroup/database-server": "",
			"servicegroup/webserver":    "",
		},
	}
}
