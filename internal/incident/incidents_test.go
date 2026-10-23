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
		"rule_entry_recipient",
		"rule_entry",
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
	ch := makeTestChannel(t, db, cleaner, "notification_history_channel ", "sleep", `{"duration":"1ns","success": true}`)
	ch2 := makeTestChannel(t, db, cleaner, "notification_history_channel_2 ", "sleep", `{"duration":"1ns","success": true}`)

	contact := makeContact(t, db, cleaner, "testuser", "testuser", ch.ID)
	_ = makeContact(t, db, cleaner, "testuser_not_notifiable", "testuser_not_notifiable", ch.ID)
	incidentManager := makeContact(t, db, cleaner, "Thomas A. Anderson", "neo", ch.ID)
	incidentSubscriber := makeContact(t, db, cleaner, "Agent Smith", "smith", ch.ID)
	notificationContact := makeContact(t, db, cleaner, "notification_testuser", "notification_testuser", ch.ID)
	notificationContact2 := makeContact(t, db, cleaner, "notification_testuser_2", "notification_testuser_2", ch2.ID)
	passiveNotificationContact := makeContact(t, db, cleaner, "passive_notification_testuser", "passive_notification_testuser", ch.ID)
	bypassContact := makeContact(t, db, cleaner, "suppression_bypass_testuser", "suppression_bypass_testuser", ch.ID)
	knownContact := makeContact(t, db, cleaner, "suppression_known_testuser", "suppression_known_testuser", ch.ID)
	knownSubscriber := makeContact(t, db, cleaner, "suppression_known_subscriber", "suppression_known_subscriber", ch.ID)
	freshContact := makeContact(t, db, cleaner, "suppression_fresh_testuser", "suppression_fresh_testuser", ch.ID)
	escalatedContact := makeContact(t, db, cleaner, "suppression_escalated_testuser", "suppression_escalated_testuser", ch.ID)

	var unmanagedEscalationID, managedEscalationID int64
	var basicRule, managedRule, notificationRule, triggerNotificationsRule, escalationsPassivelyNotifiedRule,
		suppressionBypassRule, suppressionBypassKnownRule *rule.Rule
	err = db.ExecTx(t.Context(), nil, func(ctx context.Context, tx *sqlx.Tx) error {
		insertRule := func(name, filter string, ruleType rule.Type) *rule.Rule {
			r := &rule.Rule{
				Name:             name,
				SourceType:       source.Type,
				ObjectFilterExpr: types.MakeString(filter, types.TransformEmptyStringToNull),
				ChangedAt:        source.ChangedAt,
				Deleted:          source.Deleted,
				Type:             ruleType,
			}
			id, err := database.InsertObtainID(ctx, tx, database.BuildInsertStmtWithout(db, r, "id"), r)
			assert.NoError(t, err)
			r.ID = id
			return r
		}
		basicRule = insertRule("Escalation Test Rule", `
{
	"ast":{
		"op":"&",
		"rules":[
			{"op":"!=","attributes":["$.host.name"],"value":"managed_escalations"},
			{"op":"!=","attributes":["$.host.name"],"regex":"trigger_notifications*"},
			{"op":"!=","attributes":["$.host.name"],"value":"notification_rule"},
			{"op":"!=","attributes":["$.host.name"],"regex":"passive_notifications*"},
			{"op":"!=","attributes":["$.host.name"],"regex":"suppression_bypass*"}
		]
	}
}`, rule.TypeEscalation)
		managedRule = insertRule("Managed Test Rule", `{"ast":{"op":"=","attributes":["$.host.name"],"value":"managed_escalations"}}`, rule.TypeEscalation)
		triggerNotificationsRule = insertRule("Trigger Notifications Test Rule", `{"ast":{"op":"=","attributes":["$.host.name"],"regex":"trigger_notifications*"}}`, rule.TypeEscalation)
		escalationsPassivelyNotifiedRule = insertRule("Passively Notify Escalation Recipients Rule", `{"ast":{"op":"=","attributes":["$.host.name"],"regex":"passive_notifications*"}}`, rule.TypeEscalation)
		notificationRule = insertRule("Notification Test Rule", `{"ast":{"op":"=","attributes":["$.host.name"],"regex":"notifications_rule*"}}`, rule.TypeNotification)
		suppressionBypassRule = insertRule("Suppression Bypass Rule", `{"ast":{"op":"=","attributes":["$.host.name"],"value":"suppression_bypass"}}`, rule.TypeEscalation)
		suppressionBypassKnownRule = insertRule("Suppression Bypass Known Recipients Rule", `{"ast":{"op":"=","attributes":["$.host.name"],"value":"suppression_bypass_known_recipients"}}`, rule.TypeEscalation)

		insertEntryVia := func(c *recipient.Contact, channelID types.Int, position, ruleID int64, condition string) int64 {
			escalation := &rule.Entry{
				RuleID:        ruleID,
				Position:      types.MakeInt(position),
				ConditionExpr: types.MakeString(condition),
				ChangedAt:     types.UnixMilli(time.Now()),
				Deleted:       types.MakeBool(false),
			}
			id, err := database.InsertObtainID(ctx, tx, database.BuildInsertStmtWithout(db, escalation, "id"), escalation)
			require.NoError(t, err, "populating rule_entry table should not fail")

			if c == nil {
				c = makeContact(t, db, cleaner, testutils.MakeRandomString(t), testutils.MakeRandomString(t), ch.ID)
			}

			escalationRecipient := &rule.EntryRecipient{
				EntryID:   id,
				ChannelID: channelID,
				Recipient: c,
				ContactID: types.MakeInt(c.ID),
				ChangedAt: types.UnixMilli(time.Now()),
				Deleted:   types.MakeBool(false),
			}
			_, err = tx.NamedExecContext(ctx, database.BuildInsertStmtWithout(db, escalationRecipient, "id"), escalationRecipient)
			require.NoError(t, err, "populating rule_escalation_recipient table should not fail")

			return id
		}
		// insertEntry inserts an entry whose recipient is notified via the contact's default channel.
		insertEntry := func(c *recipient.Contact, position, ruleID int64, condition string) int64 {
			return insertEntryVia(c, types.Int{}, position, ruleID, condition)
		}

		insertEntry(contact, 2, basicRule.ID, `{"ast":{"op":">=","attributes":["incident_severity"],"value":"ok"}}`)
		insertEntry(contact, 1, basicRule.ID, `{"ast":{"op":">=","attributes":["incident_age"],"value":"1h"}}`)

		unmanagedEscalationID = insertEntry(contact, 1, managedRule.ID, `{"ast":{"op":"=","attributes":["is_managed"],"value":"n"}}`)
		managedEscalationID = insertEntry(contact, 2, managedRule.ID, `{"ast":{"op":"=","attributes":["is_managed"],"value":"y"}}`)

		insertEntry(nil, 1, triggerNotificationsRule.ID, `{"ast":{"op":">=","attributes":["incident_severity"],"value":"warning"}}`)
		insertEntry(nil, 2, triggerNotificationsRule.ID, `{"ast":{"op":">=","attributes":["incident_severity"],"value":"error"}}`)

		insertEntry(notificationContact, 1, notificationRule.ID, `{"ast":{"op":"=","attributes":["event_type"],"value":"test-type"}}`)
		insertEntry(notificationContact2, 2, notificationRule.ID, `{"ast":{"op":"=","attributes":["event_type"],"value":"test-type"}}`)
		insertEntry(passiveNotificationContact, 3, notificationRule.ID, `{"ast":{"op":"=","attributes":["event_type"],"value":"test-type"}}`)
		insertEntry(passiveNotificationContact, 1, escalationsPassivelyNotifiedRule.ID, `{"ast":{"op":">=","attributes":["incident_severity"],"value":"ok"}}`)

		// See the "Suppression Bypass" test case that makes use of these entries.
		insertEntry(bypassContact, 1, suppressionBypassRule.ID, `{"ast":{"op":">=","attributes":["incident_age"],"value":"30m"}}`)
		insertEntry(bypassContact, 2, suppressionBypassRule.ID, `{"ast":{"op":">=","attributes":["incident_age"],"value":"1h"}}`)
		insertEntryVia(bypassContact, types.MakeInt(ch2.ID), 3, suppressionBypassRule.ID, `{"ast":{"op":">=","attributes":["incident_age"],"value":"1h"}}`)

		// These entries are used in the ""Suppression Bypass Known Recipients" test case.
		insertEntry(knownContact, 1, suppressionBypassKnownRule.ID, `{"ast":{"op":">=","attributes":["incident_severity"],"value":"error"}}`)
		insertEntry(knownContact, 2, suppressionBypassKnownRule.ID, `{"ast":{"op":">=","attributes":["incident_age"],"value":"30m"}}`)
		insertEntry(knownSubscriber, 3, suppressionBypassKnownRule.ID, `{"ast":{"op":">=","attributes":["incident_age"],"value":"30m"}}`)
		insertEntry(freshContact, 4, suppressionBypassKnownRule.ID, `{"ast":{"op":">=","attributes":["incident_age"],"value":"30m"}}`)
		insertEntry(escalatedContact, 5, suppressionBypassKnownRule.ID, `{"ast":{"op":">=","attributes":["incident_severity"],"value":"critical"}}`)

		return nil
	})
	require.NoError(t, err)
	for _, r := range []*rule.Rule{
		basicRule,
		managedRule,
		notificationRule,
		triggerNotificationsRule,
		escalationsPassivelyNotifiedRule,
		suppressionBypassRule,
		suppressionBypassKnownRule,
	} {
		cleaner.Add("rule", fmt.Sprintf("id = %d", r.ID))
		cleaner.Add("rule_entry", fmt.Sprintf("rule_id = %d", r.ID))
		cleaner.Add("rule_entry_recipient", fmt.Sprintf("rule_entry_id IN (SELECT id FROM rule_entry WHERE rule_id = %d)", r.ID))
	}

	runtimeConfig := config.NewRuntimeConfig(logs, db)
	require.NoError(t, runtimeConfig.UpdateFromDatabase(t.Context()))

	require.NotNil(t, runtimeConfig.Rules[basicRule.ID])
	require.NotNil(t, runtimeConfig.Rules[managedRule.ID])
	require.NotNil(t, runtimeConfig.Rules[notificationRule.ID])
	require.NotNil(t, runtimeConfig.Rules[triggerNotificationsRule.ID])
	require.Len(t, slices.Collect(runtimeConfig.Sources[source.ID].RuleIDs()), 7)

	t.Run("YieldIncidents", func(t *testing.T) {
		t.Parallel()

		activeIncidents := make(map[string]*Incident, 48)
		recoveredIncidents := make(map[string]*Incident, 16)
		for n := range 64 {
			ev := makeEvent(t, source.ID, withIncident(true), withSeverity(baseEv.SeverityCritical), withTime(time.Now()))
			i := makeIncident(db, logs, runtimeConfig, t, ev)
			if n%4 == 0 { // 64 / 4 => 16 existing incidents will be marked as recovered!
				require.NoError(t, Process(t.Context(), db, logs, runtimeConfig, makeEvent(t, source.ID,
					withIncident(true), withClose(), withTags(ev.Tags), withTime(time.Now()))))
				require.NotZero(t, reloadIncident(t, db, i).RecoveredAt)
				recoveredIncidents[i.ObjectID.String()] = i
			} else {
				activeIncidents[i.ObjectID.String()] = i
			}
		}
		assert.Equal(t, 48, len(activeIncidents))
		assert.Equal(t, 16, len(recoveredIncidents))

		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()

		var incidentsLen int
		pairCh, errCh := Yield(ctx, db, logs, runtimeConfig)
		for pair := range pairCh {
			current := pair.Incident
			i := activeIncidents[current.ObjectID.String()]
			// If the incident is not in the active incidents map, then it must be from other parallel test runs,
			// thus it shouldn't be in the recovered incidents map either, otherwise Yield is behaving incorrectly.
			if i == nil {
				assert.Nil(t, recoveredIncidents[current.ObjectID.String()])
				continue
			}
			incidentsLen++

			assert.Equal(t, i.Id, current.Id)
			assert.Equal(t, i.Severity, current.Severity)
			assert.Equal(t, i.StartedAt, current.StartedAt)
			assert.Equal(t, i.RecoveredAt, current.RecoveredAt)

			assert.NotNil(t, current.EscalationState)
			assert.NotNil(t, current.Recipients)
			assert.NotNil(t, current.EscalationRules)
			assert.NotNil(t, mustIncidentObject(t, current), "failed to restore incident object")
			assert.Equal(t, mustIncidentObject(t, i), mustIncidentObject(t, current))
		}
		assert.NoError(t, <-errCh)
		assert.Equal(t, len(activeIncidents), incidentsLen)
	})

	t.Run("Severity Change", func(t *testing.T) {
		t.Parallel()

		relations := map[string]any{"host": map[string]string{"name": "trigger_notifications_severity_change"}}
		i := makeIncident(db, logs, runtimeConfig, t, makeEvent(t, source.ID,
			withTime(time.Now()), withRelations(relations), withIncident(true), withSeverity(baseEv.SeverityOK)))

		// No escalation should be triggered yet, because the incident severity is below the escalation condition.
		verifyIncident(t, db, i, 1, 0, 0, 0, baseEv.SeverityOK)

		require.NoError(t, Process(t.Context(), db, logs, runtimeConfig, makeEvent(t, source.ID,
			withTime(time.Now()), withRelations(relations), withIncident(true), withSeverity(baseEv.SeverityWarning),
			withTags(mustIncidentObject(t, i).Tags))))

		// Now, the first escalation with condition >=warning should have been triggered, and the recipient
		// should have been added to the incident.
		verifyIncident(t, db, i, 1, 1, 1, 1, baseEv.SeverityWarning)

		require.NoError(t, Process(t.Context(), db, logs, runtimeConfig, makeEvent(t, source.ID,
			withTime(time.Now()), withMuted(true), withRelations(relations), withIncident(true),
			withSeverity(baseEv.SeverityWarning), withTags(mustIncidentObject(t, i).Tags))))

		// Same severity, but muted, so the incident should still be muted and no new notification should have been sent.
		verifyIncident(t, db, i, 1, 1, 1, 1, baseEv.SeverityWarning)
		assert.True(t, reloadIncident(t, db, i).IsMuted())

		require.NoError(t, Process(t.Context(), db, logs, runtimeConfig, makeEvent(t, source.ID,
			withTime(time.Now()), withMuted(false), withRelations(relations), withIncident(true),
			withSeverity(baseEv.SeverityError), withTags(mustIncidentObject(t, i).Tags))))

		// Severity increased, so the incident should be unmuted and a new notification should have been sent.
		verifyIncident(t, db, i, 1, 2, 2, 3, baseEv.SeverityError)
		assert.False(t, reloadIncident(t, db, i).IsMuted())

		err := Process(t.Context(), db, logs, runtimeConfig, makeEventNoValidate(t, source.ID,
			withMuted(false), withSeverity(baseEv.SeverityWarning), withTags(mustIncidentObject(t, i).Tags)))
		require.ErrorIs(t, err, ErrSeverityChangeWithoutIncidentFlag)
		assert.Equal(t, baseEv.SeverityError, reloadIncident(t, db, i).Severity)
	})

	t.Run("Incident Open", func(t *testing.T) {
		t.Parallel()

		relations := map[string]any{"host": map[string]string{"name": "trigger_notifications_incident_open"}}

		i := makeIncident(db, logs, runtimeConfig, t, makeEvent(t, source.ID,
			withTime(time.Now()), withIncident(true), withRelations(relations), withSeverity(baseEv.SeverityOK)))
		verifyIncident(t, db, i, 1, 0, 0, 0, baseEv.SeverityOK)

		// Attempting to open an incident without a severity should fail.
		ev := makeEvent(t, source.ID, withIncident(true))
		ev.Severity = baseEv.SeverityNone
		require.ErrorIs(t, Process(t.Context(), db, logs, runtimeConfig, ev), ErrOpenIncidentWithoutSeverity)

		i = makeIncident(db, logs, runtimeConfig, t, makeEvent(t, source.ID,
			withTime(time.Now()), withIncident(true), withRelations(relations), withSeverity(baseEv.SeverityCritical), withMsg("Incident opened!")))
		verifyIncident(t, db, i, 1, 2, 2, 2, baseEv.SeverityCritical)
		assert.Equal(t, "Incident opened!", reloadIncident(t, db, i).Message.String)

		require.NoError(t, Process(t.Context(), db, logs, runtimeConfig, makeEvent(t, source.ID,
			withTime(time.Now()),
			withIncident(true),
			withRelations(relations),
			withSeverity(baseEv.SeverityCritical),
			withMsg("Incident updated!"),
			withTags(mustIncidentObject(t, i).Tags))))
		verifyIncident(t, db, i, 1, 2, 2, 2, baseEv.SeverityCritical) // Nothing changed.
		assert.Equal(t, "Incident updated!", reloadIncident(t, db, i).Message.String)

		// We shouldn't be able to update the incident message without the incident flag set.
		require.NoError(t, Process(t.Context(), db, logs, runtimeConfig,
			makeEvent(t, source.ID, withMuted(false), withMsg("YOLO!"), withTags(mustIncidentObject(t, i).Tags))))
		assert.Equal(t, "Incident updated!", reloadIncident(t, db, i).Message.String)
	})

	t.Run("Incident False Flag", func(t *testing.T) {
		t.Parallel()

		msg := "Non-State Notification Event: OK"
		relations := map[string]any{"host": map[string]string{"name": "notifications_rule"}}
		ev := makeEvent(t, source.ID, withIncident(false), withMsg(msg), withRelations(relations), withEventType("test-type"))
		require.NoError(t, Process(t.Context(), db, logs, runtimeConfig, ev))

		msgNotMatching := "Non-State Notification Event: Relations Not Matching!"
		relationsNotMatching := map[string]any{"host": map[string]string{"name": "not_matching_name"}}
		evNotMatching1 := makeEvent(t, source.ID, withIncident(false), withMsg(msgNotMatching), withRelations(relationsNotMatching), withEventType("test-type"))
		require.NoError(t, Process(t.Context(), db, logs, runtimeConfig, evNotMatching1))

		msgNotMatching = "Non-State Notification Event: Entry Rule Condition Not Matching!"
		relationsNotMatching = map[string]any{"host": map[string]string{"name": "notifications_rule"}}
		evNotMatching2 := makeEvent(t, source.ID, withIncident(false), withMsg(msgNotMatching), withRelations(relationsNotMatching), withEventType("not-matching-type"))
		require.NoError(t, Process(t.Context(), db, logs, runtimeConfig, evNotMatching2))

		entryCh, errCh := YieldNotificationHistory(t.Context(), db, 1)

		count := 0
		for entry := range entryCh {
			if entry.EventID != ev.ID && entry.EventID != evNotMatching1.ID && entry.EventID != evNotMatching2.ID {
				continue
			}
			count++
			assert.Equal(t, ev.Tags, entry.Object.Tags)

			switch entry.ContactName.String {
			case notificationContact.FullName:
				assert.Equal(t, ch.Name, entry.ChannelName)
			case notificationContact2.FullName:
				assert.Equal(t, ch2.Name, entry.ChannelName)
			case passiveNotificationContact.FullName:
				assert.Equal(t, ch.Name, entry.ChannelName)
			default:

			}
			assert.Equal(t, msg, entry.EventMessage)
			assert.False(t, entry.ContactgroupName.Valid, "contactgroup_name must be an empty string, not null, when there's no contactgroup")
			assert.False(t, entry.ScheduleName.Valid, "schedule_name must be an empty string, not null, when there's no schedule")
		}
		require.NoError(t, <-errCh)
		assert.Equal(t, 3, count, "the exact count of notification history entries must be matched")
	})

	t.Run("Close Flag", func(t *testing.T) {
		t.Parallel()

		relations := map[string]any{"host": map[string]string{"name": "trigger_notifications_incident_close"}}

		// Incident opened and closed immediately, so it's no longer active.
		require.Nil(t, makeIncident(db, logs, runtimeConfig, t, makeEvent(t, source.ID,
			withTime(time.Now()), withIncident(true), withClose(), withRelations(relations), withSeverity(baseEv.SeverityOK))))

		i := makeIncident(db, logs, runtimeConfig, t, makeEvent(t, source.ID,
			withIncident(true), withRelations(relations), withSeverity(baseEv.SeverityWarning)))
		verifyIncident(t, db, i, 1, 1, 1, 1, baseEv.SeverityWarning)

		// Closing incident with a new severity will update the severity and mark it as recovered.
		require.NoError(t, Process(t.Context(), db, logs, runtimeConfig, makeEvent(t, source.ID,
			withTime(time.Now()), withIncident(true), withClose(), withRelations(relations), withSeverity(baseEv.SeverityCritical),
			withTags(mustIncidentObject(t, i).Tags))))
		i = reloadIncident(t, db, i)
		assert.NotZero(t, i.RecoveredAt)
		assert.Equal(t, baseEv.SeverityCritical, i.Severity)

		i = makeIncident(db, logs, runtimeConfig, t, makeEvent(t, source.ID,
			withTime(time.Now()), withIncident(true), withRelations(relations), withSeverity(baseEv.SeverityError)))
		verifyIncident(t, db, i, 1, 2, 2, 2, baseEv.SeverityError)

		require.NoError(t, Process(t.Context(), db, logs, runtimeConfig, makeEvent(t, source.ID,
			withTime(time.Now()), withIncident(true), withClose(), withRelations(relations), withTags(mustIncidentObject(t, i).Tags))))
		i = reloadIncident(t, db, i)
		assert.NotZero(t, i.RecoveredAt)
		assert.Equal(t, baseEv.SeverityWarning, i.Severity)
	})

	t.Run("Notify Flag", func(t *testing.T) {
		t.Parallel()

		relations := map[string]any{"host": map[string]string{"name": "trigger_notifications_incident_notify"}}

		i := makeIncident(db, logs, runtimeConfig, t, makeEvent(t, source.ID,
			withTime(time.Now()), withIncident(true), withRelations(relations), withSeverity(baseEv.SeverityWarning)))
		verifyIncident(t, db, i, 1, 1, 1, 1, baseEv.SeverityWarning)

		require.NoError(t, Process(t.Context(), db, logs, runtimeConfig, makeEvent(t, source.ID,
			withTime(time.Now()), withIncident(true), withNotify(), withRelations(relations), withSeverity(baseEv.SeverityWarning),
			withTags(mustIncidentObject(t, i).Tags))))

		// No severity change, but the notify flag was set to true, so a new notification should have been sent.
		verifyIncident(t, db, i, 1, 1, 1, 2, baseEv.SeverityWarning)

		require.NoError(t, Process(t.Context(), db, logs, runtimeConfig, makeEvent(t, source.ID,
			withTime(time.Now()), withIncident(true), withRelations(relations), withSeverity(baseEv.SeverityCritical), withTags(mustIncidentObject(t, i).Tags))))

		// Severity increased, so two new notifications should have been sent.
		verifyIncident(t, db, i, 1, 2, 2, 4, baseEv.SeverityCritical)

		require.NoError(t, Process(t.Context(), db, logs, runtimeConfig, makeEvent(t, source.ID,
			withTime(time.Now()), withIncident(true), withNotify(), withRelations(relations), withSeverity(baseEv.SeverityCritical),
			withTags(mustIncidentObject(t, i).Tags))))

		// No severity change, but the notify flag was set to true, so a new notification should have been sent to both recipients.
		verifyIncident(t, db, i, 1, 2, 2, 6, baseEv.SeverityCritical)
	})

	t.Run("Muted Flag", func(t *testing.T) {
		t.Parallel()

		relations := map[string]any{"host": map[string]string{"name": "trigger_notifications_incident_muted"}}

		i := makeIncident(db, logs, runtimeConfig, t, makeEvent(t, source.ID,
			withTime(time.Now()), withIncident(true), withRelations(relations), withSeverity(baseEv.SeverityWarning), withMuted(true)))
		verifyIncident(t, db, i, 1, 1, 1, 1, baseEv.SeverityWarning)

		i = reloadIncident(t, db, i)
		assert.True(t, i.IsMuted())
		assert.Equal(t, "You're gonna have a bad time!", i.MuteReason.String)

		// Sending the muted flag again without the incident flag set should trigger new notifications.
		require.NoError(t, Process(t.Context(), db, logs, runtimeConfig, makeEvent(t, source.ID,
			withTime(time.Now()), withMuted(true), withRelations(relations), withTags(mustIncidentObject(t, i).Tags))))
		verifyIncident(t, db, i, 1, 1, 1, 2, baseEv.SeverityWarning)

		// Unmute it with the incident flag still set...
		require.NoError(t, Process(t.Context(), db, logs, runtimeConfig, makeEvent(t, source.ID,
			withTime(time.Now()), withIncident(true), withMuted(false), withRelations(relations), withTags(mustIncidentObject(t, i).Tags))))
		// Since we didn't change the severity, no new notifications should have been sent.
		verifyIncident(t, db, i, 1, 1, 1, 2, baseEv.SeverityWarning)
		assert.False(t, reloadIncident(t, db, i).IsMuted())

		// Re-sending the muted flag without the incident flag set should trigger new notifications again.
		require.NoError(t, Process(t.Context(), db, logs, runtimeConfig, makeEvent(t, source.ID,
			withTime(time.Now()), withMuted(false), withRelations(relations), withTags(mustIncidentObject(t, i).Tags))))
		verifyIncident(t, db, i, 1, 1, 1, 3, baseEv.SeverityWarning)

		require.NoError(t, Process(t.Context(), db, logs, runtimeConfig, makeEvent(t, source.ID,
			withTime(time.Now()), withMuted(false), withRelations(relations), withTags(mustIncidentObject(t, i).Tags))))
		verifyIncident(t, db, i, 1, 1, 1, 4, baseEv.SeverityWarning)

		// Muted flag without the incident flag has no effect on non-existing incidents.
		require.Nil(t, makeIncident(db, logs, runtimeConfig, t, makeEvent(t, source.ID, withMuted(true))))
	})

	t.Run("QuickAction", func(t *testing.T) {
		t.Parallel()

		ev := makeEvent(t, source.ID, withIncident(true), withSeverity(baseEv.SeverityError), withMsg("Something went wrong!"))
		i := makeIncident(db, logs, runtimeConfig, t, ev)

		// The first notification should have been sent to the default "contact" recipient of the escalation rule.
		assert.Equal(t, []NotificationState{NotificationStateSent}, notifiedStates(t, db, i, contact, ch.ID))

		for _, action := range []event.Action{event.ActionSubscribe, event.ActionManage} {
			unknown := makeContact(t, db, cleaner, "Unknown", "unknown"+action.String(), ch.ID)
			qa := &event.QuickAction{
				ID:         types.MakeUUID(uuid.New()),
				Time:       time.Now(),
				Kind:       action,
				ContactID:  unknown.ID,
				ObjectTags: ev.Tags,
				EventTypes: types.MakeStringList([]string{"DowntimeStart", "DowntimeEnd"}),
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
				assert.Equal(t, i.Recipients[recipient.ToKey(incidentManager)].EventTypes, qa.EventTypes)
				// One notification was caused by the escalation rule, and there should be a second
				// notification caused by the quick action.
				assert.Equal(t, []NotificationState{NotificationStateSent, NotificationStateSent}, notifiedStates(t, db, i, contact, ch.ID))

				// A manager can neither subscribe nor unsubscribe, as this would implicitly demote or remove them
				// without unmanage notifications.
				for _, kind := range []event.Action{event.ActionSubscribe, event.ActionUnsubscribe} {
					qa.Kind = kind
					require.Error(t, Process(t.Context(), db, logs, runtimeConfig, qa))
					i = reloadIncidentRecursive(t, db, i)
					assert.True(t, i.HasManager())
					assert.Equal(t, i.Recipients[recipient.ToKey(incidentManager)].Role, recipient.RoleManager)
					assert.Equal(t, i.Recipients[recipient.ToKey(incidentManager)].EventTypes, qa.EventTypes)
					assert.Equal(t, []NotificationState{NotificationStateSent, NotificationStateSent}, notifiedStates(t, db, i, contact, ch.ID))
				}

				qa.Kind = event.ActionUnmanage
			} else {
				assert.False(t, i.HasManager())
				assert.Equal(t, i.Recipients[recipient.ToKey(incidentSubscriber)].Role, recipient.RoleSubscriber)
				assert.Equal(t, i.Recipients[recipient.ToKey(incidentSubscriber)].EventTypes, qa.EventTypes)
				// There should be only one notification caused by the escalation rule.
				assert.Equal(t, []NotificationState{NotificationStateSent}, notifiedStates(t, db, i, contact, ch.ID))

				qa.Kind = event.ActionUnsubscribe
			}

			// Now, unsubscribe/unmanage the recipient from that very same incident.
			require.NoError(t, Process(t.Context(), db, logs, runtimeConfig, qa))
			i = reloadIncidentRecursive(t, db, i)
			if action == event.ActionManage { // Managers get first demoted to subscribers.
				assert.False(t, i.HasManager())
				assert.Equal(t, i.Recipients[recipient.ToKey(incidentManager)].Role, recipient.RoleSubscriber)
				assert.Equal(t, i.Recipients[recipient.ToKey(incidentManager)].EventTypes, qa.EventTypes)
				// First one is caused by the escalation rule, and the two others are manage/unmanage notifications.
				assert.Equal(t, []NotificationState{NotificationStateSent, NotificationStateSent, NotificationStateSent}, notifiedStates(t, db, i, contact, ch.ID))

				// Cannot unmanage an incident that doesn't have a manager.
				require.Error(t, Process(t.Context(), db, logs, runtimeConfig, qa))
				assert.Equal(t, i.Recipients[recipient.ToKey(incidentManager)].Role, recipient.RoleSubscriber)
				assert.Equal(t, i.Recipients[recipient.ToKey(incidentManager)].EventTypes, qa.EventTypes)

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

	t.Run("SubscribeExistingRecipient", func(t *testing.T) {
		t.Parallel()

		ev := makeEvent(t, source.ID, withIncident(true), withSeverity(baseEv.SeverityError), withMsg("Something went wrong!"))
		i := makeIncident(db, logs, runtimeConfig, t, ev)

		roleChanges := func() int64 {
			var count int64
			require.NoError(t, db.GetContext(
				t.Context(), &count,
				db.Rebind(`SELECT COUNT(*) FROM incident_history WHERE incident_id = ? AND type = 'recipient_role_changed'`),
				i.Id))
			return count
		}

		// The contact is already a plain recipient of the incident due to the triggered escalation.
		i = reloadIncidentRecursive(t, db, i)
		key := recipient.ToKey(contact)
		require.Equal(t, recipient.RoleRecipient, i.Recipients[key].Role)
		assert.Empty(t, i.Recipients[key].EventTypes.List)
		before := roleChanges()

		qa := &event.QuickAction{
			ID:         types.MakeUUID(uuid.New()),
			Time:       time.Now(),
			Kind:       event.ActionSubscribe,
			ContactID:  contact.ID,
			ObjectTags: ev.Tags,
			EventTypes: types.MakeStringList([]string{"DowntimeStart"}),
		}

		// Subscribing an existing recipient promotes them and stores the chosen event types.
		require.NoError(t, Process(t.Context(), db, logs, runtimeConfig, qa))
		i = reloadIncidentRecursive(t, db, i)
		assert.Equal(t, recipient.RoleSubscriber, i.Recipients[key].Role)
		assert.Equal(t, qa.EventTypes, i.Recipients[key].EventTypes)
		assert.Equal(t, before+1, roleChanges())
	})

	// This test mustn't run in parallel with any other test, as ReevaluateEscalations reevaluates the escalations
	// of all due incidents, not only of the one created here, and would thus interfere with other tests relying
	// on time-based escalations being reached at well-defined incident ages.
	t.Run("Time-Based Escalation", func(t *testing.T) {
		i := makeIncident(db, logs, runtimeConfig, t,
			makeEvent(t, source.ID, withIncident(true), withSeverity(baseEv.SeverityCritical)))
		assert.NotNil(t, i)
		assert.NotZero(t, i.NextEscalationCheckAt)
		assert.WithinDuration(t, i.StartedAt.Time().Add(time.Hour), i.NextEscalationCheckAt.Time(), time.Second)

		i = reloadIncidentRecursive(t, db, i)
		// We will find a single escalation state for the incident, because the condition of the escalation defined
		// outside this function is met immediately.
		assert.Len(t, i.EscalationState, 1)
		assert.Len(t, i.EscalationRules, 1)

		assert.NoError(t, ReevaluateEscalations(t.Context(), db, logs.GetChildLogger("incident"), runtimeConfig))
		i = reloadIncidentRecursive(t, db, i)

		// After reevaluating the escalations, we should find two escalation states for the incident,
		// because the second escalation's condition is now met.
		assert.Len(t, i.EscalationState, 2)
		i = reloadIncident(t, db, i)
		assert.Zero(t, i.NextEscalationCheckAt)
	})

	t.Run("Suppression Bypass", func(t *testing.T) {
		t.Parallel()

		relations := map[string]any{"host": map[string]string{"name": "suppression_bypass"}}
		i := makeIncident(db, logs, runtimeConfig, t, makeEvent(t, source.ID,
			withIncident(true), withRelations(relations), withSeverity(baseEv.SeverityError), withMuted(true)))

		// No escalation should be triggered yet, because the incident isn't old enough.
		verifyIncident(t, db, i, 1, 0, 0, 0, baseEv.SeverityError)
		assert.True(t, i.IsMuted())

		obj := mustIncidentObject(t, i)

		// The first escalation is reached, and its recipient+channel pair isn't known to the incident yet,
		// so it bypasses the suppression once.
		require.NoError(t, i.RetriggerEscalations(t.Context(), obj,
			&event.Event{Time: i.StartedAt.Time().Add(31 * time.Minute), Incident: types.MakeBool(true)}))
		verifyIncident(t, db, i, 1, 1, 1, 1, baseEv.SeverityError)
		assert.Equal(t, []NotificationState{NotificationStateSent}, notifiedStates(t, db, i, bypassContact, ch.ID))

		// The second escalation selects the very same recipient+channel pair, so it's suppressed, whereas the third
		// one notifies that recipient via another channel, so it bypasses the suppression once again.
		require.NoError(t, i.RetriggerEscalations(t.Context(), obj,
			&event.Event{Time: i.StartedAt.Time().Add(61 * time.Minute), Incident: types.MakeBool(true)}))
		verifyIncident(t, db, i, 1, 1, 3, 3, baseEv.SeverityError)
		assert.Equal(t, []NotificationState{NotificationStateSent, NotificationStateSuppressed},
			notifiedStates(t, db, i, bypassContact, ch.ID))
		assert.Equal(t, []NotificationState{NotificationStateSent}, notifiedStates(t, db, i, bypassContact, ch2.ID))
	})

	t.Run("Suppression Bypass Known Recipients", func(t *testing.T) {
		t.Parallel()

		relations := map[string]any{"host": map[string]string{"name": "suppression_bypass_known_recipients"}}
		i := makeIncident(db, logs, runtimeConfig, t, makeEvent(t, source.ID,
			withIncident(true), withRelations(relations), withSeverity(baseEv.SeverityError)))

		// Only the severity-based escalation is reached, and the incident isn't muted yet, so it's notified as usual.
		verifyIncident(t, db, i, 1, 1, 1, 1, baseEv.SeverityError)
		assert.Equal(t, []NotificationState{NotificationStateSent}, notifiedStates(t, db, i, knownContact, ch.ID))

		require.NoError(t, Process(t.Context(), db, logs, runtimeConfig, &event.QuickAction{
			ID:         types.MakeUUID(uuid.New()),
			Time:       time.Now(),
			Kind:       event.ActionSubscribe,
			ContactID:  knownSubscriber.ID,
			ObjectTags: mustIncidentObject(t, i).Tags,
		}))
		require.NoError(t, Process(t.Context(), db, logs, runtimeConfig, makeEvent(t, source.ID,
			withTime(i.StartedAt.Time().Add(time.Minute)), withIncident(true), withMuted(true), withRelations(relations),
			withSeverity(baseEv.SeverityError), withTags(mustIncidentObject(t, i).Tags))))
		assert.True(t, reloadIncident(t, db, i).IsMuted())
		subscriberStates := notifiedStates(t, db, i, knownSubscriber, ch.ID)

		require.NoError(t, i.RetriggerEscalations(t.Context(), mustIncidentObject(t, i),
			&event.Event{Time: i.StartedAt.Time().Add(31 * time.Minute), Incident: types.MakeBool(true)}))
		reloaded := reloadIncidentRecursive(t, db, i)
		assert.Len(t, reloaded.EscalationState, 4)
		assert.Len(t, reloaded.Recipients, 3)

		// The known contact was already notified via its default channel by the severity-based escalation, and the
		// subscriber is known to the incident as well, so both are suppressed. Only the fresh contact bypasses it.
		assert.Equal(t, []NotificationState{NotificationStateSent, NotificationStateSuppressed},
			notifiedStates(t, db, i, knownContact, ch.ID))
		assert.Equal(t, append(subscriberStates, NotificationStateSuppressed), notifiedStates(t, db, i, knownSubscriber, ch.ID))
		assert.Equal(t, []NotificationState{NotificationStateSent}, notifiedStates(t, db, i, freshContact, ch.ID))

		// Escalations triggered while processing an event bypass the suppression the same way, while
		// all the other recipients remain suppressed.
		require.NoError(t, Process(t.Context(), db, logs, runtimeConfig, makeEvent(t, source.ID,
			withTime(i.StartedAt.Time().Add(32*time.Minute)), withIncident(true), withRelations(relations),
			withSeverity(baseEv.SeverityCritical), withTags(mustIncidentObject(t, i).Tags))))
		reloaded = reloadIncidentRecursive(t, db, i)
		assert.Len(t, reloaded.EscalationState, 5)
		assert.Len(t, reloaded.Recipients, 4)
		assert.True(t, reloaded.IsMuted())

		assert.Equal(t, []NotificationState{NotificationStateSent}, notifiedStates(t, db, i, escalatedContact, ch.ID))
		assert.Equal(t, []NotificationState{NotificationStateSent, NotificationStateSuppressed, NotificationStateSuppressed},
			notifiedStates(t, db, i, knownContact, ch.ID))
		assert.Equal(t, []NotificationState{NotificationStateSent, NotificationStateSuppressed},
			notifiedStates(t, db, i, freshContact, ch.ID))
		assert.Equal(t, append(subscriberStates, NotificationStateSuppressed, NotificationStateSuppressed),
			notifiedStates(t, db, i, knownSubscriber, ch.ID))

		require.NoError(t, Process(t.Context(), db, logs, runtimeConfig, &event.QuickAction{
			ID:         types.MakeUUID(uuid.New()),
			Time:       time.Now(),
			Kind:       event.ActionManage,
			ContactID:  freshContact.ID,
			ObjectTags: mustIncidentObject(t, i).Tags,
		}))
		reloaded = reloadIncidentRecursive(t, db, i)
		assert.Len(t, reloaded.EscalationState, 5)
		assert.Len(t, reloaded.Recipients, 4)
		assert.True(t, reloaded.HasManager())

		// The fresh contact is now the manager of the incident, so all the other recipients should
		// have received a notification bypassing the suppression.
		assert.Equal(t, []NotificationState{NotificationStateSent, NotificationStateSent}, notifiedStates(t, db, i, escalatedContact, ch.ID))
		assert.Equal(t, []NotificationState{NotificationStateSent, NotificationStateSuppressed, NotificationStateSuppressed,
			NotificationStateSent}, notifiedStates(t, db, i, knownContact, ch.ID))
		// Nothing new for the manager, since we don't notify managers about their own actions.
		assert.Equal(t, []NotificationState{NotificationStateSent, NotificationStateSuppressed},
			notifiedStates(t, db, i, freshContact, ch.ID))
		assert.Equal(t, append(subscriberStates, NotificationStateSuppressed, NotificationStateSuppressed,
			NotificationStateSent), notifiedStates(t, db, i, knownSubscriber, ch.ID))

		require.NoError(t, Process(t.Context(), db, logs, runtimeConfig, &event.QuickAction{
			ID:         types.MakeUUID(uuid.New()),
			Time:       time.Now(),
			Kind:       event.ActionUnmanage,
			ContactID:  freshContact.ID,
			ObjectTags: mustIncidentObject(t, i).Tags,
		}))
		reloaded = reloadIncidentRecursive(t, db, i)
		assert.False(t, reloaded.HasManager())

		// The fresh contact is no longer the manager of the incident, so all the other recipients should
		// have received a notification bypassing the suppression.
		assert.Equal(t, []NotificationState{NotificationStateSent, NotificationStateSent, NotificationStateSent},
			notifiedStates(t, db, i, escalatedContact, ch.ID))
		assert.Equal(t, []NotificationState{NotificationStateSent, NotificationStateSuppressed, NotificationStateSuppressed,
			NotificationStateSent, NotificationStateSent}, notifiedStates(t, db, i, knownContact, ch.ID))
		// Again, nothing new for the manager, since we don't notify managers about their own actions.
		assert.Equal(t, []NotificationState{NotificationStateSent, NotificationStateSuppressed},
			notifiedStates(t, db, i, freshContact, ch.ID))
		assert.Equal(t, append(subscriberStates, NotificationStateSuppressed, NotificationStateSuppressed,
			NotificationStateSent, NotificationStateSent), notifiedStates(t, db, i, knownSubscriber, ch.ID))
	})

	t.Run("Managed Escalation", func(t *testing.T) {
		t.Parallel()

		relations := map[string]any{"host": map[string]string{"name": "managed_escalations"}}
		ev := makeEvent(t, source.ID, withIncident(true), withSeverity(baseEv.SeverityCritical), withRelations(relations))
		i := reloadIncidentRecursive(t, db, makeIncident(db, logs, runtimeConfig, t, ev))
		require.NotNil(t, i)

		// Nobody has taken over the responsibility for the incident yet, so only the first escalation is triggered.
		require.Len(t, i.EscalationRules, 1)
		_, exists := i.EscalationRules[managedRule.ID]
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
		ev := makeEvent(t, source.ID, withIncident(true), withTime(time.Now()), withSeverity(baseEv.SeverityOK), withTags(tags))
		msg, summary := ev.Message, ev.Summary
		assert.NotZero(t, ev.ID)
		i := makeIncident(db, logs, runtimeConfig, t, ev)
		assert.NotZero(t, i.Id)
		assert.Zero(t, i.RecoveredAt)
		assert.Equal(t, baseEv.SeverityOK, i.Severity)

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
				assert.Equal(t, ch.Name, entry.ChannelName)
				assert.Equal(t, types.MakeString(contact.FullName), entry.ContactName)
				assert.Equal(t, msg, entry.EventMessage)
				assert.Equal(t, summary, entry.EventSummary)
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

	t.Run("Generate Notifications", func(t *testing.T) {
		t.Parallel()

		logger := logs.GetChildLogger("incident").SugaredLogger
		assertNotificationEntry := func(notification *NotificationEntry, passiveNotification bool) {
			switch notification.ContactID {
			case notificationContact.ID:
				assert.Equal(t, ch.ID, notification.ChannelID)
			case notificationContact2.ID:
				assert.Equal(t, ch2.ID, notification.ChannelID)
			case passiveNotificationContact.ID:
				assert.Equal(t, ch.ID, notification.ChannelID)
			}
		}

		t.Run("Notification Without Incident", func(t *testing.T) {
			t.Parallel()

			tags := map[string]string{"generate_notification_test": "without-incident"}
			relations := map[string]any{"host": map[string]string{"name": "notifications_rule"}}
			ev := makeEvent(t, source.ID, withIncident(false), withEventType("test-type"), withRelations(relations), withTags(tags))
			msg, summary := ev.Message, ev.Summary

			var notifications []*NotificationEntry
			err := db.ExecTx(t.Context(), nil, func(ctx context.Context, tx *sqlx.Tx) error {
				i := new(Incident)
				i.initializeFields(db, runtimeConfig, logger)
				i.runtimeConfig.RLock()
				defer i.runtimeConfig.RUnlock()

				require.NoError(t, i.applyMatchingRules(ctx, tx, ev))

				var err error
				notifications, err = i.generateNotifications(ctx, tx, ev, i.collectContactChannels(ev, nil), notifyReason{Kind: ReasonEvent})
				require.NoError(t, err)

				return nil
			})
			require.NoError(t, err)
			assert.NotNil(t, notifications)

			for _, notification := range notifications {
				assertNotificationEntry(notification, false)
				assert.False(t, notification.IsIncidentRelated)
				assert.Equal(t, msg, notification.HistoryEntry.EventMessage)
				assert.Equal(t, summary, notification.HistoryEntry.EventSummary)
			}
		})

		t.Run("Notification With Incident", func(t *testing.T) {
			t.Parallel()

			tags := map[string]string{"generate_notification_test": "with-incident"}
			msg := testutils.MakeRandomString(t)
			relations := map[string]any{"host": map[string]string{"name": "passive_notifications_rule_by_non_state_event"}}
			ev := makeEvent(t, source.ID, withIncident(true), withTags(tags), withRelations(relations), withSeverity(baseEv.SeverityCritical))
			require.NoError(t, Process(t.Context(), db, logs, runtimeConfig, ev))

			var notifications []*NotificationEntry
			err := db.ExecTx(t.Context(), nil, func(ctx context.Context, tx *sqlx.Tx) error {
				i := &Incident{ObjectID: object.ID(tags)}
				i.initializeFields(db, runtimeConfig, logger)
				i = reloadIncidentRecursive(t, db, i)
				i.runtimeConfig.RLock()
				defer i.runtimeConfig.RUnlock()

				ev = makeEvent(t, source.ID, withIncident(false), withEventType("test-type"), withRelations(relations), withTags(tags), withMsg(msg))
				require.NoError(t, i.applyMatchingRules(ctx, tx, ev))

				var err error
				notifications, err = i.generateNotifications(ctx, tx, ev, i.collectContactChannels(ev, nil), notifyReason{Kind: ReasonEvent})
				require.NoError(t, err)

				return nil
			})
			require.NoError(t, err)
			assert.NotNil(t, notifications)
			assert.Len(t, notifications, 3)

			seen := make(map[int64]int64)
			for _, notification := range notifications {
				assert.NotEqual(t, seen[notification.ChannelID], notification.ContactID)
				assertNotificationEntry(notification, true)

				assert.Equal(t, msg, notification.HistoryEntry.EventMessage)
				seen[notification.ChannelID] = notification.ContactID
			}
		})
	})
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
	ev := makeEventNoValidate(t, sourceID, opts...)
	require.NoError(t, ev.Validate(), "failed to validate event")
	return ev
}

func makeEventNoValidate(t *testing.T, sourceID int64, opts ...eventOption) *event.Event {
	ev := &event.Event{
		SourceId: sourceID,
		ID:       types.MakeUUID(uuid.New()),
		Name:     testutils.MakeRandomString(t),
		Summary:  testutils.MakeRandomString(t),
		Message:  testutils.MakeRandomString(t),
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
	if ev.Time.IsZero() {
		ev.Time = time.Now().Add(-2 * time.Hour).Truncate(time.Second)
	}
	if ev.OpenOrEscalate() && ev.Severity == baseEv.SeverityNone {
		ev.Severity = baseEv.SeverityWarning
	}
	if ev.Muted.Valid {
		ev.MutedReason = "You're gonna have a bad time!"
	}
	return ev
}

// eventOption is a functional option type for modifying an event.
type eventOption func(*event.Event)

func withTime(t time.Time) eventOption { return func(ev *event.Event) { ev.Time = t } }
func withIncident(v bool) eventOption {
	return func(ev *event.Event) { ev.Incident = types.MakeBool(v) }
}
func withClose() eventOption                      { return func(ev *event.Event) { ev.Close = types.MakeBool(true) } }
func withMuted(v bool) eventOption                { return func(ev *event.Event) { ev.Muted = types.MakeBool(v) } }
func withNotify() eventOption                     { return func(ev *event.Event) { ev.Notify = types.MakeBool(true) } }
func withTags(tags map[string]string) eventOption { return func(ev *event.Event) { ev.Tags = tags } }
func withMsg(msg string) eventOption              { return func(ev *event.Event) { ev.Message = msg } }
func withSeverity(sev baseEv.Severity) eventOption {
	return func(ev *event.Event) { ev.Severity = sev }
}
func withEventType(eventType string) eventOption {
	return func(ev *event.Event) { ev.Type = eventType }
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

// assertHistoryNotified checks the incident history for the given incident and asserts that the number
// of "notified" entries matches the expected count.
func assertHistoryNotified(t *testing.T, db *database.DB, i *Incident, expected int) {
	var notifiedCount int64
	require.NoError(t, db.GetContext(
		t.Context(), &notifiedCount,
		db.Rebind(`SELECT COUNT(*) FROM incident_history WHERE incident_id = ? AND type = 'notified'`),
		i.Id))
	assert.Equal(t, int64(expected), notifiedCount)
}

// notifiedStates returns the notification states of all "notified" incident history entries of the given
// incident, contact and channel in the order they were recorded.
func notifiedStates(t *testing.T, db *database.DB, i *Incident, c *recipient.Contact, channelID int64) []NotificationState {
	var states []NotificationState
	require.NoError(t, db.SelectContext(t.Context(), &states, db.Rebind(
		`SELECT notification_state FROM incident_history
		WHERE incident_id = ? AND type = 'notified' AND contact_id = ? AND channel_id = ?
		ORDER BY id`),
		i.Id, c.ID, channelID))
	return states
}

func verifyIncident(t *testing.T, db *database.DB, i *Incident, ruleCount, recipientCount, escalationCount, notifiedCount int, severity baseEv.Severity) {
	i = reloadIncidentRecursive(t, db, i)
	assert.Equal(t, severity, i.Severity)
	assert.Len(t, i.EscalationRules, ruleCount)
	assert.Len(t, i.EscalationState, escalationCount)
	assert.Len(t, i.Recipients, recipientCount)
	assertHistoryNotified(t, db, i, notifiedCount)
}
