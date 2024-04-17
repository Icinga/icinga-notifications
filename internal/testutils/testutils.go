package testutils

import (
	"context"
	"github.com/creasty/defaults"
	"github.com/icinga/icingadb/pkg/config"
	"github.com/icinga/icingadb/pkg/icingadb"
	"github.com/icinga/icingadb/pkg/logging"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// GetTestDB retrieves the database config from env variables, opens a new database and returns it.
//
// The test suite will be skipped if no environment variable is set, otherwise fails fatally when
// invalid configurations are specified.
func GetTestDB(ctx context.Context, t *testing.T) *icingadb.DB {
	c := &config.Database{}
	require.NoError(t, defaults.Set(c), "applying config default should not fail")

	if v, ok := os.LookupEnv("NOTIFICATIONS_TESTS_DB_TYPE"); ok {
		c.Type = strings.ToLower(v)
	} else {
		t.Skipf("Environment %q not set, skipping test!", "NOTIFICATIONS_TESTS_DB_TYPE")
	}

	if v, ok := os.LookupEnv("NOTIFICATIONS_TESTS_DB"); ok {
		c.Database = v
	}
	if v, ok := os.LookupEnv("NOTIFICATIONS_TESTS_DB_USER"); ok {
		c.User = v
	}
	if v, ok := os.LookupEnv("NOTIFICATIONS_TESTS_DB_PASSWORD"); ok {
		c.Password = v
	}
	if v, ok := os.LookupEnv("NOTIFICATIONS_TESTS_DB_HOST"); ok {
		c.Host = v
	}
	if v, ok := os.LookupEnv("NOTIFICATIONS_TESTS_DB_PORT"); ok {
		port, err := strconv.Atoi(v)
		require.NoError(t, err, "invalid port provided")

		c.Port = port
	}

	require.NoError(t, c.Validate(), "database config validation should not fail")

	db, err := c.Open(logging.NewLogger(zaptest.NewLogger(t).Sugar(), time.Hour))
	require.NoError(t, err, "connecting to database should not fail")
	require.NoError(t, db.PingContext(ctx), "pinging the database should not fail")

	return db
}
