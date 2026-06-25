package schema

import (
	"context"
	"embed"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/icinga/icinga-go-library/database"
	"github.com/icinga/icinga-go-library/logging"
)

const expectedVersion = "v1.0"

var upgrades = []string{}

//go:embed mysql/schema.sql mysql/upgrades/*.sql pgsql/schema.sql pgsql/upgrades/*.sql
var files embed.FS

// Ensure imports or updates the database schema to the version supported by this daemon.
func Ensure(ctx context.Context, db *database.DB, logger *logging.Logger) error {
	hasSchema, err := db.HasTable(ctx, "notifications_schema")
	if err != nil {
		return err
	}

	if hasSchema {
		version, err := latestVersion(ctx, db)
		if err != nil {
			return err
		}

		return update(ctx, db, logger, version)
	}

	hasBaseSchema, err := db.HasTable(ctx, "source")
	if err != nil {
		return err
	}
	if !hasBaseSchema {
		logger.Info("Importing database schema")

		schema, err := readSchema(db.DriverName())
		if err != nil {
			return err
		}

		return execStatements(ctx, db, schema)
	}

	isCurrentSchema, err := hasColumn(ctx, db, "source", "listener_username")
	if err != nil {
		return err
	}
	if isCurrentSchema {
		logger.Info("Recording database schema version")

		return createVersionTable(ctx, db, expectedVersion)
	}

	return update(ctx, db, logger, "")
}

func latestVersion(ctx context.Context, db *database.DB) (string, error) {
	var version string
	query := "SELECT version FROM notifications_schema ORDER BY id DESC LIMIT 1"
	if err := db.QueryRowxContext(ctx, query).Scan(&version); err != nil {
		return "", database.CantPerformQuery(err, query)
	}

	return version, nil
}

func update(ctx context.Context, db *database.DB, logger *logging.Logger, currentVersion string) error {
	if currentVersion == expectedVersion {
		return nil
	}
	if currentVersion != "" && len(upgrades) == 0 {
		return fmt.Errorf("unexpected database schema version %q, expected %q", currentVersion, expectedVersion)
	}

	applied := currentVersion
	for _, version := range upgrades {
		shouldApply := currentVersion == ""
		if !shouldApply {
			cmp, err := compareVersions(currentVersion, version)
			if err != nil {
				return err
			}
			shouldApply = cmp < 0
		}
		if !shouldApply {
			continue
		}

		logger.Infof("Updating database schema to %s", version)

		upgrade, err := readUpgrade(db.DriverName(), version)
		if err != nil {
			return err
		}
		if err := execStatements(ctx, db, upgrade); err != nil {
			return err
		}

		applied = version
	}

	if applied != expectedVersion {
		return fmt.Errorf("no database schema upgrade path from %q to %q", currentVersion, expectedVersion)
	}

	return nil
}

func createVersionTable(ctx context.Context, db *database.DB, version string) error {
	switch db.DriverName() {
	case database.MySQL:
		createStmt := `CREATE TABLE IF NOT EXISTS notifications_schema (
    id int NOT NULL AUTO_INCREMENT,
    version varchar(64) NOT NULL,
    timestamp bigint NOT NULL,

    CONSTRAINT pk_notifications_schema PRIMARY KEY (id),
    CONSTRAINT uk_notifications_schema_version UNIQUE (version)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin`
		if _, err := db.ExecContext(ctx, createStmt); err != nil {
			return database.CantPerformQuery(err, createStmt)
		}

		insertStmt := "INSERT INTO notifications_schema (version, timestamp) VALUES (?, UNIX_TIMESTAMP() * 1000)"
		if _, err := db.ExecContext(ctx, insertStmt, version); err != nil {
			return database.CantPerformQuery(err, insertStmt)
		}
	case database.PostgreSQL:
		createStmt := `CREATE TABLE IF NOT EXISTS notifications_schema (
    id serial,
    version varchar(64) NOT NULL,
    timestamp bigint NOT NULL,

    CONSTRAINT pk_notifications_schema PRIMARY KEY (id),
    CONSTRAINT uk_notifications_schema_version UNIQUE (version)
)`
		if _, err := db.ExecContext(ctx, createStmt); err != nil {
			return database.CantPerformQuery(err, createStmt)
		}

		insertStmt := "INSERT INTO notifications_schema (version, timestamp) VALUES ($1, floor(extract(epoch from current_timestamp) * 1000)::bigint)"
		if _, err := db.ExecContext(ctx, insertStmt, version); err != nil {
			return database.CantPerformQuery(err, insertStmt)
		}
	default:
		return fmt.Errorf("unsupported database driver %q", db.DriverName())
	}

	return nil
}

