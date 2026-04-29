package config

import (
	"crypto/subtle"
	"fmt"
	"github.com/icinga/icinga-go-library/types"
	"github.com/icinga/icinga-notifications/internal/config/baseconf"
	"go.uber.org/zap/zapcore"
	"golang.org/x/crypto/bcrypt"
	"sync"
)

// Source entry within the ConfigSet to describe a source.
type Source struct {
	baseconf.IncrementalPkDbEntry[int64] `db:",inline"`

	Type string `db:"type"`
	Name string `db:"name"`

	ListenerUsername      types.String `db:"listener_username"`
	ListenerPasswordHash  types.String `db:"listener_password_hash"`
	listenerPassword      []byte       `db:"-"`
	listenerPasswordMutex sync.Mutex

	// ruleIDs is a list of rule IDs belonging to this source.
	//
	// Each of these IDs corresponds to a rule in the [ConfigSet.Rules] map and is used to quickly access
	// the rules for a specific source without iterating over all rules. It is not stored in the database,
	// but is updated when applying pending rules in [RuntimeConfig.applyPendingRules].
	ruleIDs []int64
}

// MarshalLogObject implements the zapcore.ObjectMarshaler interface.
func (source *Source) MarshalLogObject(encoder zapcore.ObjectEncoder) error {
	encoder.AddInt64("id", source.ID)
	encoder.AddString("type", source.Type)
	encoder.AddString("name", source.Name)
	return nil
}

// PasswordCompare checks if a password matches this Source's password with a cache.
//
// This method returns nil if the password is correct, [bcrypt.ErrMismatchedHashAndPassword] in case of an invalid
// password and another error if something else went wrong.
//
// This cache might be necessary as, for the moment, bcrypt is used to hash the passwords. By design, bcrypt is
// expensive, resulting in unnecessary delays when excessively using the API.
//
// If either PHP's PASSWORD_DEFAULT changes or Icinga Web 2 starts using something else, e.g., Argon2id, this will
// return a descriptive error as the identifier does no longer match the bcrypt "$2y$".
//
// If a Source changes, it will be recreated - RuntimeConfig.applyPendingSources has a nil updateFn - and the cache is
// automatically purged.
func (source *Source) PasswordCompare(password []byte) error {
	if !source.ListenerPasswordHash.Valid {
		return fmt.Errorf("source has no password hash to compare")
	}

	source.listenerPasswordMutex.Lock()
	defer source.listenerPasswordMutex.Unlock()

	if source.listenerPassword != nil {
		if subtle.ConstantTimeCompare(source.listenerPassword, password) != 1 {
			return bcrypt.ErrMismatchedHashAndPassword
		}

		return nil
	}

	err := bcrypt.CompareHashAndPassword([]byte(source.ListenerPasswordHash.String), password)
	if err != nil {
		return err
	}

	source.listenerPassword = password

	return nil
}

// RuleIDs returns the list of rule IDs belonging to this source.
func (source *Source) RuleIDs() []int64 { return source.ruleIDs }

// applyPendingSources synchronizes changed sources.
func (r *RuntimeConfig) applyPendingSources() {
	incrementalApplyPending(
		r,
		&r.Sources, &r.configChange.Sources,
		func(newElement *Source) error {
			// When the event rules are loaded before the sources, the rule IDs are not yet added to the
			// per-source rules cache. We need to add them here to make sure the cache is correct.
			for _, rule := range r.Rules {
				if rule.SourceID == newElement.ID {
					newElement.ruleIDs = append(newElement.ruleIDs, rule.ID)
				}
			}
			return nil
		},
		nil,
		nil)
}
