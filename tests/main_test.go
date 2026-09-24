package notifications_test

import (
	"github.com/icinga/icinga-testing"
	"github.com/icinga/icinga-testing/services"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/require"
	"testing"
)

var it *icingatesting.IT

func TestMain(m *testing.M) {
	it = icingatesting.NewIT()
	defer it.Cleanup()

	m.Run()
}

func getDatabase(t testing.TB) services.RelationalDatabase {
	rdb := getEmptyDatabase(t)
	rdb.ImportIcingaNotificationsSchema()

	db, err := sqlx.Open(rdb.Driver(), rdb.DSN())
	require.NoError(t, err, "SQL database open")
	defer func() { _ = db.Close() }()

	_, err = db.Exec(`
		INSERT INTO source (id, type, name, listener_password_hash)
		VALUES (1, 'icinga2', 'Icinga 2', '$2y$10$QU8bJ7cpW1SmoVQ/RndX5O2J5L1PJF7NZ2dlIW7Rv3zUEcbUFg3z2')`)
	require.NoError(t, err, "populating source table failed")

	return rdb
}

func getEmptyDatabase(t testing.TB) services.RelationalDatabase {
	// Currently, PostgreSQL is the only supported database backend.
	return it.PostgresqlDatabaseT(t)
}
