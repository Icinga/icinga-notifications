package event

import (
	"context"
	"crypto/sha256"
	"testing"
	"time"

	"github.com/icinga/icinga-go-library/database"
	baseEv "github.com/icinga/icinga-go-library/notifications/event"
	"github.com/icinga/icinga-go-library/types"
	"github.com/icinga/icinga-notifications/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// randomObjectID returns an arbitrary 32-byte object ID, matching the shape produced by object.ID.
//
// event_queue.object_id has no foreign key, so any 32-byte value is a valid stand-in for these tests
// without pulling in the internal/object package, which would create an import cycle with this package.
func randomObjectID(t *testing.T) types.Binary {
	sum := sha256.Sum256([]byte(testutils.MakeRandomString(t)))
	return sum[:]
}

// cleanupQueueEntry deletes the event_queue row with the given ID, so that enqueued test events don't accumulate
// in the shared test database across runs. event_queue has no rows referencing it via foreign key.
func cleanupQueueEntry(t *testing.T, db *database.DB, id types.UUID) {
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := db.ExecContext(ctx, db.Rebind(`DELETE FROM event_queue WHERE id = ?`), id)
		assert.NoError(t, err, "cleaning up event_queue entry should not fail")
	})
}

func TestEnqueue(t *testing.T) {
	db := testutils.GetTestDB(t.Context(), t)

	t.Run("Populates Queue ID From Event ID", func(t *testing.T) {
		ev, err := CreateEvent(1, baseEv.Event{
			Name:    testutils.MakeRandomString(t),
			Message: "some message",
		})
		require.NoError(t, err)
		cleanupQueueEntry(t, db, ev.ID)

		objectID := randomObjectID(t)

		require.NoError(t, Enqueue(t.Context(), db, &ev, objectID))

		var q Queue
		require.NoError(t, db.GetContext(t.Context(), &q, db.Rebind(`SELECT * FROM event_queue WHERE id = ?`), ev.ID))

		assert.Equal(t, ev.ID, q.ID)
	})

	t.Run("Re-Enqueueing Identical Event Does Not Error", func(t *testing.T) {
		ev, err := CreateEvent(1, baseEv.Event{
			Name:    testutils.MakeRandomString(t),
			Message: "some message",
		})
		require.NoError(t, err)
		cleanupQueueEntry(t, db, ev.ID)

		objectID := randomObjectID(t)

		require.NoError(t, Enqueue(t.Context(), db, &ev, objectID))
		// Enqueuing the exact same event (same content, thus same generated ID) a second time must not fail,
		// as event_queue relies on an INSERT IGNORE to deduplicate resubmissions of the same event.
		require.NoError(t, Enqueue(t.Context(), db, &ev, objectID))

		var count int
		require.NoError(t, db.GetContext(t.Context(), &count, db.Rebind(`SELECT COUNT(*) FROM event_queue WHERE id = ?`), ev.ID))
		assert.Equal(t, 1, count, "the event must only be stored once")
	})
}
