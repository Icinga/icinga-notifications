package listener

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/icinga/icinga-go-library/database"
	"github.com/icinga/icinga-go-library/logging"
	"github.com/icinga/icinga-go-library/notifications"
	baseEv "github.com/icinga/icinga-go-library/notifications/event"
	"github.com/icinga/icinga-notifications/internal"
	"github.com/icinga/icinga-notifications/internal/config"
	"github.com/icinga/icinga-notifications/internal/daemon"
	"github.com/icinga/icinga-notifications/internal/event"
	"github.com/icinga/icinga-notifications/internal/incident"
	"github.com/icinga/icinga-notifications/internal/object"
	"go.uber.org/zap"
	"net/http"
	"time"
)

// responseWriter is a wrapper around [http.ResponseWriter] that captures the status code written to the response.
//
// Note: This struct satisfies only the [http.ResponseWriter] interface, but not the optional [http.Flusher],
// [http.Hijacker], and [http.Pusher] interfaces. If the underlying [http.ResponseWriter] implements any of
// these interfaces, the caller must use the [http.ResponseWriter] directly to access them.
type responseWriter struct {
	http.ResponseWriter
	status int
}

// WriteHeader captures the status code and writes it to the underlying [ResponseWriter].
func (rw *responseWriter) WriteHeader(status int) {
	rw.status = status
	rw.ResponseWriter.WriteHeader(status)
}

type Listener struct {
	db            *database.DB
	logger        *logging.Logger
	runtimeConfig *config.RuntimeConfig

	logs *logging.Logging
	mux  http.ServeMux
}

func NewListener(db *database.DB, runtimeConfig *config.RuntimeConfig, logs *logging.Logging) *Listener {
	l := &Listener{
		db:            db,
		logger:        logs.GetChildLogger("listener"),
		logs:          logs,
		runtimeConfig: runtimeConfig,
	}

	debugMux := http.NewServeMux()
	debugMux.HandleFunc("/dump-config", l.DumpConfig)
	debugMux.HandleFunc("/dump-incidents", l.DumpIncidents)
	debugMux.HandleFunc("/dump-schedules", l.DumpSchedules)
	debugMux.HandleFunc("/dump-rules", l.DumpRules)

	l.mux.Handle("/debug/", http.StripPrefix("/debug", l.requireDebugAuth(debugMux)))
	l.mux.HandleFunc("/process-event", l.ProcessEvent)
	l.mux.HandleFunc("/incidents", l.GetIncidents)
	return l
}

// ServeHTTP implements the [http.Handler] interface for the Listener, allowing it to be used as an actual HTTP handler.
//
// It just sets a Server header and then delegates the request handling to the internal [http.ServeMux].
// The status code of the response is captured and logged together with other request information after
// the request has been handled.
func (l *Listener) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	rw.Header().Set("Server", "icinga-notifications/"+internal.Version.Version)
	// Default to OK, so that if a handler just writes to the response without explicitly setting a status,
	// we still log the request as successful instead of logging it with a misleading 0 status code.
	crw := &responseWriter{ResponseWriter: rw, status: http.StatusOK}
	l.mux.ServeHTTP(crw, req)

	// We're not using a `defer` here because we don't actually care logging these info if the handler panics,
	// so we want to let the panic propagate instead of logging a potentially misleading request log with OK status.
	l.logger.Debugw("Handled request",
		zap.String("method", req.Method),
		zap.String("target_url", req.RequestURI),
		zap.String("remote_addr", req.RemoteAddr),
		zap.String("user_agent", req.UserAgent()),
		zap.String("status", http.StatusText(crw.status)))
}

// Run the Listener's web server and block until the server has finished.
//
// The web server either returns (early) when its ListenAndServe fails or when the given context is finished. After the
// context is done, the web server shuts down gracefully with a hard limit of three seconds.
//
// An error is returned in every case except for a gracefully context-based shutdown without hitting the time limit.
func (l *Listener) Run(ctx context.Context) error {
	conf := daemon.Config().Listener
	tlsConfig, err := conf.GetTlsConfig()
	if err != nil {
		return err
	}

	var https string
	if conf.TLSOptions.Enable {
		https = "s"
	}
	l.logger.Infof("Starting listener on http%s://%s", https, conf.Addr)

	stdlogger, err := zap.NewStdLogAt(l.logger.Desugar(), zap.ErrorLevel)
	if err != nil {
		return err
	}

	server := &http.Server{
		Addr:        conf.Addr,
		Handler:     l,
		TLSConfig:   tlsConfig,
		ReadTimeout: 10 * time.Second,
		IdleTimeout: 30 * time.Second,
		// Redirect the standard library's HTTP server error log to our logger with error level because these
		// errors are usually unexpected and indicate a problem with the server that should be investigated.
		ErrorLog: stdlogger,
	}

	serverErr := make(chan error, 1)
	go func() {
		if conf.TLSOptions.Enable {
			// We've already created the TLS config for the server, so we can pass empty strings
			// for certFile and keyFile, which makes ListenAndServeTLS use the TLS config directly
			// instead of trying to load certs from files.
			serverErr <- server.ListenAndServeTLS("", "")
		} else {
			serverErr <- server.ListenAndServe()
		}
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)

	case err := <-serverErr:
		return err
	}
}

