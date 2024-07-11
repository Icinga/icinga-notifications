package config

import (
	"context"
	"github.com/icinga/icinga-go-library/types"
	"github.com/icinga/icinga-notifications/internal/config/baseconf"
	"go.uber.org/zap/zapcore"
)

// SourceTypeIcinga2 represents the "icinga2" Source Type for Event Stream API sources.
const SourceTypeIcinga2 = "icinga2"

// Source entry within the ConfigSet to describe a source.
type Source struct {
	baseconf.IncrementalPkDbEntry[int64] `db:",inline"`

	Type string `db:"type"`
	Name string `db:"name"`

	ListenerPasswordHash types.String `db:"listener_password_hash"`

	Icinga2BaseURL     types.String `db:"icinga2_base_url"`
	Icinga2AuthUser    types.String `db:"icinga2_auth_user"`
	Icinga2AuthPass    types.String `db:"icinga2_auth_pass"`
	Icinga2CAPem       types.String `db:"icinga2_ca_pem"`
	Icinga2CommonName  types.String `db:"icinga2_common_name"`
	Icinga2InsecureTLS types.Bool   `db:"icinga2_insecure_tls"`

	// Icinga2SourceConf for Event Stream API sources, only if Source.Type == SourceTypeIcinga2.
	Icinga2SourceCancel context.CancelFunc `db:"-" json:"-"`
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
		func(newElement *Source) error {
			if newElement.Type == SourceTypeIcinga2 {
				r.EventStreamLaunchFunc(newElement)
			}
			return nil
		},
		nil,
		func(delElement *Source) error {
			if delElement.Type == SourceTypeIcinga2 && delElement.Icinga2SourceCancel != nil {
				delElement.Icinga2SourceCancel()
				delElement.Icinga2SourceCancel = nil
			}
			return nil
		})
}
