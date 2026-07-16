package incident

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/icinga/icinga-go-library/database"
	"github.com/icinga/icinga-go-library/logging"
	baseEv "github.com/icinga/icinga-go-library/notifications/event"
	"github.com/icinga/icinga-go-library/types"
	"github.com/icinga/icinga-notifications/internal/config"
	"github.com/icinga/icinga-notifications/internal/event"
	"github.com/icinga/icinga-notifications/internal/object"
	"github.com/icinga/icinga-notifications/internal/rule"
	"github.com/icinga/icinga-notifications/internal/testutils"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"
)

func TestIncidents(t *testing.T) {
	t.Parallel()

	db := testutils.GetTestDB(t.Context(), t)
	logs := logging.NewLoggingWithFactory("testing", zapcore.DebugLevel, time.Second, func(level zap.AtomicLevel) zapcore.Core {
		return zaptest.NewLogger(t, zaptest.Level(level.Level())).Core()
	})

	// Insert a dummy source for our test cases!
	source := &config.Source{
		Type:             "notifications",
		Name:             "Icinga Notifications",
		ListenerUsername: types.MakeString("notifications"),
	}
	source.ChangedAt = types.UnixMilli(time.Date(2009, time.November, 10, 23, 0, 0, 0, time.UTC))
	source.Deleted = types.Bool{Bool: false, Valid: true}

	err := db.ExecTx(t.Context(), nil, func(ctx context.Context, tx *sqlx.Tx) error {
		id, err := database.InsertObtainID(ctx, tx, database.BuildInsertStmtWithout(db, source, "id"), source)
		require.NoError(t, err, "populating source table should not fail")

		source.ID = id
		return nil
	})
	require.NoError(t, err, "db.ExecTx should not fail")

	t.Cleanup(func() {
		// Cleanup all the database tables, so that one can just re-run the tests without having either to re-create
		// the database or to manually clean it up. We can't use t.Context() here though, as it will be canceled before
		// our cleanup function gets called, so we need to create a new context with a timeout for the cleanup.
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cleanupDB(ctx, db, t)
	})

	runtimeConfig := config.NewRuntimeConfig(logs, db)
	require.NoError(t, runtimeConfig.UpdateFromDatabase(t.Context()))

	t.Run("LoadOpenIncidents", func(t *testing.T) {
		// Reduce the default placeholders per statement to a meaningful number, so that we can
		// test some parallelism when loading the incidents.
		db.Options.MaxPlaceholdersPerStatement = 100

		// Due to the 10*maxPlaceholders constraint, only 10 goroutines are going to process simultaneously.
		// Therefore, reduce the default maximum number of connections per table to 4 in order to fully simulate
		// semaphore lock wait cycles for a given table.
		db.Options.MaxConnectionsPerTable = 4

		testData := make(map[string]*Incident, 10*db.Options.MaxPlaceholdersPerStatement)
		for j := 1; j <= 10*db.Options.MaxPlaceholdersPerStatement; j++ {
			i := makeIncident(db, logs, runtimeConfig, t, makeEvent(t, source.ID, withIncident(), withSeverity(baseEv.SeverityCrit)))
			testData[i.ObjectID.String()] = i
		}

		t.Run("WithNoRecoveredIncidents", func(t *testing.T) {
			assertIncidents(t.Context(), db, logs, runtimeConfig, t, testData)
		})

		t.Run("WithSomeRecoveredIncidents", func(t *testing.T) {
			pairCh, errCh := Yield(t.Context(), db, logs, runtimeConfig)
			for pair := range pairCh {
				// Mark some of the existing incidents as recovered.
				if pair.Incident.Id%20 == 0 { // 1000 / 20 => 50 existing incidents will be marked as recovered!
					require.NoError(t, ProcessEvent(t.Context(), db, logs, runtimeConfig, makeEvent(t, source.ID,
						withIncident(), withClose(), withTags(pair.Object.Tags))))
					require.NotZero(t, reloadIncident(t, db, pair.Incident).RecoveredAt)
					delete(testData, pair.Object.ID.String())
				}
			}
			assert.NoError(t, <-errCh)

			var incidentsLen int
			pairCh, errCh = Yield(t.Context(), db, logs, runtimeConfig)
			for range pairCh {
				incidentsLen++
			}
			assert.NoError(t, <-errCh)
			assert.Equal(t, len(testData), incidentsLen, "only the recovered incidents should be gone")

			for j := 1; j <= db.Options.MaxPlaceholdersPerStatement/2; j++ {
				require.NoError(t, ProcessEvent(t.Context(), db, logs, runtimeConfig, makeEvent(t, source.ID,
					withIncident(), withClose(), withSeverity(baseEv.SeverityAlert))))

				if j%2 == 0 {
					// Add some extra new not recovered incidents to fully simulate a daemon reload.
					i := makeIncident(db, logs, runtimeConfig, t, makeEvent(t, source.ID,
						withIncident(), withSeverity(baseEv.SeverityWarning)))
					testData[i.ObjectID.String()] = i
				}
			}

			assertIncidents(t.Context(), db, logs, runtimeConfig, t, testData)

			// Close all remaining incidents to clean up the database for the next test run.
			pairCh, errCh = Yield(t.Context(), db, logs, runtimeConfig)
			for pair := range pairCh {
				require.NoError(t, ProcessEvent(t.Context(), db, logs, runtimeConfig, makeEvent(t, source.ID,
					withIncident(), withClose(), withTags(pair.Object.Tags))))
			}
			assert.NoError(t, <-errCh)

			incidentsLen = 0
			pairCh, errCh = Yield(t.Context(), db, logs, runtimeConfig)
			for range pairCh {
				incidentsLen++
			}
			assert.NoError(t, <-errCh)
			assert.Equal(t, 0, incidentsLen, "there should be no active incidents")
			testData = make(map[string]*Incident) // Reset test data for the next test run.
		})
	})

	t.Run("Severity Change", func(t *testing.T) {
		t.Parallel()

		i := makeIncident(db, logs, runtimeConfig, t, makeEvent(t, source.ID, withIncident(), withSeverity(baseEv.SeverityDebug)))
		assert.NotZero(t, i.ID())
		assert.Zero(t, i.RecoveredAt)
		assert.Equal(t, baseEv.SeverityDebug, i.Severity)

		require.NoError(t, ProcessEvent(t.Context(), db, logs, runtimeConfig, makeEvent(t, source.ID,
			withIncident(), withSeverity(baseEv.SeverityEmerg), withTags(mustIncidentObject(t, i).Tags))))
		i = reloadIncident(t, db, i)
		assert.Equal(t, baseEv.SeverityEmerg, i.Severity)

		err := ProcessEvent(t.Context(), db, logs, runtimeConfig, makeEvent(t, source.ID,
			withMuted(false), withSeverity(baseEv.SeverityNotice), withTags(mustIncidentObject(t, i).Tags)))
		require.ErrorIs(t, err, ErrSeverityChangeWithoutIncidentFlag)
		i = reloadIncident(t, db, i)
		assert.Equal(t, baseEv.SeverityEmerg, i.Severity)
	})

	t.Run("Incident Open", func(t *testing.T) {
		t.Parallel()

		i := makeIncident(db, logs, runtimeConfig, t, makeEvent(t, source.ID, withIncident(), withSeverity(baseEv.SeverityDebug)))
		assert.NotZero(t, i.ID())
		assert.Zero(t, i.RecoveredAt)
		assert.Equal(t, baseEv.SeverityDebug, i.Severity)

		// Attempting to open an incident without a severity should fail.
		err := ProcessEvent(t.Context(), db, logs, runtimeConfig, makeEvent(t, source.ID, withIncident()))
		require.ErrorIs(t, err, ErrOpenIncidentWithoutSeverity)

		i = makeIncident(db, logs, runtimeConfig, t, makeEvent(t, source.ID,
			withIncident(), withSeverity(baseEv.SeverityEmerg), withMsg("Incident opened!")))
		assert.NotZero(t, i.ID())
		assert.Equal(t, baseEv.SeverityEmerg, i.Severity)
		assert.Equal(t, "Incident opened!", i.Message.String)

		require.NoError(t, ProcessEvent(t.Context(), db, logs, runtimeConfig, makeEvent(t, source.ID,
			withIncident(),
			withSeverity(baseEv.SeverityEmerg),
			withMsg("Incident updated!"),
			withTags(mustIncidentObject(t, i).Tags))))
		i = reloadIncident(t, db, i)
		assert.Equal(t, baseEv.SeverityEmerg, i.Severity)
		assert.Equal(t, "Incident updated!", i.Message.String)

		// We shouldn't be able to update the incident message without the incident flag set.
		require.NoError(t, ProcessEvent(t.Context(), db, logs, runtimeConfig,
			makeEvent(t, source.ID, withMuted(false), withMsg("YOLO!"), withTags(mustIncidentObject(t, i).Tags))))
		i = reloadIncident(t, db, i)
		assert.Equal(t, "Incident updated!", i.Message.String)
	})

	t.Run("Close Flag", func(t *testing.T) {
		t.Parallel()

		// Incident opened and closed immediately, so it's no longer active.
		require.Nil(t, makeIncident(db, logs, runtimeConfig, t, makeEvent(t, source.ID,
			withIncident(), withClose(), withSeverity(baseEv.SeverityDebug))))

		i := makeIncident(db, logs, runtimeConfig, t, makeEvent(t, source.ID,
			withIncident(), withSeverity(baseEv.SeverityInfo)))
		assert.Zero(t, i.RecoveredAt)
		assert.Equal(t, baseEv.SeverityInfo, i.Severity)

		// Closing incident with a new severity will update the severity and mark it as recovered.
		require.NoError(t, ProcessEvent(t.Context(), db, logs, runtimeConfig, makeEvent(t, source.ID,
			withIncident(), withClose(), withSeverity(baseEv.SeverityEmerg), withTags(mustIncidentObject(t, i).Tags))))
		i = reloadIncident(t, db, i)
		assert.NotZero(t, i.RecoveredAt)
		assert.Equal(t, baseEv.SeverityEmerg, i.Severity)

		i = makeIncident(db, logs, runtimeConfig, t, makeEvent(t, source.ID, withIncident(), withSeverity(baseEv.SeverityWarning)))
		assert.Zero(t, i.RecoveredAt)
		assert.Equal(t, baseEv.SeverityWarning, i.Severity)

		// Closing incident without providing a severity will keep the existing severity and mark it as recovered.
		require.NoError(t, ProcessEvent(t.Context(), db, logs, runtimeConfig, makeEvent(t, source.ID,
			withIncident(), withClose(), withTags(mustIncidentObject(t, i).Tags))))
		i = reloadIncident(t, db, i)
		assert.NotZero(t, i.RecoveredAt)
		assert.Equal(t, baseEv.SeverityWarning, i.Severity)
	})

	t.Run("Notify Flag", func(t *testing.T) {
		t.Skipf("Skipping Notify Flag test, as it requires to verify whether notifications were sent")
	})

	t.Run("Muted Flag", func(t *testing.T) {
		t.Parallel()

		i := makeIncident(db, logs, runtimeConfig, t, makeEvent(t, source.ID,
			withIncident(), withSeverity(baseEv.SeverityDebug), withMuted(true)))
		assert.Equal(t, baseEv.SeverityDebug, i.Severity)
		assert.True(t, i.IsMuted())
		assert.Equal(t, "You're gonna have a bad time!", i.MuteReason.String)

		// Unmute it with the incident flag still set...
		require.NoError(t, ProcessEvent(t.Context(), db, logs, runtimeConfig, makeEvent(t, source.ID,
			withIncident(), withMuted(false), withTags(mustIncidentObject(t, i).Tags))))
		i = reloadIncident(t, db, i)
		assert.Equal(t, baseEv.SeverityDebug, i.Severity)
		assert.False(t, i.IsMuted())
		assert.Equal(t, "", i.MuteReason.String)

		i = makeIncident(db, logs, runtimeConfig, t, makeEvent(t, source.ID,
			withIncident(), withSeverity(baseEv.SeverityDebug), withMuted(true)))
		assert.Equal(t, baseEv.SeverityDebug, i.Severity)
		assert.True(t, i.IsMuted())
		assert.Equal(t, "You're gonna have a bad time!", i.MuteReason.String)

		// Unmute it without the incident flag set...
		require.NoError(t, ProcessEvent(t.Context(), db, logs, runtimeConfig, makeEvent(t, source.ID,
			withMuted(false), withTags(mustIncidentObject(t, i).Tags))))
		i = reloadIncident(t, db, i)
		assert.Equal(t, baseEv.SeverityDebug, i.Severity)
		assert.False(t, i.IsMuted())
		assert.Equal(t, "", i.MuteReason.String)

		// Muted flag without the incident flag has no effect on non-existing incidents.
		i = makeIncident(db, logs, runtimeConfig, t, makeEvent(t, source.ID, withMuted(true)))
		require.Nil(t, i)
	})

	t.Run("Time-Based Escalation", func(t *testing.T) {
		t.Parallel()

		// Dedicated source to not interact with other subtests.
		escalationSource := &config.Source{
			Type:             "notifications",
			Name:             "Icinga Notifications Escalations",
			ListenerUsername: types.MakeString("notifications-escalations"),
		}
		escalationSource.ChangedAt = types.UnixMilli(time.Now())
		escalationSource.Deleted = types.MakeBool(false)

		escalationRule := &rule.Rule{Name: "escalation test rule"}
		escalationRule.ChangedAt = escalationSource.ChangedAt
		escalationRule.Deleted = escalationSource.Deleted

		err := db.ExecTx(t.Context(), nil, func(ctx context.Context, tx *sqlx.Tx) error {
			id, err := database.InsertObtainID(ctx, tx, database.BuildInsertStmtWithout(db, escalationSource, "id"), escalationSource)
			assert.NoError(t, err)
			escalationSource.ID = id

			escalationRule.SourceID = id
			id, err = database.InsertObtainID(ctx, tx, database.BuildInsertStmtWithout(db, escalationRule, "id"), escalationRule)
			assert.NoError(t, err)
			escalationRule.ID = id

			escalation := &escalationRow{
				RuleID:    id,
				Position:  1,
				Condition: "incident_age>=1h",
				ChangedAt: escalationSource.ChangedAt,
				Deleted:   escalationSource.Deleted,
			}
			stmt, _ := db.BuildInsertStmt(escalation)
			_, err = tx.NamedExecContext(ctx, stmt, escalation)
			assert.NoError(t, err)
			return nil
		})
		assert.NoError(t, err)
		assert.NoError(t, runtimeConfig.UpdateFromDatabase(t.Context()))

		i := makeIncident(db, logs, runtimeConfig, t,
			makeEvent(t, escalationSource.ID, withIncident(), withSeverity(baseEv.SeverityCrit)))
		assert.NotNil(t, i)
		assert.NotZero(t, i.NextEscalationCheckAt)
		assert.WithinDuration(t, i.StartedAt.Time().Add(time.Hour), i.NextEscalationCheckAt.Time(), time.Second)

		selectStates := func() (states []*EscalationState) {
			assert.NoError(t, db.SelectContext(
				t.Context(),
				&states,
				db.Rebind(db.BuildSelectStmt(new(EscalationState), new(EscalationState))+` WHERE "incident_id" = ?`),
				i.Id))
			return
		}
		assert.Empty(t, selectStates())

		var ruleRows []*RuleRow
		assert.NoError(t, db.SelectContext(t.Context(), &ruleRows,
			db.Rebind(db.BuildSelectStmt(new(RuleRow), new(RuleRow))+` WHERE "incident_id" = ?`), i.Id))
		assert.Len(t, ruleRows, 1)

		assert.NoError(t, ReevaluateEscalations(t.Context(), db, logs.GetChildLogger("incident"), runtimeConfig))

		assert.Len(t, selectStates(), 1)
		i = reloadIncident(t, db, i)
		assert.Zero(t, i.NextEscalationCheckAt)
	})
}

