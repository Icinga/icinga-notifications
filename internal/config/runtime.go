package config

import (
	"context"
	"database/sql"
	"errors"
	"github.com/icinga/icinga-go-library/database"
	"github.com/icinga/icinga-go-library/logging"
	"github.com/icinga/icinga-notifications/internal/channel"
	"github.com/icinga/icinga-notifications/internal/recipient"
	"github.com/icinga/icinga-notifications/internal/rule"
	"github.com/icinga/icinga-notifications/internal/timeperiod"
	"github.com/jmoiron/sqlx"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// RuntimeConfig stores the runtime representation of the configuration present in the database.
type RuntimeConfig struct {
	// ConfigSet is the current live config. It is embedded to allow direct access to its members.
	// Accessing it requires a lock that is obtained with RLock() and released with RUnlock().
	ConfigSet

	// EventStreamLaunchFunc is a callback to launch an Event Stream API Client.
	// This became necessary due to circular imports, either with the incident or icinga2 package.
	EventStreamLaunchFunc func(source *Source)

	// pending contains changes to config objects that are to be applied to the embedded live config.
	pending ConfigSet

	logs   *logging.Logging
	logger *logging.Logger
	db     *database.DB

	// mu is used to synchronize access to the live ConfigSet.
	mu sync.RWMutex
}

func NewRuntimeConfig(
	esLaunch func(source *Source),
	logs *logging.Logging,
	db *database.DB,
) *RuntimeConfig {
	return &RuntimeConfig{
		EventStreamLaunchFunc: esLaunch,

		logs:   logs,
		logger: logs.GetChildLogger("runtime-updates"),
		db:     db,
	}
}

type ConfigSet struct {
	Channels         map[int64]*channel.Channel
	Contacts         map[int64]*recipient.Contact
	ContactAddresses map[int64]*recipient.Address
	Groups           map[int64]*recipient.Group
	TimePeriods      map[int64]*timeperiod.TimePeriod
	Schedules        map[int64]*recipient.Schedule
	Rules            map[int64]*rule.Rule
	Sources          map[int64]*Source
}

func (r *RuntimeConfig) UpdateFromDatabase(ctx context.Context) error {
	err := r.fetchFromDatabase(ctx)
	if err != nil {
		return err
	}

	r.applyPending()

	err = r.debugVerify()
	if err != nil {
		panic(err)
	}

	return nil
}

func (r *RuntimeConfig) PeriodicUpdates(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			r.logger.Debug("periodically updating config")
			err := r.UpdateFromDatabase(ctx)
			if err != nil {
				r.logger.Errorw("periodic config update failed, continuing with previous config", zap.Error(err))
			}
		case <-ctx.Done():
			return
		}
	}
}

// RLock locks the config for reading.
func (r *RuntimeConfig) RLock() {
	r.mu.RLock()
}

// RUnlock releases a lock obtained by RLock().
func (r *RuntimeConfig) RUnlock() {
	r.mu.RUnlock()
}

func (r *RuntimeConfig) GetRecipient(k recipient.Key) recipient.Recipient {
	// Note: be careful to return nil for non-existent IDs instead of (*T)(nil) as (*T)(nil) != nil.
	if k.ContactID.Valid {
		c := r.Contacts[k.ContactID.Int64]
		if c != nil {
			return c
		}
	} else if k.GroupID.Valid {
		g := r.Groups[k.GroupID.Int64]
		if g != nil {
			return g
		}
	} else if k.ScheduleID.Valid {
		s := r.Schedules[k.ScheduleID.Int64]
		if s != nil {
			return s
		}
	}

	return nil
}

// GetRuleEscalation returns a *rule.Escalation by the given id.
// Returns nil if there is no rule escalation with given id.
func (r *RuntimeConfig) GetRuleEscalation(escalationID int64) *rule.Escalation {
	for _, r := range r.Rules {
		escalation, ok := r.Escalations[escalationID]
		if ok {
			return escalation
		}
	}

	return nil
}

