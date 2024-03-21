package config

import (
	"context"
	"github.com/icinga/icingadb/pkg/types"
	"github.com/jmoiron/sqlx"
	"go.uber.org/zap"
)

// SourceTypeIcinga2 represents the "icinga2" Source Type for Event Stream API sources.
const SourceTypeIcinga2 = "icinga2"

// Source entry within the ConfigSet to describe a source.
type Source struct {
	ID   int64  `db:"id"`
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

// fieldEquals checks if this Source's database fields are equal to those of another Source.
func (source *Source) fieldEquals(other *Source) bool {
	boolEq := func(a, b types.Bool) bool { return (!a.Valid && !b.Valid) || (a == b) }
	stringEq := func(a, b types.String) bool { return (!a.Valid && !b.Valid) || (a == b) }

	return source.ID == other.ID &&
		source.Type == other.Type &&
		source.Name == other.Name &&
		stringEq(source.ListenerPasswordHash, other.ListenerPasswordHash) &&
		stringEq(source.Icinga2BaseURL, other.Icinga2BaseURL) &&
		stringEq(source.Icinga2AuthUser, other.Icinga2AuthUser) &&
		stringEq(source.Icinga2AuthPass, other.Icinga2AuthPass) &&
		stringEq(source.Icinga2CAPem, other.Icinga2CAPem) &&
		stringEq(source.Icinga2CommonName, other.Icinga2CommonName) &&
		boolEq(source.Icinga2InsecureTLS, other.Icinga2InsecureTLS)
}

// stop this Source's worker; currently only Icinga Event Stream API Client.
func (source *Source) stop() {
	if source.Type == SourceTypeIcinga2 && source.Icinga2SourceCancel != nil {
		source.Icinga2SourceCancel()
		source.Icinga2SourceCancel = nil
	}
}

func (r *RuntimeConfig) fetchSources(ctx context.Context, tx *sqlx.Tx) error {
	var sourcePtr *Source
	stmt := r.db.BuildSelectStmt(sourcePtr, sourcePtr)
	r.logger.Debugf("Executing query %q", stmt)

	var sources []*Source
	if err := tx.SelectContext(ctx, &sources, stmt); err != nil {
		r.logger.Errorln(err)
		return err
	}

	sourcesById := make(map[int64]*Source)
	for _, s := range sources {
		sourceLogger := r.logger.With(
			zap.Int64("id", s.ID),
			zap.String("name", s.Name),
			zap.String("type", s.Type),
		)
		if sourcesById[s.ID] != nil {
			sourceLogger.Error("Ignoring duplicate config for source ID")
			continue
		}

		sourcesById[s.ID] = s
		sourceLogger.Debug("loaded source config")
	}

	if r.Sources != nil {
		// mark no longer existing sources for deletion
		for id := range r.Sources {
			if _, ok := sourcesById[id]; !ok {
				sourcesById[id] = nil
			}
		}
	}

	r.pending.Sources = sourcesById

	return nil
}

func (r *RuntimeConfig) applyPendingSources() {
	if r.Sources == nil {
		r.Sources = make(map[int64]*Source)
	}

	for id, pendingSource := range r.pending.Sources {
		logger := r.logger.With(zap.Int64("id", id))
		currentSource := r.Sources[id]

		// Compare the pending source with an optional existing source; instruct the Event Source Client, if necessary.
		if pendingSource == nil && currentSource != nil {
			logger.Info("Source has been removed")

			currentSource.stop()
			delete(r.Sources, id)
			continue
		} else if pendingSource != nil && currentSource != nil {
			if currentSource.fieldEquals(pendingSource) {
				continue
			}

			logger.Info("Source has been updated")
			currentSource.stop()
		} else if pendingSource != nil && currentSource == nil {
			logger.Info("Source has been added")
		} else {
			// Neither an active nor a pending source?
			logger.Error("Cannot applying pending configuration: neither an active nor a pending source")
			continue
		}

		if pendingSource.Type == SourceTypeIcinga2 {
			r.EventStreamLaunchFunc(pendingSource)
		}

		r.Sources[id] = pendingSource
	}

	r.pending.Sources = nil
}