// assertIncidents restores all not recovered incidents from the database and asserts them based on the given testData.
//
// The incident loading process is limited to a maximum duration of 10 seconds and will be
// aborted and causes the entire test suite to fail immediately, if it takes longer.
func assertIncidents(ctx context.Context, db *database.DB, l *logging.Logging, rc *config.RuntimeConfig, t *testing.T, testData map[string]*Incident) {
	// The incident loading process may hang due to unknown bugs or semaphore lock waits.
	// Therefore, give it maximum time of 10s to finish normally, otherwise give up and fail.
	ctx, cancelFunc := context.WithDeadline(ctx, time.Now().Add(10*time.Second))
	defer cancelFunc()

	var incidentsLen int
	pairCh, errCh := Yield(ctx, db, l, rc)
	for pair := range pairCh {
		incidentsLen++
		current := pair.Incident
		i := testData[current.ObjectID.String()]
		assert.NotNilf(t, i, "found mysterious incident that's not part of our test data")
		assert.NotNil(t, mustIncidentObject(t, current), "failed to restore incident object")

		if i != nil {
			assert.Equal(t, i.Id, current.Id, "incidents linked to the same object don't have the same ID")
			assert.Equal(t, i.Severity, current.Severity, "failed to restore incident severity")
			assert.Equal(t, i.StartedAt, current.StartedAt, "failed to restore incident started at")
			assert.Equal(t, i.RecoveredAt, current.RecoveredAt, "failed to restore incident recovered at")

			assert.NotNil(t, current.EscalationState, "incident escalation state map should've initialised")
			assert.NotNil(t, current.Recipients, "incident recipients map should've initialised")
			assert.NotNil(t, current.Rules, "incident rules map should've initialised")

			assert.Equal(t, mustIncidentObject(t, i), mustIncidentObject(t, current), "failed to fully restore incident")
		}
	}
	assert.NoError(t, <-errCh)
	assert.Equal(t, len(testData), incidentsLen, "failed to load all active incidents")
}

