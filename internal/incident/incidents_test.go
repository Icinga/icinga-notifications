package incident

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/icinga/icinga-go-library/database"
	"github.com/icinga/icinga-go-library/logging"
	baseEv "github.com/icinga/icinga-go-library/notifications/event"
	"github.com/icinga/icinga-go-library/types"
	"github.com/icinga/icinga-notifications/internal/channel"
	"github.com/icinga/icinga-notifications/internal/config"
	"github.com/icinga/icinga-notifications/internal/daemon"
	"github.com/icinga/icinga-notifications/internal/event"
	"github.com/icinga/icinga-notifications/internal/object"
	"github.com/icinga/icinga-notifications/internal/recipient"
	"github.com/icinga/icinga-notifications/internal/rule"
	"github.com/icinga/icinga-notifications/internal/testutils"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIncidents(t *testing.T) {
	t.Parallel()

	// This will either load the test config from env vars or skip the test if the required env variable is not set.
	testutils.SkipTestIfDBConfigIsMissing(t)
	daemon.InjectTestConfig(func(configFile *daemon.ConfigFile) { testutils.LoadTestConfig(t, configFile) })

	db := testutils.GetTestDB(t.Context(), t, &daemon.Config().Database)
	logs := testutils.GetTestLogging(t)

	cleaner := testutils.NewDBCleaner(
		"incident_history",
		"incident_rule_escalation_state",
		"incident_rule",
		"incident_contact",
		"incident",
		"skipped_notification_history",
		"notification_history",
		"object_id_tag",
		"object_source",
		"object",
		"rule_escalation_recipient",
		"rule_escalation",
		"rule",
		"contact",
		"channel",
		"source",
	)

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cleaner.Clean(ctx, t, db)
	})

	// Insert a dummy source for our test cases!
	source := &config.Source{
		Type:             "notifications",
		Name:             "Icinga Notifications",
		ListenerUsername: types.MakeString("notifications"),
		ChangedAt:        types.UnixMilli(time.Date(2009, time.November, 10, 23, 0, 0, 0, time.UTC)),
		Deleted:          types.MakeBool(false),
	}
	id, err := database.InsertObtainID(t.Context(), db, database.BuildInsertStmtWithout(db, source, "id"), source)
	require.NoError(t, err, "populating source table should not fail")
	source.ID = id

	objectIDInQuery := fmt.Sprintf("object_id IN (SELECT object_id FROM object_source WHERE source_id = %d)", source.ID)
	byIncidentIDSubquery := fmt.Sprintf("incident_id IN (SELECT id FROM incident WHERE %s)", objectIDInQuery)

	cleaner.Add("source", fmt.Sprintf("id = %d", source.ID))
	cleaner.Add("object_source", fmt.Sprintf("source_id = %d", source.ID))
	cleaner.Add("object_id_tag", objectIDInQuery)
	cleaner.Add("incident", objectIDInQuery)
	cleaner.Add("notification_history", objectIDInQuery)
	cleaner.Add("skipped_notification_history", fmt.Sprintf("notification_history_id IN (SELECT id FROM notification_history WHERE %s)", objectIDInQuery))
	cleaner.Add("incident_contact", byIncidentIDSubquery)
	cleaner.Add("incident_rule", byIncidentIDSubquery)
	cleaner.Add("incident_rule_escalation_state", byIncidentIDSubquery)
	cleaner.Add("incident_history", byIncidentIDSubquery)
	// The object table is a bit special, as we don't track all the created objects by this test suite, we are only
	// allowed to clean it up filtered by the source_id in the object_source table. However, since the object_source
	// table is cleaned up first, we can't just use a simple subquery here, as it would always yield an empty result
	// set. Instead, we let the cleaner lazily produce the condition we want to use when it actually runs the cleanup,
	// so that we can query the object_source table before it gets cleaned up.
	cleaner.AddLazy("object", func(ctx context.Context) string {
		var ids []types.Binary
		require.NoError(t, db.SelectContext(ctx, &ids, db.Rebind("SELECT object_id FROM object_source WHERE source_id = ?"), source.ID))
		if len(ids) == 0 {
			return ""
		}
		return fmt.Sprintf("id IN (%s)", func() string {
			var b strings.Builder
			for i, id := range ids {
				if i > 0 {
					b.WriteRune(',')
				}
				// PostgreSQL doesn't like comparing hex values to bytea columns, so convert the hex back to bytea.
				if db.DriverName() == database.PostgreSQL {
					b.WriteString("DECODE('")
					b.WriteString(id.String())
					b.WriteString("', 'HEX')")
				} else {
					b.WriteString("UNHEX('")
					b.WriteString(id.String())
					b.WriteString("')")
				}
			}
			return b.String()
		}())
	})

	channel.UpsertPlugins(t.Context(), daemon.Config().ChannelsDir, logs.GetChildLogger("channel"), db)
	ch := makeTestChannel(t, db, cleaner, "notification_history_channel ", "sleep", `{"success": true}`)

	contact := makeContact(t, db, cleaner, "testuser", "testuser", ch.ID)
	incidentManager := makeContact(t, db, cleaner, "Thomas A. Anderson", "neo", ch.ID)
	incidentSubscriber := makeContact(t, db, cleaner, "Agent Smith", "smith", ch.ID)

	var unmanagedEscalationID, managedEscalationID int64
	var basicRule, managedRule *rule.Rule
	err = db.ExecTx(t.Context(), nil, func(ctx context.Context, tx *sqlx.Tx) error {
		insertRule := func(name, filter string) *rule.Rule {
			r := &rule.Rule{
				Name:             name,
				SourceType:       source.Type,
				ObjectFilterExpr: types.MakeString(filter),
				ChangedAt:        source.ChangedAt,
				Deleted:          source.Deleted,
			}
			id, err := database.InsertObtainID(ctx, tx, database.BuildInsertStmtWithout(db, r, "id"), r)
			assert.NoError(t, err)
			r.ID = id
			return r
		}
		basicRule = insertRule("Escalation Test Rule", `{"ast":{"op":"!=","attributes":["$.host.name"],"value":"managed_escalations"}}`)
		managedRule = insertRule("Managed Test Rule", `{"ast":{"op":"=","attributes":["$.host.name"],"value":"managed_escalations"}}`)

		insertEscalation := func(position, ruleID int64, condition string) int64 {
			escalation := &rule.Escalation{
				RuleID:        ruleID,
				Position:      types.MakeInt(position),
				ConditionExpr: types.MakeString(condition),
				ChangedAt:     types.UnixMilli(time.Now()),
				Deleted:       types.MakeBool(false),
			}
			id, err := database.InsertObtainID(ctx, tx, database.BuildInsertStmtWithout(db, escalation, "id"), escalation)
			require.NoError(t, err, "populating rule_escalation table should not fail")

			escalationRecipient := &rule.EscalationRecipient{
				EscalationID: id,
				Recipient:    contact,
				ContactID:    types.MakeInt(contact.ID),
				ChangedAt:    types.UnixMilli(time.Now()),
				Deleted:      types.MakeBool(false),
			}
			_, err = tx.NamedExecContext(ctx, database.BuildInsertStmtWithout(db, escalationRecipient, "id"), escalationRecipient)
			require.NoError(t, err, "populating rule_escalation_recipient table should not fail")

			return id
		}

		insertEscalation(2, basicRule.ID, "incident_severity>=ok")
		insertEscalation(1, basicRule.ID, "incident_age>=1h")

		unmanagedEscalationID = insertEscalation(1, managedRule.ID, "is_managed=n")
		managedEscalationID = insertEscalation(2, managedRule.ID, "is_managed=y")

		return nil
	})
	require.NoError(t, err)
	for _, ruleID := range []int64{basicRule.ID, managedRule.ID} {
		cleaner.Add("rule", fmt.Sprintf("id = %d", ruleID))
		cleaner.Add("rule_escalation", fmt.Sprintf("rule_id = %d", ruleID))
		cleaner.Add("rule_escalation_recipient", fmt.Sprintf("rule_escalation_id IN (SELECT id FROM rule_escalation WHERE rule_id = %d)", ruleID))
	}

	runtimeConfig := config.NewRuntimeConfig(logs, db)
	require.NoError(t, runtimeConfig.UpdateFromDatabase(t.Context()))

	require.NotNil(t, runtimeConfig.Rules[basicRule.ID])
	require.NotNil(t, runtimeConfig.Rules[managedRule.ID])
	require.Len(t, slices.Collect(runtimeConfig.Sources[source.ID].RuleIDs()), 2)

	t.Run("YieldIncidents", func(t *testing.T) {
		testData := make(map[string]*Incident, 64)
		for range 64 {
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
				if pair.Incident.Id%4 == 0 { // 64 / 4 => 16 existing incidents will be marked as recovered!
					require.NoError(t, Process(t.Context(), db, logs, runtimeConfig, makeEvent(t, source.ID,
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

			for j := range 16 {
				require.NoError(t, Process(t.Context(), db, logs, runtimeConfig, makeEvent(t, source.ID,
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
				require.NoError(t, Process(t.Context(), db, logs, runtimeConfig, makeEvent(t, source.ID,
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
			clear(testData)
			testData = nil
		})
	})

	t.Run("Severity Change", func(t *testing.T) {
		t.Parallel()

		i := makeIncident(db, logs, runtimeConfig, t, makeEvent(t, source.ID, withIncident(), withSeverity(baseEv.SeverityDebug)))
		assert.NotZero(t, i.Id)
		assert.Zero(t, i.RecoveredAt)
		assert.Equal(t, baseEv.SeverityDebug, i.Severity)

		require.NoError(t, Process(t.Context(), db, logs, runtimeConfig, makeEvent(t, source.ID,
			withIncident(), withSeverity(baseEv.SeverityEmerg), withTags(mustIncidentObject(t, i).Tags))))
		i = reloadIncident(t, db, i)
		assert.Equal(t, baseEv.SeverityEmerg, i.Severity)

		err := Process(t.Context(), db, logs, runtimeConfig, makeEvent(t, source.ID,
			withMuted(false), withSeverity(baseEv.SeverityNotice), withTags(mustIncidentObject(t, i).Tags)))
		require.ErrorIs(t, err, ErrSeverityChangeWithoutIncidentFlag)
		i = reloadIncident(t, db, i)
		assert.Equal(t, baseEv.SeverityEmerg, i.Severity)
	})

	t.Run("Incident Open", func(t *testing.T) {
		t.Parallel()

		i := makeIncident(db, logs, runtimeConfig, t, makeEvent(t, source.ID, withIncident(), withSeverity(baseEv.SeverityDebug)))
		assert.NotZero(t, i.Id)
		assert.Zero(t, i.RecoveredAt)
		assert.Equal(t, baseEv.SeverityDebug, i.Severity)

		// Attempting to open an incident without a severity should fail.
		err := Process(t.Context(), db, logs, runtimeConfig, makeEvent(t, source.ID, withIncident()))
		require.ErrorIs(t, err, ErrOpenIncidentWithoutSeverity)

		i = makeIncident(db, logs, runtimeConfig, t, makeEvent(t, source.ID,
			withIncident(), withSeverity(baseEv.SeverityEmerg), withMsg("Incident opened!")))
		assert.NotZero(t, i.Id)
		assert.Equal(t, baseEv.SeverityEmerg, i.Severity)
		assert.Equal(t, "Incident opened!", i.Message.String)

		require.NoError(t, Process(t.Context(), db, logs, runtimeConfig, makeEvent(t, source.ID,
			withIncident(),
			withSeverity(baseEv.SeverityEmerg),
			withMsg("Incident updated!"),
			withTags(mustIncidentObject(t, i).Tags))))
		i = reloadIncident(t, db, i)
		assert.Equal(t, baseEv.SeverityEmerg, i.Severity)
		assert.Equal(t, "Incident updated!", i.Message.String)

		// We shouldn't be able to update the incident message without the incident flag set.
		require.NoError(t, Process(t.Context(), db, logs, runtimeConfig,
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
		require.NoError(t, Process(t.Context(), db, logs, runtimeConfig, makeEvent(t, source.ID,
			withIncident(), withClose(), withSeverity(baseEv.SeverityEmerg), withTags(mustIncidentObject(t, i).Tags))))
		i = reloadIncident(t, db, i)
		assert.NotZero(t, i.RecoveredAt)
		assert.Equal(t, baseEv.SeverityEmerg, i.Severity)

		i = makeIncident(db, logs, runtimeConfig, t, makeEvent(t, source.ID, withIncident(), withSeverity(baseEv.SeverityWarning)))
		assert.Zero(t, i.RecoveredAt)
		assert.Equal(t, baseEv.SeverityWarning, i.Severity)

		// Closing incident without providing a severity will keep the existing severity and mark it as recovered.
		require.NoError(t, Process(t.Context(), db, logs, runtimeConfig, makeEvent(t, source.ID,
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
		require.NoError(t, Process(t.Context(), db, logs, runtimeConfig, makeEvent(t, source.ID,
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
		require.NoError(t, Process(t.Context(), db, logs, runtimeConfig, makeEvent(t, source.ID,
			withMuted(false), withTags(mustIncidentObject(t, i).Tags))))
		i = reloadIncident(t, db, i)
		assert.Equal(t, baseEv.SeverityDebug, i.Severity)
		assert.False(t, i.IsMuted())
		assert.Equal(t, "", i.MuteReason.String)

		// Muted flag without the incident flag has no effect on non-existing incidents.
		i = makeIncident(db, logs, runtimeConfig, t, makeEvent(t, source.ID, withMuted(true)))
		require.Nil(t, i)
	})

	t.Run("QuickAction", func(t *testing.T) {
		t.Parallel()

		ev := makeEvent(t, source.ID, withIncident(), withSeverity(baseEv.SeverityWarning), withMsg("Something went wrong!"))
		i := makeIncident(db, logs, runtimeConfig, t, ev)

		for _, action := range []event.Action{event.ActionSubscribe, event.ActionManage} {
			unknown := makeContact(t, db, cleaner, "Unknown", "unknown"+action.String(), ch.ID)
			qa := &event.QuickAction{
				ID:         types.MakeUUID(uuid.New()),
				Time:       time.Now(),
				Kind:       action,
				ContactID:  unknown.ID,
				ObjectTags: ev.Tags,
			}

			// Recipient is not yet known to the runtime config, so nothing should happen here.
			require.NoError(t, Process(t.Context(), db, logs, runtimeConfig, qa))
			i = reloadIncidentRecursive(t, db, i)
			assert.Len(t, i.Recipients, 1)
			assert.Equal(t, i.Recipients[recipient.ToKey(contact)].Role, recipient.RoleRecipient)
			assert.Equal(t, i.Recipients[recipient.ToKey(incidentManager)].Role, recipient.RoleNone)
			assert.Equal(t, i.Recipients[recipient.ToKey(incidentSubscriber)].Role, recipient.RoleNone)

			if action == event.ActionManage {
				qa.ContactID = incidentManager.ID
			} else {
				qa.ContactID = incidentSubscriber.ID
			}

			require.NoError(t, Process(t.Context(), db, logs, runtimeConfig, qa))
			i = reloadIncidentRecursive(t, db, i)
			assert.Len(t, i.Recipients, 2)
			if action == event.ActionManage {
				assert.True(t, i.HasManager())
				assert.Equal(t, i.Recipients[recipient.ToKey(incidentManager)].Role, recipient.RoleManager)

				qa.Kind = event.ActionUnmanage
			} else {
				assert.False(t, i.HasManager())
				assert.Equal(t, i.Recipients[recipient.ToKey(incidentSubscriber)].Role, recipient.RoleSubscriber)

				qa.Kind = event.ActionUnsubscribe
			}

			// Now, unsubscribe/unmanage the recipient from that very same incident.
			require.NoError(t, Process(t.Context(), db, logs, runtimeConfig, qa))
			i = reloadIncidentRecursive(t, db, i)
			if action == event.ActionManage { // Managers get first demoted to subscribers.
				assert.False(t, i.HasManager())
				assert.Equal(t, i.Recipients[recipient.ToKey(incidentManager)].Role, recipient.RoleSubscriber)

				// Cannot unmanage an incident that doesn't have a manager.
				require.Error(t, Process(t.Context(), db, logs, runtimeConfig, qa))
				assert.Equal(t, i.Recipients[recipient.ToKey(incidentManager)].Role, recipient.RoleSubscriber)

				qa.Kind = event.ActionUnsubscribe
				require.NoError(t, Process(t.Context(), db, logs, runtimeConfig, qa))
				i = reloadIncidentRecursive(t, db, i)
			}
			assert.Len(t, i.Recipients, 1)

			// Unsubscribing an already unsubscribed contact makes no sense, so it should fail.
			require.Error(t, Process(t.Context(), db, logs, runtimeConfig, qa))
			assert.Len(t, i.Recipients, 1)
			i = reloadIncidentRecursive(t, db, i)
		}
	})

	t.Run("Time-Based Escalation", func(t *testing.T) {
		t.Parallel()

		i := makeIncident(db, logs, runtimeConfig, t,
			makeEvent(t, source.ID, withIncident(), withSeverity(baseEv.SeverityCrit)))
		assert.NotNil(t, i)
		assert.NotZero(t, i.NextEscalationCheckAt)
		assert.WithinDuration(t, i.StartedAt.Time().Add(time.Hour), i.NextEscalationCheckAt.Time(), time.Second)

		i = reloadIncidentRecursive(t, db, i)
		// We will find a single escalation state for the incident, because the condition of the escalation defined
		// outside this function is met immediately.
		assert.Len(t, i.EscalationState, 1)
		assert.Len(t, i.Rules, 1)

		assert.NoError(t, ReevaluateEscalations(t.Context(), db, logs.GetChildLogger("incident"), runtimeConfig))
		i = reloadIncidentRecursive(t, db, i)

		// After reevaluating the escalations, we should find two escalation states for the incident,
		// because the second escalation's condition is now met.
		assert.Len(t, i.EscalationState, 2)
		i = reloadIncident(t, db, i)
		assert.Zero(t, i.NextEscalationCheckAt)
	})

	t.Run("Managed Escalation", func(t *testing.T) {
		t.Parallel()

		relations := map[string]any{"host": map[string]string{"name": "managed_escalations"}}
		ev := makeEvent(t, source.ID, withIncident(), withSeverity(baseEv.SeverityCrit), withRelations(relations))
		i := reloadIncidentRecursive(t, db, makeIncident(db, logs, runtimeConfig, t, ev))
		require.NotNil(t, i)

		// Nobody has taken over the responsibility for the incident yet, so only the first escalation is triggered.
		require.Len(t, i.Rules, 1)
		_, exists := i.Rules[managedRule.ID]
		require.True(t, exists)
		require.Len(t, i.EscalationState, 1)
		assert.NotNil(t, i.EscalationState[unmanagedEscalationID])
		assert.False(t, i.HasManager())

		// As long as the incident remains unmanaged, the second escalation isn't reached.
		require.NoError(t, Process(t.Context(), db, logs, runtimeConfig, ev))
		i = reloadIncidentRecursive(t, db, i)
		require.Len(t, i.EscalationState, 1)
		assert.NotNil(t, i.EscalationState[unmanagedEscalationID])
		assert.False(t, i.HasManager())

		// Let someone take over the responsibility for the incident.
		require.NoError(t, Process(t.Context(), db, logs, runtimeConfig, &event.QuickAction{
			ID:         types.MakeUUID(uuid.New()),
			Time:       time.Now(),
			Kind:       event.ActionManage,
			ContactID:  incidentManager.ID,
			ObjectTags: ev.Tags,
		}))

		// The next event evaluates the escalations again, this time on a managed incident.
		require.NoError(t, Process(t.Context(), db, logs, runtimeConfig, ev))

		i = reloadIncidentRecursive(t, db, i)
		require.Len(t, i.EscalationState, 2)
		assert.NotNil(t, i.EscalationState[unmanagedEscalationID])
		assert.NotNil(t, i.EscalationState[managedEscalationID])
		assert.True(t, i.HasManager())
	})

	t.Run("Notification History", func(t *testing.T) {
		t.Parallel()

		tags := map[string]string{"notification_history_test": "true"}
		msg := testutils.MakeRandomString(t)
		ev := makeEvent(t, source.ID, withIncident(), withSeverity(baseEv.SeverityDebug), withTags(tags), withMsg(msg))
		// Otherwise, it will race with the time-based escalation test, which calls ReevaluateEscalations and causes
		// the escalation to match on this incident as well, which would cause the notification history to contain
		// two entries instead of one.
		ev.Time = time.Now()
		assert.NotZero(t, ev.ID)
		i := makeIncident(db, logs, runtimeConfig, t, ev)
		assert.NotZero(t, i.Id)
		assert.Zero(t, i.RecoveredAt)
		assert.Equal(t, baseEv.SeverityDebug, i.Severity)

		t.Run("Fetch Everything", func(t *testing.T) {
			t.Parallel()

			entryCh, errCh := YieldNotificationHistory(t.Context(), db, 1)

			count := 0
			for entry := range entryCh {
				if entry.EventID != ev.ID {
					continue
				}
				count++
				assert.Equal(t, i.ObjectID, entry.ObjectID)
				assert.Equal(t, tags, entry.Object.Tags)
				assert.Equal(t, types.MakeString(ch.Name), entry.ChannelName)
				assert.Equal(t, types.MakeString(contact.FullName), entry.ContactName)
				assert.Equal(t, types.MakeString(msg), entry.EventMessage)
				assert.False(t, entry.ContactgroupName.Valid, "contactgroup_name must be an empty string, not null, when there's no contactgroup")
				assert.False(t, entry.ScheduleName.Valid, "schedule_name must be an empty string, not null, when there's no schedule")
			}
			require.NoError(t, <-errCh)
			assert.Equal(t, 1, count, "there must be at least one notification history entry")
		})

		t.Run("Filtered By Since", func(t *testing.T) {
			t.Parallel()

			entryCh, errCh := YieldNotificationHistory(t.Context(), db, 9999999999999)

			count := 0
			for range entryCh {
				count++
			}
			require.NoError(t, <-errCh)
			assert.Equal(t, count, 0, "there must be no notification history entries since a future timestamp")
		})

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

// makeIncident creates a new incident by processing the given event and returns the resulting incident object.
//
// The incident is guaranteed to be fully initialized and ready for assertions but might be nil if it's immediately closed.
func makeIncident(db *database.DB, logs *logging.Logging, runtimeConfig *config.RuntimeConfig, t *testing.T, ev *event.Event) *Incident {
	require.NoError(t, Process(t.Context(), db, logs, runtimeConfig, ev))
	i := new(Incident)
	i.ObjectID = object.ID(ev.Tags)
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

// reloadIncidentRecursive reloads the given Incident from the database recursively and returns a new one.
func reloadIncidentRecursive(t *testing.T, db *database.DB, i *Incident) *Incident {
	reloaded := &Incident{ObjectID: i.ObjectID}
	reloaded.initializeFields(db, i.runtimeConfig, i.logger)
	err := db.ExecTx(t.Context(), nil, func(ctx context.Context, tx *sqlx.Tx) error {
		return reloaded.RestoreState(ctx, tx, true)
	})
	require.NoError(t, err)
	return reloaded
}

// mustIncidentObject returns the object.Object of the given incident, or fails the test.
func mustIncidentObject(t *testing.T, i *Incident) *object.Object {
	obj, err := i.Object(t.Context())
	require.NoError(t, err)
	return obj
}

// makeContact generates a fully initialized contact based on the provided args, sync it to the database and returns it.
func makeContact(t *testing.T, db *database.DB, cleaner *testutils.DBCleaner, fullName, username string, channelID int64) *recipient.Contact {
	contact := &recipient.Contact{
		FullName:         fullName,
		Username:         types.MakeString(username),
		DefaultChannelID: channelID,
		ExternalUUID:     types.MakeUUID(uuid.New()),
		Deleted:          types.MakeBool(false),
		ChangedAt:        types.UnixMilli(time.Now()),
	}
	contactID, err := database.InsertObtainID(t.Context(), db, database.BuildInsertStmtWithout(db, contact, "id"), contact)
	require.NoError(t, err)
	contact.ID = contactID
	cleaner.Add("contact", fmt.Sprintf("id = %d", contact.ID))
	return contact
}

// makeEvent returns a fully initialized event based on the given parameters.
func makeEvent(t *testing.T, sourceID int64, opts ...eventOption) *event.Event {
	ev := &event.Event{
		Time:     time.Now().Add(-2 * time.Hour).Truncate(time.Second),
		SourceId: sourceID,
		ID:       types.MakeUUID(uuid.New()),
		Name:     testutils.MakeRandomString(t),
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
	if ev.Relations == nil {
		ev.Relations = map[string]any{"host": map[string]string{"name": testutils.MakeRandomString(t)}}
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
func withRelations(relations map[string]any) eventOption {
	return func(ev *event.Event) { ev.Relations = relations }
}

// makeTestChannel creates a new Channel instance with the provided name, type, and config for testing purposes.
func makeTestChannel(t *testing.T, db *database.DB, cleaner *testutils.DBCleaner, name, ctype, config string) *channel.Channel {
	ch := &channel.Channel{
		Name:         name,
		Type:         ctype,
		Config:       config,
		ExternalUUID: types.MakeUUID(uuid.New()),
		ChangedAt:    types.UnixMilli(time.Now()),
		Deleted:      types.MakeBool(false),
	}
	id, err := database.InsertObtainID(t.Context(), db, database.BuildInsertStmtWithout(db, ch, "id"), ch)
	require.NoError(t, err)
	ch.ID = id
	cleaner.Add("channel", fmt.Sprintf("id = %d", ch.ID))
	return ch
}
