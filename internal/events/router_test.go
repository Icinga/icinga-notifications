package events

import (
	"context"
	"github.com/icinga/icinga-notifications/internal/config"
	"github.com/icinga/icinga-notifications/internal/daemon"
	"github.com/icinga/icinga-notifications/internal/event"
	"github.com/icinga/icinga-notifications/internal/incident"
	"github.com/icinga/icinga-notifications/internal/object"
	"github.com/icinga/icinga-notifications/internal/testutils"
	"github.com/icinga/icingadb/pkg/logging"
	"github.com/icinga/icingadb/pkg/types"
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

	require.NoError(t, daemon.LoadConfig("../../config.example.yml"), "loading config file should not fail")

	// Insert a dummy sources for our test cases!
	source := config.Source{ID: 10, Type: "notifications", Name: "Icinga Notifications", Icinga2InsecureTLS: types.Bool{Bool: false, Valid: true}}
	stmt, _ := db.BuildInsertStmt(source)
	_, err := db.NamedExecContext(ctx, stmt, source)
	require.NoError(t, err, "populating source table should not fail")

	logs, err := logging.NewLogging("testing", zapcore.DebugLevel, "console", nil, time.Hour)
	require.NoError(t, err, "logging initialisation should not fail")

	// Load the just populated config into the runtime config.
	runtimeConfig := config.NewRuntimeConfig(nil, logs, db)
	require.NoError(t, runtimeConfig.UpdateFromDatabase(ctx), "fetching configs from database should not fail")

	t.Run("InvalidEvents", func(t *testing.T) {
		// These events require an active incident to be processed successfully.
		assert.ErrorIs(t, Process(ctx, db, logs, runtimeConfig, makeEvent(t, event.TypeState, event.SeverityOK)), event.ErrSuperfluousStateChange)
		assert.ErrorIs(t, Process(ctx, db, logs, runtimeConfig, makeEvent(t, event.TypeState, event.SeverityNone)), event.ErrEventProcessing)
		assert.ErrorIs(t, Process(ctx, db, logs, runtimeConfig, makeEvent(t, event.TypeAcknowledgementSet, event.SeverityOK)), event.ErrEventProcessing)
		assert.ErrorIs(t, Process(ctx, db, logs, runtimeConfig, makeEvent(t, event.TypeAcknowledgementCleared, event.SeverityOK)), event.ErrEventProcessing)
	})

	t.Run("StateChangeEvents", func(t *testing.T) {
		states := map[string]*event.Event{
			"crit":  makeEvent(t, event.TypeState, event.SeverityCrit),
			"warn":  makeEvent(t, event.TypeState, event.SeverityWarning),
			"err":   makeEvent(t, event.TypeState, event.SeverityErr),
			"alert": makeEvent(t, event.TypeState, event.SeverityAlert),
		}

		for severity, ev := range states {
			assert.NoErrorf(t, Process(ctx, db, logs, runtimeConfig, ev), "state event with severity %q should open an incident", severity)
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
		states := []*event.Event{
			makeEvent(t, event.TypeDowntimeStart, event.SeverityNone),
			makeEvent(t, event.TypeDowntimeEnd, event.SeverityOK),
			makeEvent(t, event.TypeDowntimeRemoved, event.SeverityNone),
			makeEvent(t, event.TypeCustom, event.SeverityOK),
			makeEvent(t, event.TypeFlappingStart, event.SeverityNone),
			makeEvent(t, event.TypeFlappingEnd, event.SeverityNone),
		}

		for _, ev := range states {
			assert.NoErrorf(t, Process(ctx, db, logs, runtimeConfig, ev), "processing non-state event %q should not fail", ev.Type)
			assert.Lenf(t, incident.GetCurrentIncidents(), 0, "non-state event %q should not open an incident", ev.Type)
		}
	})
}

// makeEvent creates a fully initialised event.Event of the given type and severity.
func makeEvent(t *testing.T, typ string, severity event.Severity) *event.Event {
	return &event.Event{
		SourceId: 10,
		Name:     testutils.MakeRandomString(t),
		URL:      "https://localhost/icingaweb2/icingadb",
		Type:     typ,
		Time:     time.Now(),
		Severity: severity,
		Username: "icingaadmin",
		Message:  "You will contract a rare disease",
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
