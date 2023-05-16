package config

import (
	"context"
	"database/sql"
	"github.com/icinga/icinga-notifications/internal/channel"
	"github.com/icinga/icinga-notifications/internal/recipient"
	"github.com/icinga/icinga-notifications/internal/rule"
	"github.com/icinga/icinga-notifications/internal/timeperiod"
	"github.com/icinga/icingadb/pkg/icingadb"
	"github.com/icinga/icingadb/pkg/logging"
	"github.com/jmoiron/sqlx"
	"go.uber.org/zap"
	"sync"
	"time"
)

// RuntimeConfig stores the runtime representation of the configuration present in the database.
type RuntimeConfig struct {
	// ConfigSet is the current live config. It is embedded to allow direct access to its members.
	// Accessing it requires a lock that is obtained with RLock() and released with RUnlock().
	ConfigSet

	// pending contains changes to config objects that are to be applied to the embedded live config.
	pending ConfigSet

	logger *logging.Logger
	db     *icingadb.DB

	// mu is used to synchronize access to the live ConfigSet.
	mu sync.RWMutex
}

func NewRuntimeConfig(db *icingadb.DB, logger *logging.Logger) *RuntimeConfig {
	return &RuntimeConfig{db: db, logger: logger}
}

type ConfigSet struct {
	Channels         map[string]*channel.Channel
	Contacts         map[int64]*recipient.Contact
	ContactAddresses map[int64]*recipient.Address
	Groups           map[int64]*recipient.Group
	TimePeriods      map[int64]*timeperiod.TimePeriod
	Schedules        map[int64]*recipient.Schedule
	Rules            map[int64]*rule.Rule
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
			break
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

// GetContact returns *recipient.Contact by the given username.
// Returns nil when the given username doesn't exist.
func (r *RuntimeConfig) GetContact(username string) *recipient.Contact {
	for _, contact := range r.Contacts {
		if contact.Username.String == username {
			return contact
		}
	}

	return nil
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

	r.logger.Debugw("applied pending configuration", zap.Duration("took", time.Since(start)))
}