// sourceFromAuthOrAbort extracts a *config.Source from the HTTP Basic Auth. If the credentials are wrong, (nil, false) is
// returned and 401 was written back to the response writer.
func (l *Listener) sourceFromAuthOrAbort(w http.ResponseWriter, r *http.Request) (*config.Source, bool) {
	if authUser, authPass, authOk := r.BasicAuth(); authOk {
		src := l.runtimeConfig.GetSourceFromCredentials(authUser, authPass, l.logger)
		if src != nil {
			return src, true
		}
	}

	w.Header().Set("WWW-Authenticate", `Basic realm="icinga-notifications source"`)
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = fmt.Fprintln(w, "expected valid icinga-notifications source basic auth credentials")
	return nil, false
}

// abort the current connection by sending the status code and an error both to the log and back to the client.
func (l *Listener) abort(w http.ResponseWriter, statusCode int, src *config.Source, format string, a ...any) {
	msg := format
	if len(a) > 0 {
		msg = fmt.Sprintf(format, a...)
	}

	logger := l.logger.With(zap.Int("status_code", statusCode), zap.String("message", msg))
	if src != nil {
		logger = logger.With(zap.String("source", src.Name))
	}

	http.Error(w, msg, statusCode)
	logger.Debugw("Abort listener submitted event processing")
}

func (l *Listener) ProcessEvent(w http.ResponseWriter, r *http.Request) {
	// abort the current connection by sending the status code and an error both to the log and back to the client.
	if r.Method != http.MethodPost {
		l.abort(w, http.StatusMethodNotAllowed, nil, "POST required")
		return
	}

	src, isAuthenticated := l.sourceFromAuthOrAbort(w, r)
	if !isAuthenticated {
		// Listener.sourceFromAuthOrAbort writes 401 response by itself; no abort() necessary.
		return
	}

	var ev event.Event
	if err := json.NewDecoder(r.Body).Decode(&ev); err != nil {
		l.abort(w, http.StatusBadRequest, nil, "cannot parse JSON body: %v", err)
		return
	}

	ev.CompleteURL(daemon.Config().Icingaweb2URL)
	ev.Time = time.Now()
	ev.SourceId = src.ID
	if ev.Type == baseEv.TypeUnknown {
		ev.Type = baseEv.TypeState
	} else if !ev.Mute.Valid && ev.Type == baseEv.TypeMute {
		ev.SetMute(true, ev.MuteReason)
	} else if !ev.Mute.Valid && ev.Type == baseEv.TypeUnmute {
		ev.SetMute(false, ev.MuteReason)
	}

	if err := ev.Validate(); err != nil {
		l.abort(w, http.StatusBadRequest, src, "%v", err)
		return
	}

	l.logger.Debugw("Processing event", zap.String("source", src.Name), zap.Object("event", &ev))

	filterColumns, hasRulesWithoutFilter := l.runtimeConfig.GetRulesFilterColumnsForSource(src)
	missingRelations := ev.ExtractMissingRelations(filterColumns...)
	if len(missingRelations) > 0 && ShouldRejectRequestOnIncompleteRelations(r, &ev, hasRulesWithoutFilter) {
		l.sendMissingAttrsError(w, src, missingRelations)
		return
	}

	err := incident.ProcessEvent(context.Background(), l.db, l.logs, l.runtimeConfig, &ev)
	if errors.Is(err, event.ErrSuperfluousStateChange) || errors.Is(err, event.ErrSuperfluousMuteUnmuteEvent) {
		l.abort(w, http.StatusNotAcceptable, src, "%v", err)
		return
	} else if err != nil {
		l.logger.Errorw("Failed to successfully process event", zap.String("source", src.Name), zap.Error(err))
		l.abort(w, http.StatusInternalServerError, src, "event could not be processed successfully, see server logs for details")
		return
	}

	l.logger.Infow("Successfully processed event", zap.String("source", src.Name))

	w.WriteHeader(http.StatusAccepted)
	_, _ = fmt.Fprintln(w, "event processed successfully")
	_, _ = fmt.Fprintln(w)
}

func (l *Listener) GetIncidents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		l.abort(w, http.StatusMethodNotAllowed, nil, "POST required")
		return
	}

	src, isAuthenticated := l.sourceFromAuthOrAbort(w, r)
	if !isAuthenticated {
		// Listener.sourceFromAuthOrAbort writes 401 response by itself; no abort() necessary.
		return
	}

	// Temporary struct type to use for incident serialization. The Incident type itself isn't directly passed to the
	// JSON because that returns all fields for the DumpIncidents() debug endpoint.
	type SerializableIncident struct {
		Incident   string            `json:"incident"`
		ObjectTags map[string]string `json:"object_tags"`
		Severity   baseEv.Severity   `json:"severity"`
	}

	incidents := incident.GetCurrentIncidentsForSource(src.ID)
	result := make([]*SerializableIncident, 0, len(incidents))
	for _, inc := range incidents {
		if inc.Object.SourceID == src.ID {
			inc.Lock()
			result = append(result, &SerializableIncident{
				Incident:   inc.String(),
				ObjectTags: inc.Object.Tags,
				Severity:   inc.Severity,
			})
			inc.Unlock()
		}
	}
	w.Header().Add("Content-Type", "application/json")

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	err := enc.Encode(result)
	if err != nil {
		l.logger.Errorw("Failed to serialize incidents for source", zap.Object("source", src), zap.Error(err))
		return
	}
}

