package event

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/icinga/icinga-go-library/database"
	baseEv "github.com/icinga/icinga-go-library/notifications/event"
	"github.com/icinga/icinga-go-library/types"
	"github.com/icinga/icinga-go-library/utils"
	"github.com/icinga/icinga-notifications/internal/daemon"
	"github.com/icinga/icinga-notifications/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQueue(t *testing.T) {
	t.Parallel()

	daemon.InjectTestConfig(func(configFile *daemon.ConfigFile) { testutils.LoadTestConfig(t, configFile) })

	db := testutils.GetTestDB(t.Context(), t, &daemon.Config().Database)
	logs := testutils.GetTestLogging(t)

	t.Run("Enqueue", func(t *testing.T) {
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			cleanupDB(ctx, t, db)
		})

		assertJobCount := func(expected int) {
			var count int
			err := db.GetContext(t.Context(), &count, "SELECT COUNT(*) FROM job_queue")
			require.NoError(t, err)
			require.Equal(t, expected, count)
		}
		assertJobCount(0)

		require.NoError(t, Enqueue(t.Context(), db, makeEvent(t)))
		assertJobCount(1)

		ev := makeEvent(t)
		require.NoError(t, Enqueue(t.Context(), db, ev))
		assertJobCount(2)

		require.NoError(t, Enqueue(t.Context(), db, ev))
		assertJobCount(2) // Duplicate event should not increase count

		require.NoError(t, Enqueue(t.Context(), db, makeEvent(t)))
		assertJobCount(3) // Different event with same ID but different body should be enqueued.

		var wg sync.WaitGroup // Test concurrent enqueuing
		for range 16 {
			wg.Go(func() {
				for range 10 {
					require.NoError(t, Enqueue(t.Context(), db, makeEvent(t)))
				}
			})
		}
		wg.Wait()
		assertJobCount(163) // 3 from before + 16*10 = 163
	})

	t.Run("Dequeue", func(t *testing.T) {
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			cleanupDB(ctx, t, db)
		})

		producerCtx, producerCtxCancel := context.WithCancel(t.Context())
		defer producerCtxCancel()
		go func() {
			for producerCtx.Err() == nil {
				err := Enqueue(producerCtx, db, makeEvent(t))
				if producerCtx.Err() == nil {
					require.NoError(t, err)
				}
			}
		}()

		consumerCtx, consumerCtxCancel := context.WithCancel(t.Context())
		defer consumerCtxCancel()

		successfulEventJobs := make(chan *Event)
		failedEventJobs := make(chan *Event)
		var counter atomic.Int64

		cbs := QueueCallbacks{
			GenObjectID: func(m map[string]string) types.Binary {
				// This mimics the behavior of the actual implementation of object.ID() since we
				// can't use it directly here due to cyclic import issues.
				h := sha256.New()
				for k, v := range utils.IterateOrderedMap(m) {
					assert.NoError(t, binary.Write(h, binary.BigEndian, []byte(k)))
					assert.NoError(t, binary.Write(h, binary.BigEndian, []byte(v)))
				}
				return h.Sum(nil)
			},
			ProcessEvent: func(ctx context.Context, ev *Event) error {
				if counter.Add(1)%2 == 0 {
					select {
					case successfulEventJobs <- ev:
						return nil
					case <-ctx.Done():
						return ctx.Err()
					}
				}

				select {
				case failedEventJobs <- ev:
					return fmt.Errorf("simulated processing error for queue %s", makeQueueID(t, ev))
				case <-ctx.Done():
					return ctx.Err()
				}
			},
		}

		channelLogger := logs.GetChildLogger("job-queue")
		var wg sync.WaitGroup
		wg.Go(func() { assert.ErrorIs(t, ProcessQueue(consumerCtx, db, channelLogger, cbs), context.Canceled) })
		wg.Go(func() { assert.ErrorIs(t, ProcessQueue(consumerCtx, db, channelLogger, cbs), context.Canceled) })

		successEvM := make(map[string]struct{})
		failEvM := make(map[string]struct{})

		wg.Go(func() {
			ticker := time.NewTicker(time.Second)

			for {
				select {
				case <-ticker.C:
					if producerCtx.Err() == nil {
						continue
					}
					t.Logf("Processed %d events so far. Successful: %d, Failed: %d", counter.Load(), len(successEvM), len(failEvM))
					consumerCtxCancel() // Stop processing queue after 1 second of inactivity
					return

				case ev := <-successfulEventJobs:
					qID := makeQueueID(t, ev)
					_, successExits := successEvM[qID]
					assert.False(t, successExits, "event %s processed successfully more than once", qID)

					_, failExits := failEvM[qID]
					assert.False(t, failExits, "event %s processed successfully after failing", qID)

					successEvM[qID] = struct{}{}
					ticker.Reset(time.Second)

				case ev := <-failedEventJobs:
					qID := makeQueueID(t, ev)
					_, successExits := successEvM[qID]
					assert.False(t, successExits, "event %s failed after being processed successfully", qID)

					_, failExits := failEvM[qID]
					assert.False(t, failExits, "event %s failed more than once", qID)

					failEvM[qID] = struct{}{}
					ticker.Reset(time.Second)
				}
			}
		})

		time.Sleep(2 * time.Second)
		producerCtxCancel() // Stop producing events.

		wg.Wait() // Wait for both consumer goroutines to finish.

		assert.Greater(t, counter.Load(), int64(10))
		assert.Greater(t, len(successEvM), 10)
		assert.Greater(t, len(failEvM), 10)

		lockedObjectsStmt := `SELECT COUNT(*) FROM job_processing_lock`
		var lockedObjectsCount int
		err := db.GetContext(t.Context(), &lockedObjectsCount, lockedObjectsStmt)
		require.NoError(t, err)
		assert.Equal(t, 0, lockedObjectsCount, "locked objects should be released after processing")

		failedJobsStmt := `SELECT COUNT(*) FROM job_queue WHERE state = ` + fmt.Sprintf("%d", QueueStateError)
		var failedJobsCount int
		err = db.GetContext(t.Context(), &failedJobsCount, failedJobsStmt)
		require.NoError(t, err)
		assert.Len(t, failEvM, failedJobsCount)

		successfulJobsStmt := `SELECT COUNT(*) FROM job_queue WHERE state = ` + fmt.Sprintf("%d", QueueStateDone)
		var successfulJobsCount int
		err = db.GetContext(t.Context(), &successfulJobsCount, successfulJobsStmt)
		require.NoError(t, err)
		assert.Len(t, successEvM, successfulJobsCount)

		assert.Equal(t, counter.Load(), int64(successfulJobsCount+failedJobsCount))
	})
}

// makeQueueID generates a unique queue ID for the given event by hashing its JSON representation.
// This mimics the behavior of the actual implementation in Enqueue.
func makeQueueID(t *testing.T, ev *Event) string {
	h := sha256.New()
	assert.NoError(t, json.NewEncoder(h).Encode(ev))
	return hex.EncodeToString(h.Sum(nil))
}

// makeEvent creates a new Event and returns it.
func makeEvent(t *testing.T) *Event {
	return &Event{
		Time: time.Now().Truncate(time.Second),
		Event: baseEv.Event{
			ID:   "test-event-id",
			Name: "Test Event",
			Tags: map[string]string{
				"host":    testutils.MakeRandomString(t),
				"service": testutils.MakeRandomString(t),
			},
			Severity: baseEv.SeverityCrit,
			Message:  "You're gonna have a bad time.",
		},
	}
}

// cleanupDB removes all test data from the database.
func cleanupDB(ctx context.Context, t *testing.T, db *database.DB) {
	switch db.DriverName() {
	case database.PostgreSQL, database.MySQL:
		tables := []string{
			"job_processing_lock",
			"job_queue",
		}
		for _, table := range tables {
			_, err := db.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s", table))
			require.NoError(t, err)
		}
	}
}
