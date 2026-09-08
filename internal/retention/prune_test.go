package retention

import (
	"context"
	"crypto/rand"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/icinga/icinga-go-library/database"
	"github.com/icinga/icinga-go-library/types"
	"github.com/icinga/icinga-notifications/internal/daemon"
	"github.com/icinga/icinga-notifications/internal/testutils"
	"github.com/stretchr/testify/require"
)

func TestPruners(t *testing.T) {
	t.Parallel()

	testutils.SkipTestIfDBConfigIsMissing(t)
	daemon.InjectTestConfig(func(configFile *daemon.ConfigFile) { testutils.LoadTestConfig(t, configFile) })

	db := testutils.GetTestDB(t.Context(), t, &daemon.Config().Database)
	logs := testutils.GetTestLogging(t)

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := db.ExecContext(ctx, `DROP TABLE IF EXISTS retention_stats,retention_history,retention`)
		require.NoError(t, err)
	})
	prepareRetentionTables(t, db)

	// cleanupRemainingRetentionRecords is a helper function to clean up any remaining records in the retention
	// and its referencing tables after each test run. This is necessary because each test run needs to start with
	// a clean state to perform its specific assertions correctly.
	cleanupRemainingRetentionRecords := func() {
		pruner := &TimeBoundPruner{
			Table:      "retention",
			PKorFK:     "binary_id",
			TimeColumn: "updated_at",
			Referrers: []ReferencingRowPruner{
				{Table: "retention_stats", PKorFK: "binary_id"},
				{Table: "retention_history", PKorFK: "binary_id"},
			},
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		deleted, err := pruner.Exec(ctx, db, logs.GetLogger(), types.UnixMilli(time.Now()), 10)
		require.NoError(t, err)
		require.NotZero(t, deleted)
	}

	t.Run("TimeBoundPruner", func(t *testing.T) {
		testCases := []struct {
			name   string
			pruner Pruner
		}{
			{
				name: "Binary",
				pruner: &TimeBoundPruner{
					Table:      "retention",
					PKorFK:     "binary_id",
					TimeColumn: "updated_at",
					Referrers: []ReferencingRowPruner{
						{Table: "retention_stats", PKorFK: "binary_id"},
						{Table: "retention_history", PKorFK: "binary_id"},
					},
				},
			},
			{
				name: "UUID",
				pruner: &TimeBoundPruner{
					Table:        "retention",
					PKorFK:       "uuid_id",
					TimeColumn:   "updated_at",
					IsPKorFKUUID: true,
					Referrers: []ReferencingRowPruner{
						{Table: "retention_stats", PKorFK: "uuid_id"},
						{Table: "retention_history", PKorFK: "uuid_id"},
					},
				},
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				olderThan := time.Now().Add(-time.Hour)
				updateTime := time.Now().Add(-2 * time.Hour)

				seedRetentionRecords(t, db, withTime(updateTime), withCount(10), withRetentionStats(), withRetentionHistory())
				seedRetentionRecords(t, db, withTime(updateTime), withCount(5), withRetentionStats())
				seedRetentionRecords(t, db, withTime(updateTime), withCount(5), withRetentionHistory())

				noopCtx, noopCancel := context.WithTimeout(t.Context(), 2*time.Second)
				defer noopCancel()
				deleted, err := tc.pruner.Exec(noopCtx, db, logs.GetLogger(), types.UnixMilli(time.Now().Add(-5*time.Hour)), 10)
				require.NoError(t, err)
				require.Equal(t, uint64(0), deleted, "expected 0, but got %d", deleted)

				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				deleted, err = tc.pruner.Exec(ctx, db, logs.GetLogger(), types.UnixMilli(olderThan), 5)
				require.NoError(t, err)
				require.Equal(t, uint64(20), deleted, "expected 20, but got %d", deleted)

				tbp, ok := tc.pruner.(*TimeBoundPruner)
				require.True(t, ok)
				tbp.Referrers = nil // Reset the referrers to nil to target the other branch of the Exec method.

				seedRetentionRecords(t, db, withTime(updateTime), withCount(10)) // Without seeding the reference tables.

				noReferencesCtx, noReferencesCancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer noReferencesCancel()
				deleted, err = tbp.Exec(noReferencesCtx, db, logs.GetLogger(), types.UnixMilli(olderThan), 5)
				require.NoError(t, err)
				require.Equal(t, uint64(10), deleted, "expected 10, but got %d", deleted)
			})
		}
	})

	t.Run("ResetPruner", func(t *testing.T) {
		testCases := []struct {
			name   string
			pruner Pruner
		}{
			{
				name: "Binary",
				pruner: &ResetPruner{
					Table:      "retention",
					PKorFK:     "binary_id",
					TimeColumn: "updated_at",
					Referrers: []ReferencingRowPruner{
						{Table: "retention_stats", PKorFK: "binary_id"},
						{Table: "retention_history", PKorFK: "binary_id"},
					},
					ExtraCondition:   `state = 1`,
					UpdateExpression: `state = 2`,
				},
			},
			{
				name: "UUID",
				pruner: &ResetPruner{
					Table:        "retention",
					PKorFK:       "uuid_id",
					TimeColumn:   "updated_at",
					IsPKorFKUUID: true,
					Referrers: []ReferencingRowPruner{
						{Table: "retention_stats", PKorFK: "uuid_id"},
						{Table: "retention_history", PKorFK: "uuid_id"},
					},
					ExtraCondition:   `state = 1`,
					UpdateExpression: `state = 2`,
				},
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				t.Cleanup(cleanupRemainingRetentionRecords)

				updateTime := time.Now().Add(-2 * time.Hour)
				seedRetentionRecords(t, db, withTime(updateTime), withState(1), withCount(10), withRetentionStats(), withRetentionHistory())
				seedRetentionRecords(t, db, withTime(updateTime), withState(1), withCount(5), withRetentionStats())
				seedRetentionRecords(t, db, withTime(updateTime), withState(1), withCount(5), withRetentionHistory())

				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				updated, err := tc.pruner.Exec(ctx, db, logs.GetLogger(), types.UnixMilli(time.Now()), 5)
				require.NoError(t, err)
				require.Equal(t, uint64(20), updated, "expected 20, but got %d", updated)

				// The reset pruner should have deleted all the referencing rows from the child tables,
				// so we can check that they are really gone.
				var count int
				require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM retention_stats`).Scan(&count))
				require.Equal(t, 0, count)

				require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM retention_history`).Scan(&count))
				require.Equal(t, 0, count)

				require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM retention WHERE state = 2`).Scan(&count))
				require.Equal(t, 20, count) // Should still be 20 rows in the retention table.

				rp, ok := tc.pruner.(*ResetPruner)
				require.True(t, ok)
				rp.Referrers = nil // Reset the referrers to nil to target the other branch of the Exec method.

				seedRetentionRecords(t, db, withState(1), withCount(10)) // Without seeding the reference tables.

				noReferencesCtx, noReferencesCancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer noReferencesCancel()
				updated, err = rp.Exec(noReferencesCtx, db, logs.GetLogger(), types.UnixMilli(time.Now()), 5)
				require.NoError(t, err)
				require.Equal(t, uint64(10), updated, "expected 10, but got %d", updated)
			})
		}
	})

	t.Run("OrphanRowPruner", func(t *testing.T) {
		// Seed 10 rows with retention_stats and retention_history, to simulate a non-orphaned state.
		seedRetentionRecords(t, db, withTime(time.Now().Add(-time.Hour)), withCount(10), withRetentionStats(), withRetentionHistory())
		// Remove the above 10 non-orphan rows at the end of the test to avoid collisions with other tests.
		t.Cleanup(cleanupRemainingRetentionRecords)

		testCases := []struct {
			name   string
			pruner Pruner
		}{
			{
				name: "Binary",
				pruner: &OrphanRowPruner{
					Table:  "retention",
					PKorFK: "binary_id",
					// If rows in the retention table are referenced by rows in the retention_stats table,
					// they are not considered orphaned, and should not be deleted. Otherwise, it should
					// delete these and any rows in the retention_history table that reference them.
					ReferencedBy: []ReferencingRelation{{Table: "retention_stats", FK: "binary_id"}},
					Referrers:    []ReferencingRowPruner{{Table: "retention_history", PKorFK: "binary_id"}},
				},
			},
			{
				name: "UUID",
				pruner: &OrphanRowPruner{
					Table:        "retention",
					PKorFK:       "uuid_id",
					IsPKorFKUUID: true,
					ReferencedBy: []ReferencingRelation{{Table: "retention_stats", FK: "uuid_id"}},
					Referrers:    []ReferencingRowPruner{{Table: "retention_history", PKorFK: "uuid_id"}},
				},
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				// We are going to use the "retention_stats" as the referencing table, so we don't seed it for the next 20 rows.
				seedRetentionRecords(t, db, withCount(10), withRetentionHistory())
				seedRetentionRecords(t, db, withCount(10))

				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				deleted, err := tc.pruner.Exec(ctx, db, logs.GetLogger(), types.UnixMilli(time.Now()), 5)
				require.NoError(t, err)
				require.Equal(t, uint64(20), deleted, "expected 20, but got %d", deleted)

				orp, ok := tc.pruner.(*OrphanRowPruner)
				require.True(t, ok)
				orp.Referrers = nil // Reset the referrers to nil to target the other branch of the Exec method.

				seedRetentionRecords(t, db, withCount(10)) // Without seeding both of the reference tables.

				noReferencesCtx, noReferencesCancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer noReferencesCancel()
				deleted, err = orp.Exec(noReferencesCtx, db, logs.GetLogger(), types.UnixMilli(time.Now()), 5)
				require.NoError(t, err)
				require.Equal(t, uint64(10), deleted, "expected 10, but got %d", deleted)
			})
		}
	})
}

// prepareRetentionTables creates the retention tables in the database for testing purposes.
//
// This is necessary because we don't want to cause any collisions with concurrent tests that
// might be running against the same database.
func prepareRetentionTables(t *testing.T, db *database.DB) {
	switch db.DriverName() {
	case database.PostgreSQL:
		_, err := db.ExecContext(t.Context(), `
			CREATE TABLE IF NOT EXISTS retention (
				binary_id bytea NOT NULL,
				uuid_id uuid NOT NULL,
				updated_at bigint NOT NULL,
				state int NOT NULL,

				CONSTRAINT uk_retention_binary_id UNIQUE (binary_id),
				CONSTRAINT uk_retention_uuid_id UNIQUE (uuid_id)
			);

			CREATE TABLE retention_stats (
				binary_id bytea,
				uuid_id uuid,

				CONSTRAINT fk_retention_stats_binary_id FOREIGN KEY (binary_id) REFERENCES retention(binary_id),
				CONSTRAINT fk_retention_stats_uuid_id FOREIGN KEY (uuid_id) REFERENCES retention(uuid_id)
			);

			CREATE TABLE retention_history (
				binary_id bytea,
				uuid_id uuid,

				CONSTRAINT fk_retention_history_binary_id FOREIGN KEY (binary_id) REFERENCES retention(binary_id),
				CONSTRAINT fk_retention_history_uuid_id FOREIGN KEY (uuid_id) REFERENCES retention(uuid_id)
			);
		`)
		require.NoError(t, err)
	default:
		// We don't have a fully featured MySQL client that supports semicolon separated statements,
		// so we need to execute each statement separately.
		_, err := db.ExecContext(t.Context(), `
			CREATE TABLE retention (
				binary_id binary(20) NOT NULL,
				uuid_id binary(16) NOT NULL,
				updated_at bigint NOT NULL,
				state int NOT NULL,

				CONSTRAINT uk_retention_binary_id UNIQUE (binary_id),
				CONSTRAINT uk_retention_uuid_id UNIQUE (uuid_id)
			) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
		`)
		require.NoError(t, err)
		_, err = db.ExecContext(t.Context(), `
			CREATE TABLE retention_stats (
				binary_id binary(20),
				uuid_id binary(16),

				CONSTRAINT fk_retention_stats_binary_id FOREIGN KEY (binary_id) REFERENCES retention(binary_id),
				CONSTRAINT fk_retention_stats_uuid_id FOREIGN KEY (uuid_id) REFERENCES retention(uuid_id)
			) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
		`)
		require.NoError(t, err)

		_, err = db.ExecContext(t.Context(), `
			CREATE TABLE retention_history (
				binary_id binary(20),
				uuid_id binary(16),

				CONSTRAINT fk_retention_history_binary_id FOREIGN KEY (binary_id) REFERENCES retention(binary_id),
				CONSTRAINT fk_retention_history_uuid_id FOREIGN KEY (uuid_id) REFERENCES retention(uuid_id)
			) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
		`)
		require.NoError(t, err)
	}
}

// seedOption is a functional option type for configuring the seeding of retention records.
type seedOption func(options *seedOptions)

// seedOptions holds the configuration for seeding retention records.
type seedOptions struct {
	UpdateTime           time.Time
	State                int
	Count                uint64
	SeedRetentionStats   bool
	SeedRetentionHistory bool
}

// seedRetentionRecords seeds the retention table and its referencing tables with test data based on the provided options.
func seedRetentionRecords(t *testing.T, db *database.DB, opts ...seedOption) {
	sopts := new(seedOptions)
	for _, opt := range opts {
		opt(sopts)
	}
	if sopts.Count == 0 {
		sopts.Count = 1
	}
	if sopts.UpdateTime.IsZero() {
		sopts.UpdateTime = time.Now()
	}

	for range sopts.Count {
		var bID types.Binary = make([]byte, 20)
		_, err := rand.Read(bID)
		require.NoError(t, err)

		uuidID := types.MakeUUID(uuid.New())
		_, err = db.ExecContext(t.Context(), db.Rebind(`
			INSERT INTO retention (binary_id, uuid_id, updated_at, state)
			VALUES (?, ?, ?, ?)
		`), bID, uuidID, sopts.UpdateTime.UnixMilli(), sopts.State)
		require.NoError(t, err)

		if sopts.SeedRetentionStats {
			_, err := db.ExecContext(t.Context(), db.Rebind(`
				INSERT INTO retention_stats (binary_id, uuid_id)
				VALUES (?, ?)
			`), bID, uuidID)
			require.NoError(t, err)
		}

		if sopts.SeedRetentionHistory {
			_, err := db.ExecContext(t.Context(), db.Rebind(`
				INSERT INTO retention_history (binary_id, uuid_id)
				VALUES (?, ?)
			`), bID, uuidID)
			require.NoError(t, err)
		}
	}
}

func withTime(updateTime time.Time) seedOption {
	return func(o *seedOptions) { o.UpdateTime = updateTime }
}
func withState(state int) seedOption    { return func(o *seedOptions) { o.State = state } }
func withCount(count uint64) seedOption { return func(o *seedOptions) { o.Count = count } }
func withRetentionStats() seedOption    { return func(o *seedOptions) { o.SeedRetentionStats = true } }
func withRetentionHistory() seedOption  { return func(o *seedOptions) { o.SeedRetentionHistory = true } }
