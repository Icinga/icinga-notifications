package listener

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/icinga/icinga-go-library/database"
	"github.com/icinga/icinga-go-library/logging"
	baseEv "github.com/icinga/icinga-go-library/notifications/event"
	"github.com/icinga/icinga-go-library/notifications/source"
	"github.com/icinga/icinga-go-library/types"
	"github.com/icinga/icinga-notifications/internal/config"
	"github.com/icinga/icinga-notifications/internal/incident"
	"github.com/icinga/icinga-notifications/internal/object"
	"github.com/icinga/icinga-notifications/internal/testutils"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"
	"golang.org/x/crypto/bcrypt"
)

// The following minimal row types satisfy the foreign keys notification_history needs, without pulling in the
// production channel/recipient/incident packages, whose exported types either can't be inserted directly (missing
// columns like external_uuid) or hold unexported fields (e.g. incident.Incident.db) that can only be set from
// within the incident package itself.

type nhTestAvailableChannelType struct {
	Type        string `db:"type"`
	Name        string `db:"name"`
	Version     string `db:"version"`
	Author      string `db:"author"`
	ConfigAttrs string `db:"config_attrs"`
}

func (nhTestAvailableChannelType) TableName() string { return "available_channel_type" }

type nhTestChannel struct {
	ID           int64           `db:"id"`
	ExternalUUID string          `db:"external_uuid"`
	Name         string          `db:"name"`
	Type         string          `db:"type"`
	ChangedAt    types.UnixMilli `db:"changed_at"`
	Deleted      types.Bool      `db:"deleted"`
}

func (nhTestChannel) TableName() string { return "channel" }

type nhTestContact struct {
	ID               int64           `db:"id"`
	ExternalUUID     string          `db:"external_uuid"`
	FullName         string          `db:"full_name"`
	DefaultChannelID int64           `db:"default_channel_id"`
	ChangedAt        types.UnixMilli `db:"changed_at"`
	Deleted          types.Bool      `db:"deleted"`
}

func (nhTestContact) TableName() string { return "contact" }

type nhTestIncident struct {
	ID        int64           `db:"id"`
	ObjectID  types.Binary    `db:"object_id"`
	StartedAt types.UnixMilli `db:"started_at"`
	Severity  baseEv.Severity `db:"severity"`
}

func (nhTestIncident) TableName() string { return "incident" }

// notificationHistoryEndpointFixture inserts a source plus one notification_history row belonging to it, so
// GetNotificationHistory has something to stream back.
type notificationHistoryEndpointFixture struct {
	SourceID    int64
	IncidentID  int64
	ChannelID   int64
	ChannelType string
	ContactID   int64
	EventID     types.UUID
}

