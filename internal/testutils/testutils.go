package testutils

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/icinga/icinga-go-library/database"
	"github.com/icinga/icinga-go-library/logging"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// GetTestDB returns a database connection for testing purposes.
//
// It requires the environment variable "ICINGA_NOTIFICATIONS_DATABASE_TYPE" to be set, otherwise it will
// skip the test. The function validates the provided database configuration, establishes a connection,
// and pings the database to ensure it's reachable.
func GetTestDB(ctx context.Context, t *testing.T, conf *database.Config) *database.DB {
	if _, ok := os.LookupEnv("ICINGA_NOTIFICATIONS_DATABASE_TYPE"); !ok {
		t.Skipf("Environment %q not set, skipping test!", "ICINGA_NOTIFICATIONS_DATABASE_TYPE")
	}
	require.NoError(t, conf.Validate(), "database config validation should not fail")

	db, err := database.NewDbFromConfig(conf, logging.NewLogger(zaptest.NewLogger(t).Sugar(), time.Hour), database.RetryConnectorCallbacks{})
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
