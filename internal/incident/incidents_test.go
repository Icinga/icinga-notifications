package incident

import (
	"context"
	"github.com/icinga/icinga-go-library/database"
	"github.com/icinga/icinga-go-library/logging"
	"github.com/icinga/icinga-go-library/types"
	"github.com/icinga/icinga-notifications/internal/config"
	"github.com/icinga/icinga-notifications/internal/event"
	"github.com/icinga/icinga-notifications/internal/object"
	"github.com/icinga/icinga-notifications/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
	"testing"
	"time"
)

func TestLoadOpenIncidents(t *testing.T) {
	ctx := context.Background()
	db := testutils.GetTestDB(ctx, t)

	// Insert a dummy sources for our test cases!
	source := config.Source{ID: 1, Type: "notifications", Name: "Icinga Notifications", Icinga2InsecureTLS: types.Bool{Bool: false, Valid: true}}
	stmt, _ := db.BuildInsertStmt(source)
	_, err := db.NamedExecContext(ctx, stmt, source)
	require.NoError(t, err, "populating source table should not fail")

	// Reduce the default placeholders per statement to a meaningful number, so that we can
	// test some parallelism when loading the incidents.
	db.Options.MaxPlaceholdersPerStatement = 100

	// Due to the 10*maxPlaceholders constraint, only 10 goroutines are going to process simultaneously.
	// Therefore, reduce the default maximum number of connections per table to 4 in order to fully simulate
	// semaphore lock wait cycles for a given table.
	db.Options.MaxConnectionsPerTable = 4

	testData := make(map[string]*Incident, 10*db.Options.MaxPlaceholdersPerStatement)
	for j := 1; j <= 10*db.Options.MaxPlaceholdersPerStatement; j++ {
		i := makeIncident(ctx, db, t, false)
		testData[i.ObjectID.String()] = i
	}

	t.Run("WithNoRecoveredIncidents", func(t *testing.T) {
		assertIncidents(ctx, db, t, testData)
	})

	t.Run("WithSomeRecoveredIncidents", func(t *testing.T) {
		tx, err := db.BeginTxx(ctx, nil)
		require.NoError(t, err, "starting a transaction should not fail")

		// Drop all cached incidents before re-loading them!
		for _, i := range GetCurrentIncidents() {
			RemoveCurrent(i.Object)

			// Mark some of the existing incidents as recovered.
			if i.Id%20 == 0 { // 1000 / 20 => 50 existing incidents will be marked as recovered!
				i.RecoveredAt = types.UnixMilli(time.Now())

				require.NoError(t, i.Sync(ctx, tx), "failed to update/insert incident")

				// Drop it from our test data as it's recovered
				delete(testData, i.ObjectID.String())
			}
		}
		require.NoError(t, tx.Commit(), "committing a transaction should not fail")

		assert.Len(t, GetCurrentIncidents(), 0, "there should be no cached incidents")

		for j := 1; j <= db.Options.MaxPlaceholdersPerStatement/2; j++ {
			// We don't need to cache recovered incidents in memory.
			_ = makeIncident(ctx, db, t, true)

			if j%2 == 0 {
				// Add some extra new not recovered incidents to fully simulate a daemon reload.
				i := makeIncident(ctx, db, t, false)
				testData[i.ObjectID.String()] = i
			}
		}

		assertIncidents(ctx, db, t, testData)
	})
}

// assertIncidents restores all not recovered incidents from the database and asserts them based on the given testData.
//
// The incident loading process is limited to a maximum duration of 10 seconds and will be
// aborted and causes the entire test suite to fail immediately, if it takes longer.
func assertIncidents(ctx context.Context, db *database.DB, t *testing.T, testData map[string]*Incident) {
	logger := logging.NewLogger(zaptest.NewLogger(t).Sugar(), time.Hour)

	// Since we have been using object.FromEvent() to persist the test objects to the database,
	// these will be automatically added to the objects cache as well. So clear the cache before
	// reloading the incidents, otherwise it will panic in object.RestoreObjects().
	object.ClearCache()

	// The incident loading process may hang due to unknown bugs or semaphore lock waits.
	// Therefore, give it maximum time of 10s to finish normally, otherwise give up and fail.
	ctx, cancelFunc := context.WithDeadline(ctx, time.Now().Add(10*time.Second))
	defer cancelFunc()

	err := LoadOpenIncidents(ctx, db, logger, &config.RuntimeConfig{})
	require.NoError(t, err, "failed to load not recovered incidents")

	incidents := GetCurrentIncidents()
	assert.Len(t, incidents, len(testData), "failed to load all active incidents")

	for _, current := range incidents {
		i := testData[current.ObjectID.String()]
		assert.NotNilf(t, i, "found mysterious incident that's not part of our test data")
		assert.NotNil(t, current.Object, "failed to restore incident object")

		if i != nil {
			assert.Equal(t, i.Id, current.Id, "incidents linked to the same object don't have the same ID")
			assert.Equal(t, i.Severity, current.Severity, "failed to restore incident severity")
			assert.Equal(t, i.StartedAt, current.StartedAt, "failed to restore incident started at")
			assert.Equal(t, i.RecoveredAt, current.RecoveredAt, "failed to restore incident recovered at")

			assert.NotNil(t, current.EscalationState, "incident escalation state map should've initialised")
			assert.NotNil(t, current.Recipients, "incident recipients map should've initialised")
			assert.NotNil(t, current.Rules, "incident rules map should've initialised")

			if current.Object != nil {
				assert.Equal(t, i.Object, current.Object, "failed to fully restore incident")
			}
		}
	}
}

// makeIncident returns a fully initialised recovered/un-recovered incident.
//
// This will firstly create and synchronise a new object from a freshly generated dummy event with distinct
// tags and name, and ensures that no error is returned, otherwise it will cause the entire test suite to fail.
// Once the object has been successfully synchronised, an incident is created and synced with the database.
func makeIncident(ctx context.Context, db *database.DB, t *testing.T, recovered bool) *Incident {
	ev := &event.Event{
		Time:     time.Time{},
		SourceId: 1,
		Name:     testutils.MakeRandomString(t),
		Tags: map[string]string{ // Always generate unique object tags not to produce same object ID!
			"host":    testutils.MakeRandomString(t),
			"service": testutils.MakeRandomString(t),
		},
		ExtraTags: map[string]string{
			"hostgroup/database-server": "",
			"servicegroup/webserver":    "",
		},
	}

	o, err := object.FromEvent(ctx, db, ev)
	require.NoError(t, err)

	i := NewIncident(db, o, &config.RuntimeConfig{}, nil)
	i.StartedAt = types.UnixMilli(time.Now().Add(-2 * time.Hour).Truncate(time.Second))
	i.Severity = event.SeverityCrit
	if recovered {
		i.Severity = event.SeverityOK
		i.RecoveredAt = types.UnixMilli(time.Now())
	}

	tx, err := db.BeginTxx(ctx, nil)
	require.NoError(t, err, "starting a transaction should not fail")
	require.NoError(t, i.Sync(ctx, tx), "failed to insert incident")
	require.NoError(t, tx.Commit(), "committing a transaction should not fail")

	return i
}