// requireDebugAuth is a middleware that checks if the valid debug password was provided. If there is no password
// configured or the supplied password is incorrect, it sends an error code and does not redirect the request.
func (l *Listener) requireDebugAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expectedPassword := daemon.Config().Listener.DebugPassword
		if expectedPassword == "" {
			w.WriteHeader(http.StatusForbidden)
			_, _ = fmt.Fprintln(w, "config dump disabled, no debug-password set in config")

			return
		}

		_, providedPassword, _ := r.BasicAuth()
		if subtle.ConstantTimeCompare([]byte(expectedPassword), []byte(providedPassword)) != 1 {
			l.logger.Warnw("Unauthorized request", zap.String("url", r.RequestURI))

			w.Header().Set("WWW-Authenticate", `Basic realm="debug"`)
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = fmt.Fprintln(w, "please provide the debug-password as basic auth credentials (user is ignored)")
			return
		}

		next.ServeHTTP(w, r)
	})
}

// DumpConfig is used as /debug prefixed endpoint to dump the current live configuration of the daemon.
// The authorization has to be done beforehand.
func (l *Listener) DumpConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		_, _ = fmt.Fprintln(w, "GET required")
		return
	}
	w.Header().Add("Content-Type", "application/json")

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(&l.runtimeConfig.ConfigSet)
}

// DumpIncidents is used as /debug prefixed endpoint to dump all incidents. The authorization has to be done beforehand.
func (l *Listener) DumpIncidents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		_, _ = fmt.Fprintln(w, "GET required")
		return
	}
	w.Header().Add("Content-Type", "application/json")

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

// DumpSchedules is used as /debug prefixed endpoint to dump all schedules. The authorization has to be done beforehand.
func (l *Listener) DumpSchedules(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		_, _ = fmt.Fprintln(w, "GET required")
		return
	}

	l.runtimeConfig.RLock()
	defer l.runtimeConfig.RUnlock()

	for _, schedule := range l.runtimeConfig.Schedules {
		_, _ = fmt.Fprintf(w, "[id=%d] %q:\n", schedule.ID, schedule.Name)

		// Iterate in 30 minute steps as this is the granularity Icinga Notifications Web allows in the configuration.
		// Truncation to seconds happens only for a more readable output.
		step := 30 * time.Minute
		start := time.Now().Truncate(time.Second)
		for t := start; t.Before(start.Add(48 * time.Hour)); t = t.Add(step) {
			_, _ = fmt.Fprintf(w, "\t%v: %v\n", t, schedule.GetContactsAt(t))
		}

		_, _ = fmt.Fprintln(w)
	}
}

// DumpRules is used as /debug prefixed endpoint to dump all rules. The authorization has to be done beforehand.
func (l *Listener) DumpRules(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		_, _ = fmt.Fprintln(w, "GET required")
		return
	}
	w.Header().Add("Content-Type", "application/json")

	l.runtimeConfig.RLock()
	defer l.runtimeConfig.RUnlock()

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(l.runtimeConfig.Rules)
}

// sendMissingAttrsError sends a response with status code 422 Unprocessable Entity to the client.
func (l *Listener) sendMissingAttrsError(w http.ResponseWriter, src *config.Source, missingAttrs []string) {
	l.logger.Debugw(
		"Event is missing attributes required for rule evaluation",
		zap.String("source", src.Name),
		zap.Strings("missing_attributes", missingAttrs),
	)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnprocessableEntity)

	resp := map[string]any{
		"type":       "attribute negotiation",
		"attributes": missingAttrs,
	}

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		l.logger.Errorw("Failed to send missing attributes required for rule evaluation", zap.Error(err))
		return
	}
}

// ShouldRejectRequestOnIncompleteRelations determines whether a request with incomplete relations should be rejected.
//
// This function always returns true if the client explicitly requested to reject such events by setting the
// [notifications.XIcingaRejectIfRelationsIncomplete] HTTP header. Otherwise, it only returns true when the
// src doesn't have any rules without an object filter and the event doesn't cause a new incident to be opened
// and there's no active one yet for the event's source object.
func ShouldRejectRequestOnIncompleteRelations(r *http.Request, ev *event.Event, hasRulesWithoutFilter bool) bool {
	if r.Header.Get(notifications.XIcingaRejectIfRelationsIncomplete) == "true" {
		return true
	}
	if hasRulesWithoutFilter {
		return false
	}
	return !incident.CanOpenNewIncident(ev) && !incident.HasCurrent(object.GetFromCache(object.ID(ev.SourceId, ev.Tags)))
}
