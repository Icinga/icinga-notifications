package config

import (
	"github.com/icinga/icinga-go-library/types"
	"github.com/icinga/icinga-notifications/internal/config/baseconf"
	"go.uber.org/zap/zapcore"
)

// Source entry within the ConfigSet to describe a source.
type Source struct {
	baseconf.IncrementalPkDbEntry[int64] `db:",inline"`

	Type string `db:"type"`
	Name string `db:"name"`

	ListenerPasswordHash types.String `db:"listener_password_hash"`
}

// MarshalLogObject implements the zapcore.ObjectMarshaler interface.
func (source *Source) MarshalLogObject(encoder zapcore.ObjectEncoder) error {
	encoder.AddInt64("id", source.ID)
	encoder.AddString("type", source.Type)
	encoder.AddString("name", source.Name)
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
