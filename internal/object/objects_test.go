package object

import (
	"context"
	"github.com/icinga/icinga-go-library/database"
	baseEv "github.com/icinga/icinga-go-library/notifications/event"
	"github.com/icinga/icinga-go-library/types"
	"github.com/icinga/icinga-notifications/internal/event"
	"github.com/icinga/icinga-notifications/internal/testutils"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestRestoreMutedObjects(t *testing.T) {
	ctx := context.Background()
	db := testutils.GetTestDB(ctx, t)

	var sourceID int64
	err := db.ExecTx(ctx, func(ctx context.Context, tx *sqlx.Tx) error {
		args := map[string]any{
			"type":       "notifications",
			"name":       "Icinga Notifications",
			"changed_at": int64(1720702049000),
			"pwd_hash":   "$2y$", // Needed to pass the database constraint.
		}
		// We can't use config.Source here unfortunately due to cyclic import error!
		id, err := database.InsertObtainID(
			ctx,
			tx,
			`INSERT INTO source (type, name, changed_at, listener_password_hash) VALUES (:type, :name, :changed_at, :pwd_hash)`,
			args)
		require.NoError(t, err, "populating source table should not fail")

		sourceID = id
		return nil
	})
	require.NoError(t, err, "db.ExecTx should not fail")

	ClearCache()

	// Just to make sure that there are no objects that have already been muted.
	require.NoError(t, RestoreMutedObjects(ctx, db), "restoring muted objects shouldn't fail")
	require.Len(t, cache, 0, "found mysterious muted objects")

	testObjects := map[string]*Object{}
	for i := 0; i < 20; i++ {
		o := makeObject(ctx, db, t, sourceID, true)
		testObjects[o.ID.String()] = o
		if i%2 == 0 { // Insert also some unmuted objects
			makeObject(ctx, db, t, sourceID, false)
		}
	}
	ClearCache()

	require.NoError(t, RestoreMutedObjects(ctx, db), "restoring muted objects shouldn't fail")
	assert.Len(t, cache, len(testObjects), "all muted objects should be restored")

	for _, o := range testObjects {
		objFromCache := GetFromCache(o.ID)
		assert.NotNilf(t, objFromCache, "muted object %q was not restored correctly", o.DisplayName())

		if objFromCache != nil {
			assert.True(t, objFromCache.IsMuted(), "object should be muted")
			assert.Equal(t, o.Name, objFromCache.Name, "objects name should match")
			assert.Equal(t, o.URL, objFromCache.URL, "objects url should match")

			assert.Equal(t, o.Tags, objFromCache.Tags, "objects tags should match")
		}

		// Purge all newly created objects and their relations not mes up local database tests.
		_, err = db.NamedExecContext(ctx, `DELETE FROM object_id_tag WHERE object_id = :id`, o)
		assert.NoError(t, err, "deleting object id tags should not fail")

		_, err = db.NamedExecContext(ctx, `DELETE FROM object WHERE id = :id`, o)
		assert.NoError(t, err, "deleting object should not fail")
	}
}

func makeObject(ctx context.Context, db *database.DB, t *testing.T, sourceID int64, mute bool) *Object {
	ev := &event.Event{
		Time:     time.Time{},
		SourceId: sourceID,
		Event: &baseEv.Event{
			Name:       testutils.MakeRandomString(t),
			Mute:       types.Bool{Valid: true, Bool: mute},
			MuteReason: "Just for testing",
			Tags: map[string]string{ // Always generate unique object tags not to produce same object ID!
				"host":    testutils.MakeRandomString(t),
				"service": testutils.MakeRandomString(t),
			},
		},
	}

	o, err := FromEvent(ctx, db, ev)
	require.NoError(t, err)

	return o
}
