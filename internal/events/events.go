package events

import (
	"context"
	"github.com/icinga/icinga-go-library/database"
	"github.com/icinga/icinga-go-library/logging"
	"github.com/icinga/icinga-notifications/internal/config"
	"github.com/icinga/icinga-notifications/internal/event"
	"github.com/icinga/icinga-notifications/internal/notification"
)

// Process processes the specified event.Event.
//
// Please note that this function is the only way to access the internal events.router type.
//
// The returned error might be wrapped around event.ErrSuperfluousStateChange.
func Process(ctx context.Context, db *database.DB, logs *logging.Logging, rc *config.RuntimeConfig, ev *event.Event) error {
	r := &router{
		logs:      logs,
		Evaluable: config.NewEvaluable(),
		Notifier: notification.Notifier{
			DB:            db,
			RuntimeConfig: rc,
			Logger:        logs.GetChildLogger("routing").SugaredLogger,
		},
	}

	return r.route(ctx, ev)
}
