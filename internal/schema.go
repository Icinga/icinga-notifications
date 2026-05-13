package internal

import (
	"context"
	"errors"
	"fmt"

	"github.com/icinga/icinga-go-library/backoff"
	"github.com/icinga/icinga-go-library/database"
	"github.com/icinga/icinga-go-library/retry"
)

const (
	// Both MySQL and PostgreSQL schema versions are currently the same, but they can
	// evolve independently in the future, so can't be merged into a single constant.
	expectedMysqlSchemaVersion    = "v1.0"
	expectedPostgresSchemaVersion = "v1.0"
)

// CheckSchema verifies that the database schema version matches the expected version for the database driver.
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
		return errors.New(
			"notifications_schema table does not exist, please make sure you have applied all" +
				" database migrations after upgrading Icinga Notifications",
		)
	}

	var dbResult []string
	err := retry.WithBackoff(
		ctx,
		func(ctx context.Context) error {
			qs := `SELECT version FROM notifications_schema ORDER BY timestamp DESC LIMIT 1`
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
