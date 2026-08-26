package testutils

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
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

// LoadTestConfig loads the configuration from environment variables into the provided config struct for testing purposes.
//
// It requires the environment variable "ICINGA_NOTIFICATIONS_DATABASE_TYPE" to be set, otherwise it will
// skip the test. The function validates the provided configuration struct and populates it with values
// from the environment variables prefixed with "ICINGA_NOTIFICATIONS_".
func LoadTestConfig[T config.Validator](t *testing.T, configFile T) {
	if _, ok := os.LookupEnv("ICINGA_NOTIFICATIONS_DATABASE_TYPE"); !ok {
		t.Skipf("Environment %q not set, skipping test!", "ICINGA_NOTIFICATIONS_DATABASE_TYPE")
	}
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
