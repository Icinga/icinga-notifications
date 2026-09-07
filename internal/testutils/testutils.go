package testutils

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/icinga/icinga-go-library/config"
	"github.com/icinga/icinga-go-library/database"
	"github.com/icinga/icinga-go-library/logging"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"
)

// DBCleaner deletes only the rows a test created from a shared test database, instead of truncating whole tables,
// so that tests running concurrently against the same database don't clobber each other's data.
//
// Register conditions for the registered tables with Add or AddLazy, and then call Clean (typically via t.Cleanup)
// to delete all rows matching those conditions. Clean will only ever delete rows from tables that have registered
// conditions, and won't touch any other tables, so it's safe to use in parallel tests that share a database.
type DBCleaner struct {
	tables                []string
	conditions            map[string][]string
	lazyTableConditioners map[string]func(context.Context) string

	mu sync.Mutex
}

// NewDBCleaner returns a DBCleaner that will only ever touch the given tables, deleted in the given order.
//
// The order must respect foreign key constraints, i.e. children before their parents, since Clean will
// perform plain filtered DELETEs rather than relying on cascading deletes.
func NewDBCleaner(tables ...string) *DBCleaner {
	return &DBCleaner{
		tables:                tables,
		conditions:            make(map[string][]string),
		lazyTableConditioners: make(map[string]func(context.Context) string),
	}
}

// Add registers a SQL WHERE condition for rows this test owns in the given table.
//
// The condition is used verbatim without any adjustments. You have to build it with fmt.Sprintf and
// embed values directly into your condition, since the cleaner doesn't support parameterized queries.
// Multiple conditions added for the same table are OR-ed together.
func (c *DBCleaner) Add(table, condition string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.conditions[table] = append(c.conditions[table], condition)
}

// AddLazy registers a SQL WHERE condition for the given table, but the condition is only evaluated when Clean is called.
//
// This is useful if the condition depends on values that are only known at the time of cleaning, or you just
// want to defer whatever logic is needed to build the condition until the last possible moment. f is called
// with the context passed to Clean, and should return the actual SQL WHERE condition string. If it returns
// an empty string, Clean will skip it if no other conditions were registered for it.
func (c *DBCleaner) AddLazy(table string, f func(context.Context) string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lazyTableConditioners[table] = f
}

// Clean deletes all registered tables, one by one, in the order given to NewDBCleaner.
//
// It is safe to call concurrently from parallel subtests, but it will only delete rows from tables
// that have registered conditions. If no conditions are registered for a table, it will be skipped.
func (c *DBCleaner) Clean(ctx context.Context, t *testing.T, db *database.DB) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Evaluate lazy conditions and add them to the conditions map before executing the DELETE statements.
	for table, conditionFunc := range c.lazyTableConditioners {
		if condition := conditionFunc(ctx); condition != "" {
			c.conditions[table] = append(c.conditions[table], condition)
		}
	}

	for _, table := range c.tables {
		tableConditions, ok := c.conditions[table]
		if !ok || len(tableConditions) == 0 {
			continue
		}
		_, err := db.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %q WHERE %s`, table, strings.Join(tableConditions, " OR ")))
		require.NoError(t, err)
	}
}

// SkipTestIfDBConfigIsMissing skips the calling test if the required test database environment variable is not set.
//
// Call this before daemon.InjectTestConfig, not from inside its callback: InjectTestConfig only ever invokes
// its callback once per test binary (it guards a package-wide singleton), so if the first test to reach it is
// the one that skips, every later test calling InjectTestConfig silently gets an empty, never-loaded config
// instead of being skipped itself. Checking the environment independently in each test, before touching the
// singleton, ensures every test in the package skips consistently when the database isn't configured.
func SkipTestIfDBConfigIsMissing(t *testing.T) {
	if _, ok := os.LookupEnv("ICINGA_NOTIFICATIONS_DATABASE_TYPE"); !ok {
		t.Skipf("Environment %q not set, skipping test!", "ICINGA_NOTIFICATIONS_DATABASE_TYPE")
	}
}

// LoadTestConfig loads the configuration from environment variables into the provided config struct for testing
// purposes, populating it with values from the environment variables prefixed with "ICINGA_NOTIFICATIONS_".
//
// Callers must first ensure the test database is configured, e.g. via SkipTestIfDBConfigIsMissing.
func LoadTestConfig[T config.Validator](t *testing.T, configFile T) {
	require.NoError(t, config.FromEnv(configFile, config.EnvOptions{Prefix: "ICINGA_NOTIFICATIONS_"}))
}

// GetTestDB returns a database.DB instance for testing purposes.
//
// It connects to the database using the provided configuration and ensures that the connection is successful.
// If the connection or ping fails, it will fail the test.
func GetTestDB(ctx context.Context, t *testing.T, dbConfig *database.Config) *database.DB {
	db, err := database.NewDbFromConfig(dbConfig, logging.NewLogger(zaptest.NewLogger(t).Sugar(), time.Hour), database.RetryConnectorCallbacks{})
	require.NoError(t, err, "connecting to database should not fail")
	require.NoError(t, db.PingContext(ctx), "pinging the database should not fail")

	return db
}

// GetTestLogging returns a logging.Logging instance for testing purposes.
//
// It sets the logging level to Debug and uses a zaptest logger to capture logs during tests.
func GetTestLogging(t *testing.T) *logging.Logging {
	return logging.NewLoggingWithFactory("testing", zapcore.DebugLevel, time.Second, func(level zap.AtomicLevel) zapcore.Core {
		return zaptest.NewLogger(t, zaptest.Level(level.Level())).Core()
	})
}

// MakeRandomString returns a 20 byte random hex string.
func MakeRandomString(t *testing.T) string {
	buf := make([]byte, 20)
	_, err := rand.Read(buf)
	require.NoError(t, err, "failed to generate random string")

	return fmt.Sprintf("%x", buf)
}