// GetContact returns *recipient.Contact by the given username (case-insensitive).
// Returns nil when the given username doesn't exist.
func (r *RuntimeConfig) GetContact(username string) *recipient.Contact {
	for _, contact := range r.Contacts {
		if strings.EqualFold(contact.Username.String, username) {
			return contact
		}
	}

	return nil
}

// GetSourceFromCredentials verifies a credential pair against known Sources.
//
// This method returns either a *Source or a nil pointer and logs the cause to the given logger. This is in almost all
// cases a debug logging message, except when something server-side is wrong, e.g., the hash is invalid.
func (r *RuntimeConfig) GetSourceFromCredentials(user, pass string, logger *logging.Logger) *Source {
	r.RLock()
	defer r.RUnlock()

	sourceIdRaw, sourceIdOk := strings.CutPrefix(user, "source-")
	if !sourceIdOk {
		logger.Debugw("Cannot extract source ID from HTTP basic auth username", zap.String("user-input", user))
		return nil
	}
	sourceId, err := strconv.ParseInt(sourceIdRaw, 10, 64)
	if err != nil {
		logger.Debugw("Cannot convert extracted source Id to int", zap.String("user-input", user), zap.Error(err))
		return nil
	}

	source, ok := r.Sources[sourceId]
	if !ok {
		logger.Debugw("Cannot check credentials for unknown source ID", zap.Int64("id", sourceId))
		return nil
	}

	if !source.ListenerPasswordHash.Valid {
		logger.Debugw("Cannot check credentials for source without a listener_password_hash", zap.Int64("id", sourceId))
		return nil
	}

	// If either PHP's PASSWORD_DEFAULT changes or Icinga Web 2 starts using something else, e.g., Argon2id, this will
	// return a descriptive error as the identifier does no longer match the bcrypt "$2y$".
	err = bcrypt.CompareHashAndPassword([]byte(source.ListenerPasswordHash.String), []byte(pass))
	if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
		logger.Debugw("Invalid password for this source", zap.Int64("id", sourceId))
		return nil
	} else if err != nil {
		logger.Errorw("Failed to verify password for this source", zap.Int64("id", sourceId), zap.Error(err))
		return nil
	}

	return source
}

func (r *RuntimeConfig) fetchFromDatabase(ctx context.Context) error {
	r.logger.Debug("fetching configuration from database")
	start := time.Now()

	// Reset all pending state to start from a clean state.
	r.pending = ConfigSet{}

	tx, err := r.db.BeginTxx(ctx, &sql.TxOptions{
		Isolation: sql.LevelRepeatableRead,
		ReadOnly:  true,
	})
	if err != nil {
		return err
	}
	// The transaction is only used for reading, never has to be committed.
	defer func() { _ = tx.Rollback() }()

	updateFuncs := []func(ctx context.Context, tx *sqlx.Tx) error{
		r.fetchChannels,
		r.fetchContacts,
		r.fetchContactAddresses,
		r.fetchGroups,
		r.fetchTimePeriods,
		r.fetchSchedules,
		r.fetchRules,
		r.fetchSources,
	}
	for _, f := range updateFuncs {
		if err := f(ctx, tx); err != nil {
			return err
		}
	}

	r.logger.Debugw("fetched configuration from database", zap.Duration("took", time.Since(start)))

	return nil
}

func (r *RuntimeConfig) applyPending() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.logger.Debug("applying pending configuration")
	start := time.Now()

	r.applyPendingChannels()
	r.applyPendingContacts()
	r.applyPendingContactAddresses()
	r.applyPendingGroups()
	r.applyPendingTimePeriods()
	r.applyPendingSchedules()
	r.applyPendingRules()
	r.applyPendingSources()

	r.logger.Debugw("applied pending configuration", zap.Duration("took", time.Since(start)))
}