func makeNotificationHistoryEndpointFixture(t *testing.T, db *database.DB, sourceUsername, sourcePasswordHash string) *notificationHistoryEndpointFixture {
	now := types.UnixMilli(time.Now())
	f := &notificationHistoryEndpointFixture{EventID: types.UUID{}}

	require.NoError(t, db.ExecTx(t.Context(), nil, func(ctx context.Context, tx *sqlx.Tx) error {
		src := &config.Source{
			Type:                 "notifications",
			Name:                 testutils.MakeRandomString(t),
			ListenerUsername:     types.MakeString(sourceUsername),
			ListenerPasswordHash: types.MakeString(sourcePasswordHash),
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

		inc := &nhTestIncident{ObjectID: objID, StartedAt: now, Severity: baseEv.SeverityCrit}
		incID, err := database.InsertObtainID(ctx, tx, database.BuildInsertStmtWithout(db, inc, "id"), inc)
		require.NoError(t, err)
		f.IncidentID = incID

		channelType := &nhTestAvailableChannelType{
			Type: testutils.MakeRandomString(t), Name: "Test", Version: "1.0.0", Author: "Test", ConfigAttrs: "[]",
		}
		stmt, _ := db.BuildInsertStmt(channelType)
		_, err = tx.NamedExecContext(ctx, stmt, channelType)
		require.NoError(t, err)
		f.ChannelType = channelType.Type

		ch := &nhTestChannel{
			ExternalUUID: testutils.MakeRandomString(t)[:32], Name: "Test Channel", Type: channelType.Type,
			ChangedAt: now, Deleted: types.MakeBool(false),
		}
		chID, err := database.InsertObtainID(ctx, tx, database.BuildInsertStmtWithout(db, ch, "id"), ch)
		require.NoError(t, err)
		f.ChannelID = chID

		contact := &nhTestContact{
			ExternalUUID: testutils.MakeRandomString(t)[:32], FullName: "Test Contact " + testutils.MakeRandomString(t),
			DefaultChannelID: chID, ChangedAt: now, Deleted: types.MakeBool(false),
		}
		contactID, err := database.InsertObtainID(ctx, tx, database.BuildInsertStmtWithout(db, contact, "id"), contact)
		require.NoError(t, err)
		f.ContactID = contactID

		hr := &incident.HistoryRow{IncidentID: incID, Time: now, Type: incident.Notified}
		require.NoError(t, hr.Sync(ctx, db, tx))

		nh := &incident.NotificationHistory{
			IncidentHistoryID: hr.ID,
			SourceID:          f.SourceID,
			EventID:           f.EventID,
			TriggeredAt:       now,
			ContactID:         contactID,
			ChannelID:         chID,
			IncidentID:        types.MakeInt(incID, types.TransformZeroIntToNull),
			Message:           types.MakeString("Something went wrong.", types.TransformEmptyStringToNull),
			State:             source.NotificationStateSent,
		}
		require.NoError(t, nh.Sync(ctx, db, tx))

		return nil
	}))

	return f
}

// cleanupNotificationHistoryEndpointFixtures deletes exactly the rows created by makeNotificationHistoryEndpointFixture,
// scoped by the fixture's own IDs.
//
// This deliberately avoids unscoped `DELETE FROM <table>`: `go test ./...` runs different packages' test binaries
// concurrently against the same shared external test database (see .github/workflows/tests_with_database.yml),
// and internal/incident's tests populate these same tables too. Wiping whole tables here would race with, and
// delete, rows that another package's concurrently running test still needs.
func cleanupNotificationHistoryEndpointFixtures(ctx context.Context, db *database.DB, t *testing.T, fixtures ...*notificationHistoryEndpointFixture) {
	for _, f := range fixtures {
		for _, stmt := range []struct {
			query string
			arg   any
		}{
			{`DELETE FROM "skipped_notification_history" WHERE "notification_history_id" IN (SELECT "id" FROM "notification_history" WHERE "source_id" = ?)`, f.SourceID},
			{`DELETE FROM "notification_history" WHERE "source_id" = ?`, f.SourceID},
			{`DELETE FROM "incident_history" WHERE "incident_id" = ?`, f.IncidentID},
			{`DELETE FROM "incident" WHERE "id" = ?`, f.IncidentID},
			{`DELETE FROM "object" WHERE "source_id" = ?`, f.SourceID},
			{`DELETE FROM "contact" WHERE "id" = ?`, f.ContactID},
			{`DELETE FROM "channel" WHERE "id" = ?`, f.ChannelID},
			{`DELETE FROM "available_channel_type" WHERE "type" = ?`, f.ChannelType},
			{`DELETE FROM "source" WHERE "id" = ?`, f.SourceID},
		} {
			_, err := db.ExecContext(ctx, db.Rebind(stmt.query), stmt.arg)
			assert.NoError(t, err, "cleaning up fixture with query %q should not fail", stmt.query)
		}
	}
}

func TestGetNotificationHistory(t *testing.T) {
	db := testutils.GetTestDB(t.Context(), t)

	hash, err := bcrypt.GenerateFromPassword([]byte("secret"), bcrypt.MinCost)
	require.NoError(t, err)

	// Each fixture's cleanup is registered right after it's created, not batched after both: if the second
	// makeNotificationHistoryEndpointFixture call fails partway (via require.NoError -> t.FailNow()), execution
	// never reaches a cleanup call registered below it, which would otherwise leak the first fixture's rows.
	fixture := makeNotificationHistoryEndpointFixture(t, db, "icingadb", string(hash))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cleanupNotificationHistoryEndpointFixtures(ctx, db, t, fixture)
	})

	other := makeNotificationHistoryEndpointFixture(t, db, "other-source", string(hash))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cleanupNotificationHistoryEndpointFixtures(ctx, db, t, other)
	})

	logs := logging.NewLoggingWithFactory("testing", zapcore.DebugLevel, time.Second, func(level zap.AtomicLevel) zapcore.Core {
		return zaptest.NewLogger(t, zaptest.Level(level.Level())).Core()
	})
	l := &Listener{
		db:            db,
		logger:        logs.GetChildLogger("listener"),
		runtimeConfig: config.NewRuntimeConfig(logs, nil),
	}
	fixtureSrc := &config.Source{ListenerUsername: types.MakeString("icingadb"), ListenerPasswordHash: types.MakeString(string(hash))}
	fixtureSrc.ID = fixture.SourceID
	otherSrc := &config.Source{ListenerUsername: types.MakeString("other-source"), ListenerPasswordHash: types.MakeString(string(hash))}
	otherSrc.ID = other.SourceID
	l.runtimeConfig.Sources = map[int64]*config.Source{
		fixture.SourceID: fixtureSrc,
		other.SourceID:   otherSrc,
	}

	t.Run("Missing Since Returns 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/notification-history", nil)
		req.SetBasicAuth("icingadb", "secret")
		rw := httptest.NewRecorder()

		l.GetNotificationHistory(rw, req)
		assert.Equal(t, http.StatusBadRequest, rw.Code)
	})

	t.Run("Bad Credentials Returns 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/notification-history?since=0", nil)
		req.SetBasicAuth("icingadb", "wrong-password")
		rw := httptest.NewRecorder()

		l.GetNotificationHistory(rw, req)
		assert.Equal(t, http.StatusUnauthorized, rw.Code)
	})

	t.Run("Happy Path Scopes To Requesting Source", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/notification-history?since=0", nil)
		req.SetBasicAuth("icingadb", "secret")
		rw := httptest.NewRecorder()

		l.GetNotificationHistory(rw, req)
		assert.Equal(t, http.StatusAccepted, rw.Code)

		body := rw.Body.String()
		assert.Contains(t, body, `"state":"sent"`)
		assert.NotContains(t, body, "error")

		// Exactly one line (NDJSON) must be present, and it must be the requesting source's own entry, not the
		// other source's.
		lines := 0
		for _, r := range body {
			if r == '\n' {
				lines++
			}
		}
		assert.Equal(t, 1, lines)
	})

	t.Run("Valid Since Returns 202", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/notification-history?since=0", nil)
		req.SetBasicAuth("icingadb", "secret")
		rw := httptest.NewRecorder()

		l.GetNotificationHistory(rw, req)
		assert.Equal(t, 202, rw.Code)
	})
}
