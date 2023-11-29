package config

import (
	"context"
	"github.com/jmoiron/sqlx"
	"go.uber.org/zap"
)

// Source entry within the ConfigSet to describe a source.
type Source struct {
	ID   int64  `db:"id"`
	Type string `db:"type"`
	Name string `db:"name"`

	ListenerPasswordHash string `db:"listener_password_hash"`
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
			sourceLogger.Warnw("ignoring duplicate config for source ID")
		} else {
			sourcesById[s.ID] = s

			sourceLogger.Debugw("loaded source config")
		}
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
		if pendingSource == nil {
			r.logger.Infow("Source has been removed",
				zap.Int64("id", r.Sources[id].ID),
				zap.String("name", r.Sources[id].Name),
				zap.String("type", r.Sources[id].Type))

			delete(r.Sources, id)
		} else {
			r.Sources[id] = pendingSource
		}
	}

	r.pending.Sources = nil
}