func hasColumn(ctx context.Context, db *database.DB, table, column string) (bool, error) {
	var tableSchemaFunc string
	switch db.DriverName() {
	case database.MySQL:
		tableSchemaFunc = "DATABASE()"
	case database.PostgreSQL:
		tableSchemaFunc = "CURRENT_SCHEMA()"
	default:
		return false, fmt.Errorf("unsupported database driver %q", db.DriverName())
	}

	query := db.Rebind("SELECT 1 FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA=" + tableSchemaFunc + " AND TABLE_NAME=? AND COLUMN_NAME=?")
	rows, err := db.QueryContext(ctx, query, table, column)
	if err != nil {
		return false, database.CantPerformQuery(err, query)
	}
	defer func() { _ = rows.Close() }()

	return rows.Next(), rows.Err()
}

func execStatements(ctx context.Context, db *database.DB, statements string) error {
	for _, statement := range splitStatements(db.DriverName(), statements) {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return database.CantPerformQuery(err, statement)
		}
	}

	return nil
}

func splitStatements(driverName, statements string) []string {
	if driverName == database.MySQL {
		return database.MysqlSplitStatements(statements)
	}

	return splitPostgreSQLStatements(statements)
}

func splitPostgreSQLStatements(sql string) []string {
	var result []string
	var start int
	var quote rune
	var dollarQuote string

	for i := 0; i < len(sql); i++ {
		if dollarQuote != "" {
			if strings.HasPrefix(sql[i:], dollarQuote) {
				i += len(dollarQuote) - 1
				dollarQuote = ""
			}
			continue
		}
		if quote != 0 {
			if rune(sql[i]) == quote {
				if i+1 < len(sql) && sql[i+1] == sql[i] {
					i++
				} else {
					quote = 0
				}
			}
			continue
		}

		switch sql[i] {
		case '\'', '"':
			quote = rune(sql[i])
		case '$':
			if tag := readDollarQuote(sql[i:]); tag != "" {
				dollarQuote = tag
				i += len(tag) - 1
			}
		case ';':
			if statement := strings.TrimSpace(sql[start:i]); statement != "" {
				result = append(result, statement)
			}
			start = i + 1
		}
	}

	if statement := strings.TrimSpace(sql[start:]); statement != "" {
		result = append(result, statement)
	}

	return result
}

var dollarQuoteRe = regexp.MustCompile(`^\$[A-Za-z_][A-Za-z_0-9]*\$|^\$\$`)

func readDollarQuote(sql string) string {
	return dollarQuoteRe.FindString(sql)
}

func compareVersions(a, b string) (int, error) {
	av, err := parseVersion(a)
	if err != nil {
		return 0, err
	}
	bv, err := parseVersion(b)
	if err != nil {
		return 0, err
	}

	for i := range av {
		if av[i] < bv[i] {
			return -1, nil
		}
		if av[i] > bv[i] {
			return 1, nil
		}
	}

	return 0, nil
}

func parseVersion(version string) ([3]int, error) {
	var parsed [3]int
	parts := strings.Split(version, ".")
	if len(parts) != len(parsed) {
		return parsed, fmt.Errorf("invalid database schema version %q", version)
	}

	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil {
			return parsed, fmt.Errorf("invalid database schema version %q", version)
		}
		parsed[i] = n
	}

	return parsed, nil
}

func readSchema(driverName string) (string, error) {
	switch driverName {
	case database.MySQL:
		return read("mysql/schema.sql")
	case database.PostgreSQL:
		return read("pgsql/schema.sql")
	default:
		return "", fmt.Errorf("unsupported database driver %q", driverName)
	}
}

func readUpgrade(driverName, version string) (string, error) {
	switch driverName {
	case database.MySQL:
		return read("mysql/upgrades/" + version + ".sql")
	case database.PostgreSQL:
		return read("pgsql/upgrades/" + version + ".sql")
	default:
		return "", fmt.Errorf("unsupported database driver %q", driverName)
	}
}

func read(name string) (string, error) {
	content, err := files.ReadFile(name)
	if err != nil {
		return "", err
	}

	return string(content), nil
}
