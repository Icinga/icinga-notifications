package eventstream

import (
	"context"
	"github.com/icinga/icinga-notifications/internal/config"
	"github.com/icinga/icinga-notifications/internal/event"
	"github.com/icinga/icinga-notifications/internal/incident"
	"github.com/icinga/icinga-notifications/internal/object"
	"github.com/icinga/icingadb/pkg/icingadb"
	"github.com/icinga/icingadb/pkg/logging"
	"go.uber.org/zap"
)

// ProcessEvent is a copy pasta version of the second half of Listener.ProcessEvent to be removed after #99 has landed.
func ProcessEvent(
	ev *event.Event,
	db *icingadb.DB,
	logger *logging.Logger,
	logs *logging.Logging,
	runtimeConfig *config.RuntimeConfig,
) {
	ctx := context.Background()
	obj, err := object.FromEvent(ctx, db, ev)
	if err != nil {
		logger.Errorw("Can't sync object", zap.Error(err))
		return
	}

	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		logger.Errorw("Can't start a db transaction", zap.Error(err))
		return
	}
	defer func() { _ = tx.Rollback() }()

	if err := ev.Sync(ctx, tx, db, obj.ID); err != nil {
		logger.Errorw("Failed to insert event and fetch its ID", zap.String("event", ev.String()), zap.Error(err))
		return
	}

	createIncident := ev.Severity != event.SeverityNone && ev.Severity != event.SeverityOK
	currentIncident, created, err := incident.GetCurrent(
		ctx,
		db,
		obj,
		logs.GetChildLogger("incident"),
		runtimeConfig,
		createIncident)
	if err != nil {
		logger.Errorw("Failed to get current incident", zap.Error(err))
		return
	}

	if currentIncident == nil {
		if ev.Type == event.TypeAcknowledgement {
			logger.Warnf("%q doesn't have active incident. Ignoring acknowledgement event from source %d", obj.DisplayName(), ev.SourceId)
			return
		}

		if ev.Severity != event.SeverityOK {
			logger.Error("non-OK state but no incident was created")
			return
		}

		logger.Warnw("Ignoring superfluous OK state event from source %d", zap.Int64("source", ev.SourceId), zap.String("object", obj.DisplayName()))
		return
	}

	logger.Debugf("Processing event %v", ev)

	if err := currentIncident.ProcessEvent(ctx, ev, created); err != nil {
		logger.Errorw("Failed to process current incident", zap.Error(err))
		return
	}

	if err = tx.Commit(); err != nil {
		logger.Errorw(
			"Can't commit db transaction", zap.String("object", obj.DisplayName()),
			zap.String("incident", currentIncident.String()), zap.Error(err),
		)
		return
	}
}

// MakeProcessEvent creates a closure around ProcessEvent to wrap all arguments except the event.Event.
func MakeProcessEvent(
	db *icingadb.DB,
	logger *logging.Logger,
	logs *logging.Logging,
	runtimeConfig *config.RuntimeConfig,
) func(*event.Event) {
	return func(ev *event.Event) { ProcessEvent(ev, db, logger, logs, runtimeConfig) }
}
