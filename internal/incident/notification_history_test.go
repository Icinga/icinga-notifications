package incident

import (
	"context"
	"testing"
	"time"

	"github.com/icinga/icinga-go-library/database"
	baseEv "github.com/icinga/icinga-go-library/notifications/event"
	"github.com/icinga/icinga-go-library/notifications/source"
	"github.com/icinga/icinga-go-library/types"
	"github.com/icinga/icinga-notifications/internal/config"
	"github.com/icinga/icinga-notifications/internal/event"
	"github.com/icinga/icinga-notifications/internal/object"
	"github.com/icinga/icinga-notifications/internal/recipient"
	"github.com/icinga/icinga-notifications/internal/rule"
	"github.com/icinga/icinga-notifications/internal/testutils"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// testAvailableChannelType, testChannel and testContact are minimal stand-ins for the channel/contact rows
// generateNotifications needs to satisfy foreign keys with. There's no need to spin up an actual channel plugin
// here, as generateNotifications never talks to one -- it only records the channel/contact IDs.

type testAvailableChannelType struct {
	Type        string `db:"type"`
	Name        string `db:"name"`
	Version     string `db:"version"`
	Author      string `db:"author"`
	ConfigAttrs string `db:"config_attrs"`
}

func (testAvailableChannelType) TableName() string { return "available_channel_type" }

type testChannel struct {
	ID           int64           `db:"id"`
	ExternalUUID string          `db:"external_uuid"`
	Name         string          `db:"name"`
	Type         string          `db:"type"`
	ChangedAt    types.UnixMilli `db:"changed_at"`
	Deleted      types.Bool      `db:"deleted"`
}

func (testChannel) TableName() string { return "channel" }

type testContact struct {
	ID               int64           `db:"id"`
	ExternalUUID     string          `db:"external_uuid"`
	FullName         string          `db:"full_name"`
	DefaultChannelID int64           `db:"default_channel_id"`
	ChangedAt        types.UnixMilli `db:"changed_at"`
	Deleted          types.Bool      `db:"deleted"`
}

func (testContact) TableName() string { return "contact" }

// notificationHistoryFixture bundles the rows generateNotifications and YieldNotificationHistoryForSource need to
// satisfy foreign keys: a source, an incident (with its object), a channel/contact pair, and a rule with two
// escalations (used to produce two distinct [rule.ChannelOrigin]s resolving to the same contact+channel).
type notificationHistoryFixture struct {
	SourceID     int64
	IncidentID   int64
	ChannelID    int64
	ChannelType  string
	Contact      *recipient.Contact
	RuleID       int64
	EscalationID [2]int64
}

func makeNotificationHistoryFixture(t *testing.T, db *database.DB) *notificationHistoryFixture {
	now := types.UnixMilli(time.Now())
	f := &notificationHistoryFixture{}

	require.NoError(t, db.ExecTx(t.Context(), nil, func(ctx context.Context, tx *sqlx.Tx) error {
		src := &config.Source{
			Type:             "notifications",
			Name:             testutils.MakeRandomString(t),
			ListenerUsername: types.MakeString(testutils.MakeRandomString(t)),
		}
		src.ChangedAt = now
		src.Deleted = types.MakeBool(false)
		id, err := database.InsertObtainID(ctx, tx, database.BuildInsertStmtWithout(db, src, "id"), src)
		require.NoError(t, err)
		f.SourceID = id

		objID := object.ID(f.SourceID, map[string]string{"host": testutils.MakeRandomString(t)})
		_, err = tx.ExecContext(ctx, db.Rebind(`INSERT INTO "object" ("id", "source_id", "name") VALUES (?, ?, ?)`),
			[]byte(objID), f.SourceID, "test object")
		require.NoError(t, err)

		incident := &Incident{ObjectID: objID, StartedAt: now, Severity: baseEv.SeverityCrit}
		incident.db = db
		require.NoError(t, incident.Sync(ctx, tx))
		f.IncidentID = incident.Id

		channelType := &testAvailableChannelType{
			Type:        testutils.MakeRandomString(t),
			Name:        "Test Channel Type",
			Version:     "1.0.0",
			Author:      "Test",
			ConfigAttrs: "[]",
		}
		stmt, _ := db.BuildInsertStmt(channelType)
		_, err = tx.NamedExecContext(ctx, stmt, channelType)
		require.NoError(t, err)
		f.ChannelType = channelType.Type

		ch := &testChannel{
			ExternalUUID: testutils.MakeRandomString(t)[:32],
			Name:         "Test Channel",
			Type:         channelType.Type,
			ChangedAt:    now,
			Deleted:      types.MakeBool(false),
		}
		id, err = database.InsertObtainID(ctx, tx, database.BuildInsertStmtWithout(db, ch, "id"), ch)
		require.NoError(t, err)
		f.ChannelID = id

		contact := &testContact{
			ExternalUUID:     testutils.MakeRandomString(t)[:32],
			FullName:         "Test Contact " + testutils.MakeRandomString(t),
			DefaultChannelID: f.ChannelID,
			ChangedAt:        now,
			Deleted:          types.MakeBool(false),
		}
		id, err = database.InsertObtainID(ctx, tx, database.BuildInsertStmtWithout(db, contact, "id"), contact)
		require.NoError(t, err)
		f.Contact = &recipient.Contact{FullName: contact.FullName}
		f.Contact.ID = id

		r := &rule.Rule{Name: testutils.MakeRandomString(t)}
		r.SourceID = f.SourceID
		r.ChangedAt = now
		r.Deleted = types.MakeBool(false)
		id, err = database.InsertObtainID(ctx, tx, database.BuildInsertStmtWithout(db, r, "id"), r)
		require.NoError(t, err)
		f.RuleID = id

		for i, pos := range []int64{1, 2} {
			esc := &escalationRow{
				RuleID:    f.RuleID,
				Position:  pos,
				Condition: "incident_age>=1h",
				ChangedAt: now,
				Deleted:   types.MakeBool(false),
			}
			stmt, _ := db.BuildInsertStmt(esc)
			escID, err := database.InsertObtainID(ctx, tx, stmt, esc)
			require.NoError(t, err)
			f.EscalationID[i] = escID
		}

		return nil
	}))

	return f
}

// cleanupNotificationHistoryFixtures deletes exactly the rows created by makeNotificationHistoryFixture, scoped by
// the fixture's own IDs.
//
// This deliberately avoids unscoped `DELETE FROM <table>`: `go test ./...` runs different packages' test binaries
// concurrently against the same shared external test database (see .github/workflows/tests_with_database.yml),
// and internal/listener's tests now populate these same tables too. Wiping whole tables here would race with,
// and delete, rows that another package's concurrently running test still needs.
func cleanupNotificationHistoryFixtures(ctx context.Context, db *database.DB, t *testing.T, fixtures ...*notificationHistoryFixture) {
	for _, f := range fixtures {
		for _, stmt := range []struct {
			query string
			arg   any
		}{
			{`DELETE FROM "skipped_notification_history" WHERE "notification_history_id" IN (SELECT "id" FROM "notification_history" WHERE "source_id" = ?)`, f.SourceID},
			{`DELETE FROM "notification_history" WHERE "source_id" = ?`, f.SourceID},
			{`DELETE FROM "incident_history" WHERE "incident_id" = ?`, f.IncidentID},
			{`DELETE FROM "rule_escalation" WHERE "rule_id" = ?`, f.RuleID},
			{`DELETE FROM "rule" WHERE "id" = ?`, f.RuleID},
			{`DELETE FROM "incident" WHERE "id" = ?`, f.IncidentID},
			{`DELETE FROM "object" WHERE "source_id" = ?`, f.SourceID},
			{`DELETE FROM "contact" WHERE "id" = ?`, f.Contact.ID},
			{`DELETE FROM "channel" WHERE "id" = ?`, f.ChannelID},
			{`DELETE FROM "available_channel_type" WHERE "type" = ?`, f.ChannelType},
			{`DELETE FROM "source" WHERE "id" = ?`, f.SourceID},
		} {
			_, err := db.ExecContext(ctx, db.Rebind(stmt.query), stmt.arg)
			assert.NoError(t, err, "cleaning up fixture with query %q should not fail", stmt.query)
		}
	}
}

func TestGenerateNotifications(t *testing.T) {
	db := testutils.GetTestDB(t.Context(), t)

	f := makeNotificationHistoryFixture(t, db)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cleanupNotificationHistoryFixtures(ctx, db, t, f)
	})

	ev, err := event.CreateEvent(f.SourceID, baseEv.Event{Name: "dummy", Message: "Something went wrong."})
	require.NoError(t, err)

	// Two origins (two different rule escalations) resolve to the same contact+channel: this must yield a single
	// pending NotificationEntry/notification_history row for the first origin, and a skipped_notification_history
	// row for the second one, matching the dedup logic added to generateNotifications/rule.ContactChannels.
	contactChannels := rule.ContactChannels{
		f.Contact: {
			f.ChannelID: {
				{RuleID: f.RuleID, RuleEscalationID: f.EscalationID[0], Role: recipient.RoleRecipient},
				{RuleID: f.RuleID, RuleEscalationID: f.EscalationID[1], Role: recipient.RoleRecipient},
			},
		},
	}

	i := &Incident{Id: f.IncidentID, db: db, logger: zaptest.NewLogger(t).Sugar()}

	var notifications []*NotificationEntry
	require.NoError(t, db.ExecTx(t.Context(), nil, func(ctx context.Context, tx *sqlx.Tx) error {
		notifications, err = i.generateNotifications(ctx, tx, &ev, contactChannels)
		return err
	}))

	require.Len(t, notifications, 1, "duplicate origins for the same contact+channel must yield a single pending entry")
	assert.Equal(t, f.Contact.ID, notifications[0].ContactID)
	assert.Equal(t, f.ChannelID, notifications[0].ChannelID)
	assert.Equal(t, source.NotificationStatePending, notifications[0].State)

	// Select only "id" rather than scanning a full *HistoryRow: incident_history.new_recipient_role/
	// old_recipient_role are NULL for a "Notified" row, and recipient.ContactRole.Scan has a pre-existing,
	// unrelated bug (checks the receiver instead of src for nil) that errors out on NULL role columns.
	var historyRowIDs []int64
	require.NoError(t, db.SelectContext(t.Context(), &historyRowIDs,
		db.Rebind(`SELECT "id" FROM "incident_history" WHERE "incident_id" = ? AND "type" = ?`),
		f.IncidentID, Notified))
	require.Len(t, historyRowIDs, 1)

	var notificationHistoryRows []*NotificationHistory
	require.NoError(t, db.SelectContext(t.Context(), &notificationHistoryRows,
		db.Rebind(`SELECT * FROM "notification_history" WHERE "incident_history_id" = ?`), historyRowIDs[0]))
	require.Len(t, notificationHistoryRows, 1, "exactly one notification_history row must be created for the deduplicated origin")
	nh := notificationHistoryRows[0]
	assert.Equal(t, ev.ID, nh.EventID, "notification_history.event_id must correlate to the triggering event")
	assert.Equal(t, f.Contact.ID, nh.ContactID)
	assert.Equal(t, f.ChannelID, nh.ChannelID)
	assert.EqualValues(t, f.IncidentID, nh.IncidentID.Int64)
	// generateNotifications always writes the fail-safe default; only notifyContacts' later update can set it to
	// sent, and that path isn't exercised here.
	assert.Equal(t, source.NotificationStateFailed, nh.State)

	var skipped []*NotificationSkippedHistoryEntry
	require.NoError(t, db.SelectContext(t.Context(), &skipped,
		db.Rebind(`SELECT "notification_history_id", "rule_id", "rule_escalation_id", "contactgroup_id", "schedule_id"
			FROM "skipped_notification_history" WHERE "notification_history_id" = ?`), nh.ID))
	require.Len(t, skipped, 1, "the second, duplicate origin must be recorded as a skipped notification")
	assert.Equal(t, f.RuleID, skipped[0].RuleID)
	assert.Equal(t, f.EscalationID[1], skipped[0].RuleEscalationID)
}

