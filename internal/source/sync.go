package source

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/icinga/icinga-go-library/database"
	"github.com/icinga/icinga-go-library/logging"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

type configuredRow struct {
	ID                   int64          `db:"id"`
	Type                 string         `db:"type"`
	Name                 string         `db:"name"`
	ListenerPasswordHash sql.NullString `db:"listener_password_hash"`
}

func SyncConfigured(ctx context.Context, db *database.DB, config []Config, logger *logging.Logger) error {
	for _, source := range config {
		if err := syncConfigured(ctx, db, source, logger); err != nil {
			return err
		}
	}

	return nil
}

func syncConfigured(ctx context.Context, db *database.DB, source Config, logger *logging.Logger) error {
	existing, err := getConfigured(ctx, db, source.Username)
	if err != nil {
		return err
	}

	now := time.Now().UnixMilli()
	if existing == nil {
		hash, err := bcrypt.GenerateFromPassword([]byte(source.Password), bcrypt.DefaultCost)
		if err != nil {
			return fmt.Errorf("can't hash source password: %w", err)
		}

		stmt := db.Rebind(`
			INSERT INTO "source" ("type", "name", "listener_username", "listener_password_hash", "changed_at")
			VALUES (?, ?, ?, ?, ?)`)
		if _, err := db.ExecContext(ctx, stmt, source.Type, source.Name, source.Username, string(hash), now); err != nil {
			return fmt.Errorf("can't create configured source %q: %w", source.Username, err)
		}

		logger.Infow("Created configured source", zap.String("source", source.Name), zap.String("username", source.Username))
		return nil
	}

	passwordHash := existing.ListenerPasswordHash.String
	if !existing.ListenerPasswordHash.Valid ||
		bcrypt.CompareHashAndPassword([]byte(existing.ListenerPasswordHash.String), []byte(source.Password)) != nil {
		hash, err := bcrypt.GenerateFromPassword([]byte(source.Password), bcrypt.DefaultCost)
		if err != nil {
			return fmt.Errorf("can't hash source password: %w", err)
		}

		passwordHash = string(hash)
	}

	if existing.Type == source.Type && existing.Name == source.Name && existing.ListenerPasswordHash.Valid &&
		existing.ListenerPasswordHash.String == passwordHash {
		logger.Debugw("Configured source already up to date", zap.String("source", source.Name), zap.String("username", source.Username))
		return nil
	}

	stmt := db.Rebind(`
		UPDATE "source"
		SET "type" = ?, "name" = ?, "listener_password_hash" = ?, "changed_at" = ?
		WHERE "id" = ?`)
	if _, err := db.ExecContext(ctx, stmt, source.Type, source.Name, passwordHash, now, existing.ID); err != nil {
		return fmt.Errorf("can't update configured source %q: %w", source.Username, err)
	}

	logger.Infow("Updated configured source", zap.String("source", source.Name), zap.String("username", source.Username))
	return nil
}

func getConfigured(ctx context.Context, db *database.DB, username string) (*configuredRow, error) {
	stmt := db.Rebind(`
		SELECT "id", "type", "name", "listener_password_hash"
		FROM "source"
		WHERE "listener_username" = ? AND "deleted" = 'n'`)

	var source configuredRow
	if err := db.GetContext(ctx, &source, stmt, username); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}

		return nil, fmt.Errorf("can't load configured source %q: %w", username, err)
	}

	return &source, nil
}