// cleanupDB removes all test data from the database tables used by the incident package.
//
// If we introduce new tests in the future that require additional database tables, we need
// to add them to the list of tables to clean up here.
func cleanupDB(ctx context.Context, db *database.DB, t *testing.T) {
	switch db.DriverName() {
	case database.PostgreSQL:
		// As opposed to MySQL, we can just use truncate to clean up all tables in one go.
		_, err := db.ExecContext(ctx, `TRUNCATE TABLE source RESTART IDENTITY CASCADE`)
		require.NoError(t, err)
	case database.MySQL:
		// InnoDB doesn't support truncating tables with foreign key constraints, so we need to delete
		// each table one by one in the correct order, not to violate any foreign key constraints.
		tables := []string{
			"incident_history",
			"incident_rule_escalation_state",
			"incident_rule",
			"incident",
			"rule_escalation",
			"rule",
			"object_id_tag",
			"object",
			"source",
		}

		for _, table := range tables {
			_, err := db.ExecContext(ctx, "DELETE FROM "+table)
			require.NoErrorf(t, err, "failed to clean up table %s", table)
		}
	default:
		t.Fatalf("unsupported database driver: %s", db.DriverName())
	}
}

// makeIncident creates a new incident by processing the given event and returns the resulting incident object.
//
// The incident is guaranteed to be fully initialized and ready for assertions but might be nil if it's immediately closed.
func makeIncident(db *database.DB, logs *logging.Logging, runtimeConfig *config.RuntimeConfig, t *testing.T, ev *event.Event) *Incident {
	require.NoError(t, ProcessEvent(t.Context(), db, logs, runtimeConfig, ev))
	i := new(Incident)
	i.ObjectID = object.ID(ev.SourceId, ev.Tags)
	i.initializeFields(db, runtimeConfig, logs.GetChildLogger("incident").SugaredLogger)
	stmt := db.Rebind(db.BuildSelectStmt(i, i) + ` WHERE "recovered_at" IS NULL AND "object_id" = ?`)
	err := db.GetContext(t.Context(), i, stmt, i.ObjectID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	require.NoError(t, err)
	return i
}

// reloadIncident reload the given Incident from the database and returns a new one.
func reloadIncident(t *testing.T, db *database.DB, i *Incident) *Incident {
	reloaded := &Incident{}
	stmt := db.Rebind(db.BuildSelectStmt(reloaded, reloaded) + ` WHERE "id" = ?`)
	require.NoError(t, db.GetContext(t.Context(), reloaded, stmt, i.Id))
	reloaded.initializeFields(db, i.runtimeConfig, i.logger)
	return reloaded
}

// escalationRow represents the rule_escalation table, including fields excluded in rule.Escalation.
type escalationRow struct {
	RuleID    int64           `db:"rule_id"`
	Position  int64           `db:"position"`
	Condition string          `db:"condition"`
	ChangedAt types.UnixMilli `db:"changed_at"`
	Deleted   types.Bool      `db:"deleted"`
}

// TableName implements the contracts.TableNamer interface.
func (escalationRow) TableName() string {
	return "rule_escalation"
}

// mustIncidentObject returns the object.Object of the given incident, or fails the test.
func mustIncidentObject(t *testing.T, i *Incident) *object.Object {
	obj, err := i.Object(t.Context())
	require.NoError(t, err)
	return obj
}

// makeEvent returns a fully initialized event based on the given parameters.
func makeEvent(t *testing.T, sourceID int64, opts ...eventOption) *event.Event {
	ev := &event.Event{
		Time:     time.Now().Add(-2 * time.Hour).Truncate(time.Second),
		SourceId: sourceID,
		Event:    baseEv.Event{Name: testutils.MakeRandomString(t)},
	}
	for _, opt := range opts {
		opt(ev)
	}
	if ev.Tags == nil {
		ev.Tags = map[string]string{ // Always generate unique object tags not to produce same object ID!
			"host":    testutils.MakeRandomString(t),
			"service": testutils.MakeRandomString(t),
		}
	}

	if ev.Muted.Valid {
		ev.MutedReason = "You're gonna have a bad time!"
	}
	require.NoError(t, ev.Validate(), "failed to validate event")
	return ev
}

// eventOption is a functional option type for modifying an event.
type eventOption func(*event.Event)

func withIncident() eventOption                   { return func(ev *event.Event) { ev.Incident = types.MakeBool(true) } }
func withClose() eventOption                      { return func(ev *event.Event) { ev.Close = types.MakeBool(true) } }
func withMuted(v bool) eventOption                { return func(ev *event.Event) { ev.Muted = types.MakeBool(v) } }
func withTags(tags map[string]string) eventOption { return func(ev *event.Event) { ev.Tags = tags } }
func withMsg(msg string) eventOption              { return func(ev *event.Event) { ev.Message = msg } }
func withSeverity(sev baseEv.Severity) eventOption {
	return func(ev *event.Event) { ev.Severity = sev }
}