func TestYieldNotificationHistoryForSource(t *testing.T) {
	db := testutils.GetTestDB(t.Context(), t)

	// Each fixture's cleanup is registered right after it's created, not batched after both: if the second
	// makeNotificationHistoryFixture call fails partway (via require.NoError -> t.FailNow()), execution never
	// reaches a cleanup call registered below it, which would otherwise leak the first fixture's rows.
	f := makeNotificationHistoryFixture(t, db)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cleanupNotificationHistoryFixtures(ctx, db, t, f)
	})

	other := makeNotificationHistoryFixture(t, db)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cleanupNotificationHistoryFixtures(ctx, db, t, other)
	})

	ev, err := event.CreateEvent(f.SourceID, baseEv.Event{Name: "dummy", Message: "Something went wrong."})
	require.NoError(t, err)

	i := &Incident{Id: f.IncidentID, db: db, logger: zaptest.NewLogger(t).Sugar()}
	contactChannels := rule.ContactChannels{
		f.Contact: {f.ChannelID: {{RuleID: f.RuleID, RuleEscalationID: f.EscalationID[0], Role: recipient.RoleRecipient}}},
	}
	require.NoError(t, db.ExecTx(t.Context(), nil, func(ctx context.Context, tx *sqlx.Tx) error {
		_, err := i.generateNotifications(ctx, tx, &ev, contactChannels)
		return err
	}))

	otherEv, err := event.CreateEvent(other.SourceID, baseEv.Event{Name: "dummy", Message: "Other source."})
	require.NoError(t, err)
	otherIncident := &Incident{Id: other.IncidentID, db: db, logger: zaptest.NewLogger(t).Sugar()}
	otherContactChannels := rule.ContactChannels{
		other.Contact: {other.ChannelID: {{RuleID: other.RuleID, RuleEscalationID: other.EscalationID[0], Role: recipient.RoleRecipient}}},
	}
	require.NoError(t, db.ExecTx(t.Context(), nil, func(ctx context.Context, tx *sqlx.Tx) error {
		_, err := otherIncident.generateNotifications(ctx, tx, &otherEv, otherContactChannels)
		return err
	}))

	t.Run("Scopes To The Requesting Source", func(t *testing.T) {
		entryCh, errCh := YieldNotificationHistoryForSource(t.Context(), db, "0", f.SourceID)

		var entries []source.NotificationHistory
		for entry := range entryCh {
			entries = append(entries, entry)
		}
		require.NoError(t, <-errCh)

		require.Len(t, entries, 1, "only the requesting source's notification history entries must be returned")
		assert.Equal(t, f.Contact.FullName, entries[0].ContactName)
		assert.True(t, entries[0].ContactgroupName.Valid, "contactgroup_name must be an empty string, not null, when there's no contactgroup")
		assert.Empty(t, entries[0].ContactgroupName.String)
		assert.True(t, entries[0].ScheduleName.Valid, "schedule_name must be an empty string, not null, when there's no schedule")
		assert.Empty(t, entries[0].ScheduleName.String)
	})

	t.Run("Filters By Since", func(t *testing.T) {
		entryCh, errCh := YieldNotificationHistoryForSource(t.Context(), db, "9999999999999", f.SourceID)
		var entries []source.NotificationHistory
		for entry := range entryCh {
			entries = append(entries, entry)
		}
		require.NoError(t, <-errCh)
		assert.Empty(t, entries, "an implausibly high since value must exclude all entries")
	})
}
