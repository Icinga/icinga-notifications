package config

import (
	"context"
	"database/sql"
	"errors"
	"github.com/icinga/icinga-go-library/database"
	"github.com/icinga/icinga-go-library/logging"
	"github.com/icinga/icinga-go-library/types"
	"github.com/icinga/icinga-notifications/internal/channel"
	"github.com/icinga/icinga-notifications/internal/recipient"
	"github.com/icinga/icinga-notifications/internal/rule"
	"github.com/icinga/icinga-notifications/internal/timeperiod"
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

	// configChange contains incremental changes to config objects to be merged into the live configuration.
	//
	// It will be both created and deleted within RuntimeConfig.UpdateFromDatabase. To keep track of the known state,
	// the last known timestamp of each ConfigSet type is stored within configChangeTimestamps.
	configChange           *ConfigSet
	configChangeAvailable  bool
	configChangeTimestamps map[string]types.UnixMilli

	logs   *logging.Logging
	logger *logging.Logger
	db     *database.DB

	// mu is used to synchronize access to the live ConfigSet.
	mu sync.RWMutex
}

func NewRuntimeConfig(
	logs *logging.Logging,
	db *database.DB,
) *RuntimeConfig {
	return &RuntimeConfig{
		configChangeTimestamps: make(map[string]types.UnixMilli),

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
	Sources          map[int64]*Source

	RuleSet // RuleSet contains the currently loaded rules and their version.

	// The following fields contain intermediate values, necessary for the incremental config synchronization.
	// Furthermore, they allow accessing intermediate tables as everything is referred by pointers.
	groupMembers             map[recipient.GroupMemberKey]*recipient.GroupMember
	timePeriodEntries        map[int64]*timeperiod.Entry
	scheduleRotations        map[int64]*recipient.Rotation
	scheduleRotationMembers  map[int64]*recipient.RotationMember
	ruleEscalations          map[int64]*rule.Escalation
	ruleEscalationRecipients map[int64]*rule.EscalationRecipient
}

func (r *RuntimeConfig) UpdateFromDatabase(ctx context.Context) error {
	startTime := time.Now()
	defer func() {
		r.logger.Debugw("Finished configuration synchronization", zap.Duration("took", time.Since(startTime)))
	}()

	r.logger.Debug("Synchronizing configuration with database")

	r.configChange = &ConfigSet{}
	r.configChangeAvailable = false
	defer func() { r.configChange = nil }()

	if err := r.fetchFromDatabase(ctx); err != nil {
		return err
	}

	r.applyPending()
	if r.configChangeAvailable {
		r.logger.Debug("Synchronizing applied configuration changes, verifying state")
		if err := r.debugVerify(); err != nil {
			r.logger.Fatalw("Newly synchronized configuration failed verification", zap.Error(err))
		}
	}

	return nil
}

func (r *RuntimeConfig) PeriodicUpdates(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := r.UpdateFromDatabase(ctx); err != nil {
				r.logger.Errorw("Periodic configuration synchronization failed", zap.Error(err))
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

const MissingRuleVersion = "N/A"

// GetRulesVersionFor retrieves the version of the rules for a specific source.
//
// If either no rules or no rule for this source exist, MissingRuleVersion is returned.
//
// May not be called while holding the write lock on the RuntimeConfig.
func (r *RuntimeConfig) GetRulesVersionFor(srcId int64) string {
	r.RLock()
	defer r.RUnlock()

	if r.RulesBySource != nil {
		if sourceInfo, ok := r.RulesBySource[srcId]; ok {
			return sourceInfo.Version.String()
		}
	}

	return MissingRuleVersion
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
		logger.Debugw("Cannot extract source ID from HTTP basic auth username", zap.String("user_input", user))
		return nil
	}
	sourceId, err := strconv.ParseInt(sourceIdRaw, 10, 64)
	if err != nil {
		logger.Debugw("Cannot convert extracted source Id to int", zap.String("user_input", user), zap.Error(err))
		return nil
	}

	src, ok := r.Sources[sourceId]
	if !ok {
		logger.Debugw("Cannot check credentials for unknown source ID", zap.Int64("id", sourceId))
		return nil
	}

	err = src.PasswordCompare([]byte(pass))
	if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
		logger.Debugw("Invalid password for this source", zap.Int64("id", sourceId))
		return nil
	} else if err != nil {
		logger.Errorw("Failed to verify password for this source", zap.Int64("id", sourceId), zap.Error(err))
		return nil
	}

	return src
}

func (r *RuntimeConfig) fetchFromDatabase(ctx context.Context) error {
	tx, err := r.db.BeginTxx(ctx, &sql.TxOptions{
		Isolation: sql.LevelRepeatableRead,
		ReadOnly:  true,
	})
	if err != nil {
		return err
	}
	// The transaction is only used for reading, never has to be committed.
	defer func() { _ = tx.Rollback() }()

	fetchFns := []func() error{
		func() error { return incrementalFetch(ctx, tx, r, &r.configChange.Channels) },
		func() error { return incrementalFetch(ctx, tx, r, &r.configChange.Contacts) },
		func() error { return incrementalFetch(ctx, tx, r, &r.configChange.ContactAddresses) },
		func() error { return incrementalFetch(ctx, tx, r, &r.configChange.Groups) },
		func() error { return incrementalFetch(ctx, tx, r, &r.configChange.groupMembers) },
		func() error { return incrementalFetch(ctx, tx, r, &r.configChange.Schedules) },
		func() error { return incrementalFetch(ctx, tx, r, &r.configChange.scheduleRotations) },
		func() error { return incrementalFetch(ctx, tx, r, &r.configChange.scheduleRotationMembers) },
		func() error { return incrementalFetch(ctx, tx, r, &r.configChange.TimePeriods) },
		func() error { return incrementalFetch(ctx, tx, r, &r.configChange.timePeriodEntries) },
		func() error { return incrementalFetch(ctx, tx, r, &r.configChange.Rules) },
		func() error { return incrementalFetch(ctx, tx, r, &r.configChange.ruleEscalations) },
		func() error { return incrementalFetch(ctx, tx, r, &r.configChange.ruleEscalationRecipients) },
		func() error { return incrementalFetch(ctx, tx, r, &r.configChange.Sources) },
	}
	for _, f := range fetchFns {
		if err := f(); err != nil {
			return err
		}
	}

	return nil
}

// applyPending synchronizes all changes.
func (r *RuntimeConfig) applyPending() {
	r.mu.Lock()
	defer r.mu.Unlock()

	applyFns := []func(){
		r.applyPendingChannels,
		r.applyPendingContacts,
		r.applyPendingGroups,
		r.applyPendingSchedules,
		r.applyPendingTimePeriods,
		r.applyPendingRules,
		r.applyPendingSources,
	}
	for _, f := range applyFns {
		f()
	}
}
