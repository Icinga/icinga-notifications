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
	"net/url"
	"strings"
)

// makeProcessEvent creates a closure function to process received events.
//
// This function contains glue code similar to those from Listener.ProcessEvent to check for incidents for the Event
// and, if existent, call *Incident.ProcessEvent on this incident.
func makeProcessEvent(
	ctx context.Context,
	db *icingadb.DB,
	logger *logging.Logger,
	logs *logging.Logging,
	runtimeConfig *config.RuntimeConfig,
) func(*event.Event) {
	return func(ev *event.Event) {
		obj, err := object.FromEvent(ctx, db, ev)
		if err != nil {
			logger.Errorw("Cannot sync object", zap.Stringer("event", ev), zap.Error(err))
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

		l := logger.With(
			zap.String("object", obj.DisplayName()),
			zap.Stringer("event", ev),
			zap.Stringer("incident", currentIncident),
			zap.Bool("created incident", created))

		if currentIncident == nil {
			switch {
			case ev.Type == event.TypeAcknowledgement:
				l.Warn("Object doesn't have active incident, ignoring acknowledgement event")
			case ev.Severity != event.SeverityOK:
				l.Error("Cannot process event with a non OK state without a known incident")
			default:
				l.Warn("Ignoring superfluous OK state event")
			}

			return
		}

		if err := currentIncident.ProcessEvent(ctx, ev, created); err != nil {
			logger.Errorw("Failed to process current incident", zap.Error(err))
			return
		}
	}
}

// rawurlencode mimics PHP's rawurlencode to be used for parameter encoding.
//
// Icinga Web uses rawurldecode instead of urldecode, which, as its main difference, does not honor the plus char ('+')
// as a valid substitution for space (' '). Unfortunately, Go's url.QueryEscape does this very substitution and
// url.PathEscape does a bit too less and has a misleading name on top.
//
//   - https://www.php.net/manual/en/function.rawurlencode.php
//   - https://github.com/php/php-src/blob/php-8.2.12/ext/standard/url.c#L538
func rawurlencode(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}
