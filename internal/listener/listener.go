package listener

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"github.com/icinga/icinga-notifications/internal/config"
	"github.com/icinga/icinga-notifications/internal/event"
	"github.com/icinga/icinga-notifications/internal/incident"
	"github.com/icinga/icinga-notifications/internal/object"
	"github.com/icinga/icinga-notifications/internal/utils"
	"github.com/icinga/icingadb/pkg/icingadb"
	"github.com/icinga/icingadb/pkg/logging"
	"go.uber.org/zap"
	"net/http"
	"time"
)

type Listener struct {
	configFile    *config.ConfigFile
	db            *icingadb.DB
	logger        *logging.Logger
	runtimeConfig *config.RuntimeConfig

	logs *logging.Logging
	mux  http.ServeMux
}

func NewListener(db *icingadb.DB, configFile *config.ConfigFile, runtimeConfig *config.RuntimeConfig, logs *logging.Logging) *Listener {
	l := &Listener{
		configFile:    configFile,
		db:            db,
		logger:        logs.GetChildLogger("listener"),
		logs:          logs,
		runtimeConfig: runtimeConfig,
	}
	l.mux.HandleFunc("/process-event", l.ProcessEvent)
	l.mux.HandleFunc("/dump-config", l.DumpConfig)
	l.mux.HandleFunc("/dump-incidents", l.DumpIncidents)
	return l
}

func (l *Listener) Run() error {
	l.logger.Infof("Starting listener on http://%s", l.configFile.Listen)
	return http.ListenAndServe(l.configFile.Listen, &l.mux)
}

func (l *Listener) ProcessEvent(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		_, _ = fmt.Fprintln(w, "POST required")
		return
	}

	var ev event.Event
	err := json.NewDecoder(req.Body).Decode(&ev)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprintf(w, "cannot parse JSON body: %v\n", err)
		return
	}
	ev.Time = time.Now()

	if ev.Severity == event.SeverityNone && ev.Type == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprintln(w, "ignoring invalid event: must set 'type' or 'severity'")
		return
	}

	if ev.Severity != event.SeverityNone {
		if ev.Type == "" {
			ev.Type = event.TypeState
		} else if ev.Type != event.TypeState {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprintf(w, "ignoring invalid event: if 'severity' is set, 'type' must not be set or set to %q\n", event.TypeState)
			return
		}
	}

	if ev.Severity == event.SeverityNone {
		if ev.Type != event.TypeAcknowledgement {
			// It's neither a state nor an acknowledgement event.
			w.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprintf(w, "received not a state/acknowledgement event, ignoring\n")
			return
		}
	}

	ctx := context.Background()
	obj, err := object.FromTags(ctx, l.db, ev.Tags)
	if err != nil {
		l.logger.Errorln(err)

		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprintln(w, err.Error())
		return
	}

	tx, err := l.db.BeginTxx(ctx, nil)
	if err != nil {
		l.logger.Errorw("Can't start a db transaction", zap.Error(err))

		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprintln(w, "can't start a db transaction")
		return
	}
	defer func() { _ = tx.Rollback() }()

	if err := obj.UpdateMetadata(ctx, tx, ev.SourceId, ev.Name, utils.ToDBString(ev.URL), ev.ExtraTags); err != nil {
		l.logger.Errorw("Can't update object metadata", zap.String("object", obj.DisplayName()), zap.Error(err))

		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprintln(w, "can't update object metadata")
		return
	}

	if err := ev.Sync(ctx, tx, l.db, obj.ID); err != nil {
		l.logger.Errorw("Failed to insert event and fetch its ID", zap.String("event", ev.String()), zap.Error(err))

		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprintln(w, "can't insert event and fetch its ID")
		return
	}

	createIncident := ev.Severity != event.SeverityNone && ev.Severity != event.SeverityOK
	currentIncident, created, err := incident.GetCurrent(ctx, l.db, obj, l.logs.GetChildLogger("incident"), l.runtimeConfig, l.configFile, createIncident)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprintln(w, err)
		return
	}

	if currentIncident == nil {
		w.WriteHeader(http.StatusNotAcceptable)

		if ev.Type == event.TypeAcknowledgement {
			msg := fmt.Sprintf("%q doesn't have active incident. Ignoring acknowledgement event from source %d", obj.DisplayName(), ev.SourceId)
			_, _ = fmt.Fprintln(w, msg)

			l.logger.Warnln(msg)
			return
		}

		if ev.Severity != event.SeverityOK {
			panic("non-OK state but no incident was created")
		}

		msg := fmt.Sprintf("Ignoring superfluous OK state event from source %d", ev.SourceId)
		l.logger.Warnw(msg, zap.String("object", obj.DisplayName()))

		_, _ = fmt.Fprintln(w, msg)
		return
	}

	l.logger.Infof("Processing event")

	if err := currentIncident.ProcessEvent(ctx, tx, ev, created); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprintln(w, err)
		return
	}

	if err = tx.Commit(); err != nil {
		l.logger.Errorw(
			"Can't commit db transaction", zap.String("object", obj.DisplayName()),
			zap.String("incident", currentIncident.String()), zap.Error(err),
		)

		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprintln(w, "can't commit db transaction")
		return
	}

	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintln(w, "event processed successfully")
	_, _ = fmt.Fprintln(w)
}

// checkDebugPassword checks if the valid debug password was provided. If there is no password configured or the
// supplied password is incorrect, it sends an error code and returns false. True is returned if access is allowed.
func (l *Listener) checkDebugPassword(w http.ResponseWriter, r *http.Request) bool {
	expectedPassword := l.configFile.DebugPassword
	if expectedPassword == "" {
		w.WriteHeader(http.StatusForbidden)
		_, _ = fmt.Fprintln(w, "config dump disables, no debug-password set in config")

		return false
	}

	_, providedPassword, _ := r.BasicAuth()
	if subtle.ConstantTimeCompare([]byte(expectedPassword), []byte(providedPassword)) != 1 {
		l.logger.Warnw("Unauthorized request", zap.String("url", r.RequestURI))

		w.Header().Set("WWW-Authenticate", `Basic realm="debug"`)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprintln(w, "please provide the debug-password as basic auth credentials (user is ignored)")
		return false
	}

	return true
}

func (l *Listener) DumpConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		_, _ = fmt.Fprintln(w, "GET required")
		return
	}

	if !l.checkDebugPassword(w, r) {
		return
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(&l.runtimeConfig.ConfigSet)
}

func (l *Listener) DumpIncidents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		_, _ = fmt.Fprintln(w, "GET required")
		return
	}

	if !l.checkDebugPassword(w, r) {
		return
	}

	incidents := incident.GetCurrentIncidents()
	encodedIncidents := make(map[int64]json.RawMessage)

	// Extra function to ensure that unlocking happens in all cases, including panic.
	encode := func(incident *incident.Incident) json.RawMessage {
		incident.Lock()
		defer incident.Unlock()

		encoded, err := json.Marshal(incident)
		if err != nil {
			encoded, err = json.Marshal(err.Error())
			if err != nil {
				// If a string can't be marshalled, something is very wrong.
				panic(err)
			}
		}

		return encoded
	}

	for id, incident := range incidents {
		encodedIncidents[id] = encode(incident)
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(encodedIncidents)
}
