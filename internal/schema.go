package internal

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"time"

	"github.com/icinga/icinga-go-library/backoff"
	"github.com/icinga/icinga-go-library/database"
	"github.com/icinga/icinga-go-library/retry"
	"github.com/jmoiron/sqlx"
)

const (
	// The MySQL and PostgreSQL schema versions evolve independently, as each database
	// has its own sequence of schema upgrades, so they can't be merged into a single constant.
	expectedMysqlSchemaVersion    = "v0.2.0-29"
	expectedPostgresSchemaVersion = "v0.2.0-29"

	// mysqlNamedLockKey is the key used for MySQL/MariaDB named locks to prevent concurrent schema imports.
	//
	// NOTE: This must not be changed, as it would break existing deployments that are already using this lock key.
	mysqlNamedLockKey = "icinga_notifications_schema_lock"

	// postgresAdvisoryLockID is the ID used for PostgreSQL advisory locks to prevent concurrent schema imports.
	//
	// The value is a 64-bit integer derived from the FNV-1a hash of the string "icinga_notifications_schema_lock".
	// This ID doesn't need to be unique within the entire PostgreSQL server, the advisory lock applies only on the
	// database the session is connected to and won't conflict with other advisory locks in other databases.
	//
	// NOTE: This must not be changed, as it would break existing deployments that are already using this lock ID.
	postgresAdvisoryLockID int64 = 7312273108300746613
)

// ErrSchemaNotExists indicates that no Icinga Notifications database schema has been imported yet.
var ErrSchemaNotExists = errors.New("no database schema exists")

