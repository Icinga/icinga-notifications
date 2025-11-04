package testutils

import (
	"context"
	"crypto/rand"
	"fmt"
	"github.com/creasty/defaults"
	"github.com/icinga/icinga-go-library/database"
	"github.com/icinga/icinga-go-library/logging"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
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
func GetTestDB(ctx context.Context, t *testing.T) *database.DB {
	c := &database.Config{}
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

	db, err := database.NewDbFromConfig(c, logging.NewLogger(zaptest.NewLogger(t).Sugar(), time.Hour), database.RetryConnectorCallbacks{})
	require.NoError(t, err, "connecting to database should not fail")
	require.NoError(t, db.PingContext(ctx), "pinging the database should not fail")

	return db
}

// MakeRandomString returns a 20 byte random hex string.
func MakeRandomString(t *testing.T) string {
	buf := make([]byte, 20)
	_, err := rand.Read(buf)
	require.NoError(t, err, "failed to generate random string")

	return fmt.Sprintf("%x", buf)
}

// NewTestLogging creates a new logging instance for testing purposes.
//
// The logger uses zaptest to integrate with the testing.T instance, allowing log output to be
// captured and displayed in test results. The logging level is set to Debug to provide detailed
// output during tests.
func NewTestLogging(t *testing.T) *logging.Logging {
	return logging.NewLoggingWithFactory(
		"testing",
		zap.DebugLevel,
		time.Hour,
		func(level zap.AtomicLevel) zapcore.Core {
			return zaptest.NewLogger(t, zaptest.Level(level.Level())).Core()
		},
	)
}
