package config

import (
	"crypto/subtle"
	"fmt"
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

	ListenerPasswordHash  string `db:"listener_password_hash"`
	listenerPassword      []byte `db:"-"`
	listenerPasswordMutex sync.Mutex
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
	if source.ListenerPasswordHash == "" {
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

	err := bcrypt.CompareHashAndPassword([]byte(source.ListenerPasswordHash), password)
	if err != nil {
		return err
	}

	source.listenerPassword = password

	return nil
}

// applyPendingSources synchronizes changed sources.
func (r *RuntimeConfig) applyPendingSources() {
	incrementalApplyPending(
		r,
		&r.Sources, &r.configChange.Sources,
		nil,
		nil,
		nil)
}