// AcquireSchemaLock acquires a named lock/advisory lock for the database schema check and import process.
//
// Please do not pass the main database connection to this function, but a dedicated one with a maximum of 1
// open connection, as the lock will be held until the connection is closed. This prevents other processes
// from acquiring the lock while the schema import/check is in progress.
func AcquireSchemaLock(ctx context.Context, db *database.DB) error {
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("could not connect to database: %w", err)
	}

	switch db.DriverName() {
	case database.MySQL, database.PostgreSQL:
		var query string
		var arg any
		if db.DriverName() == database.MySQL {
			query = "SELECT GET_LOCK(?, 5)"
			arg = mysqlNamedLockKey
		} else {
			query = "SELECT pg_try_advisory_lock(?::bigint)"
			arg = postgresAdvisoryLockID
		}

		const attempts = 4
		for attempt := range attempts {
			var lockAcquired bool
			if err := db.GetContext(ctx, &lockAcquired, db.Rebind(query), arg); err != nil {
				return fmt.Errorf("cannot acquire named/advisory lock for schema import/check: %w", err)
			}
			if lockAcquired {
				return nil
			}

			// MySQL/MariaDB already waits for 5 seconds (see query above) before returning false, so we only need
			// the throttle for PostgreSQL, which returns instantly if the lock is already held by another session.
			if attempt < attempts-1 && db.DriverName() == database.PostgreSQL {
				// Use randomized backoff to avoid thundering herd problems if multiple processes are trying
				// to acquire the lock at the same time. This will wait between 100ms and 5s before retrying.
				//
				// #nosec G404 -- It's just for randomized backoff, not for security purposes.
				throttle := time.Duration(100+rand.Intn(5000)) * time.Millisecond
				select {
				case <-time.After(throttle):
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}

		return errors.New("cannot acquire named/advisory lock for schema import/check, another process may be importing the schema")
	default:
		return fmt.Errorf("unsupported database driver %q", db.DriverName())
	}
}

// CheckSchema verifies that the database schema version matches the expected version for the database driver.
//
// Make sure to call AcquireSchemaLock() before calling this function to prevent concurrent schema imports.
func CheckSchema(ctx context.Context, db *database.DB) error {
	var expectedSchemaVersion string
	switch db.DriverName() {
	case database.MySQL:
		expectedSchemaVersion = expectedMysqlSchemaVersion
	case database.PostgreSQL:
		expectedSchemaVersion = expectedPostgresSchemaVersion
	default:
		return fmt.Errorf("unsupported database driver %q", db.DriverName())
	}

	if hasSchemaTable, err := db.HasTable(ctx, "notifications_schema"); err != nil {
		return fmt.Errorf("cannot verify existence of database schema table: %w", err)
	} else if !hasSchemaTable {
		if hasAvailableChannelTypeTable, err := db.HasTable(ctx, "available_channel_type"); err != nil {
			return fmt.Errorf("cannot verify existence of database schema table: %w", err)
		} else if hasAvailableChannelTypeTable {
			// The schema table is missing, but available_channel_type exists. This may be an older schema or a
			// partial MySQL/MariaDB import. We can't verify the schema version, so return a different error than
			// ErrSchemaNotExists to avoid re-importing and causing a duplicate-table error.
			return errors.New(
				"database schema version cannot be verified, please make sure you have applied all database " +
					"migrations after upgrading Icinga Notifications")
		}
		return ErrSchemaNotExists
	}

	var dbResult []string
	err := retry.WithBackoff(
		ctx,
		func(ctx context.Context) error {
			qs := `SELECT version FROM notifications_schema ORDER BY id DESC LIMIT 1`
			if err := db.SelectContext(ctx, &dbResult, qs); err != nil {
				return database.CantPerformQuery(err, qs)
			}
			return nil
		},
		retry.Retryable,
		backoff.DefaultBackoff,
		db.GetDefaultRetrySettings(),
	)
	if err != nil {
		return err
	}

	if len(dbResult) == 0 {
		return errors.New("no database schema version found")
	}

	if actualSchemaVersion := dbResult[0]; actualSchemaVersion != expectedSchemaVersion {
		return fmt.Errorf(
			"unexpected database schema version: %s (expected %s), please make sure you have applied all"+
				" database migrations after upgrading Icinga Notifications",
			actualSchemaVersion, expectedSchemaVersion,
		)
	}

	return nil
}

// ImportSchema imports the initial database schema from schemaDir.
//
// It reads the schema.sql file for the database driver from schemaDir/mysql or schemaDir/pgsql and runs its
// statements (for PostgreSQL in a single tx). On PostgreSQL, a failed import is fully rolled back. On MySQL,
// DDL statements commit implicitly, so a failed import may leave a partially created schema behind.
//
// Make sure to call AcquireSchemaLock() before calling this function to prevent concurrent schema imports.
func ImportSchema(ctx context.Context, db *database.DB, schemaDir string) error {
	var schemaFile string
	switch db.DriverName() {
	case database.MySQL:
		schemaFile = filepath.Join(schemaDir, "mysql", "schema.sql")
	case database.PostgreSQL:
		schemaFile = filepath.Join(schemaDir, "pgsql", "schema.sql")
	default:
		return fmt.Errorf("unsupported database driver %q", db.DriverName())
	}

	schema, err := os.ReadFile(schemaFile) // #nosec G304
	if err != nil {
		return fmt.Errorf("cannot read schema file %q: %w", schemaFile, err)
	}

	switch db.DriverName() {
	case database.PostgreSQL:
		return db.ExecTx(ctx, nil, func(ctx context.Context, tx *sqlx.Tx) error {
			if _, err := tx.ExecContext(ctx, string(schema)); err != nil {
				return fmt.Errorf("cannot execute schema query: %w", err)
			}
			return nil
		})
	default:
		// In MySQL/MariaDB, DDL statements commit implicitly, so we can't run the schema import in a single transaction.
		for _, query := range database.MysqlSplitStatements(string(schema)) {
			if _, err := db.ExecContext(ctx, query); err != nil {
				return fmt.Errorf("cannot execute schema query: %w", database.CantPerformQuery(err, query))
			}
		}

		return nil
	}
}
