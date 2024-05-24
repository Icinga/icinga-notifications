package listener

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"github.com/icinga/icinga-go-library/database"
	"github.com/icinga/icinga-go-library/logging"
	"github.com/icinga/icinga-notifications/internal"
	"github.com/icinga/icinga-notifications/internal/config"
	"github.com/icinga/icinga-notifications/internal/daemon"
	"github.com/icinga/icinga-notifications/internal/event"
	"github.com/icinga/icinga-notifications/internal/incident"
	"go.uber.org/zap"
	"net/http"
	"time"
)

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
	l.mux.HandleFunc("/process-event", l.ProcessEvent)
	l.mux.HandleFunc("/dump-config", l.DumpConfig)
	l.mux.HandleFunc("/dump-incidents", l.DumpIncidents)
	l.mux.HandleFunc("/dump-schedules", l.DumpSchedules)
	return l
}

func (l *Listener) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	rw.Header().Set("Server", "icinga-notifications/"+internal.Version.Version)
	l.mux.ServeHTTP(rw, req)
}

// Run the Listener's web server and block until the server has finished.
//
// The web server either returns (early) when its ListenAndServe fails or when the given context is finished. After the
// context is done, the web server shuts down gracefully with a hard limit of three seconds.
//
// An error is returned in every case except for a gracefully context-based shutdown without hitting the time limit.
func (l *Listener) Run(ctx context.Context) error {
	listenAddr := daemon.Config().Listen
	l.logger.Infof("Starting listener on http://%s", listenAddr)
	server := &http.Server{
		Addr:         listenAddr,
		Handler:      l,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  30 * time.Second,
	}

	serverErr := make(chan error)
	go func() {
		serverErr <- server.ListenAndServe()
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

func (l *Listener) ProcessEvent(w http.ResponseWriter, req *http.Request) {
	// abort the current connection by sending the status code and an error both to the log and back to the client.
	abort := func(statusCode int, ev *event.Event, format string, a ...any) {
		msg := format
		if len(a) > 0 {
			msg = fmt.Sprintf(format, a...)
		}

		logger := l.logger.With(zap.Int("status-code", statusCode), zap.String("msg", msg))
		if ev != nil {
			logger = logger.With(zap.Stringer("event", ev))
		}

		http.Error(w, msg, statusCode)
		logger.Debugw("Abort listener submitted event processing")
	}

	if req.Method != http.MethodPost {
		abort(http.StatusMethodNotAllowed, nil, "POST required")
		return
	}

	var source *config.Source
	if authUser, authPass, authOk := req.BasicAuth(); authOk {
		source = l.runtimeConfig.GetSourceFromCredentials(authUser, authPass, l.logger)
	}
	if source == nil {
		w.Header().Set("WWW-Authenticate", `Basic realm="icinga-notifications"`)
		abort(http.StatusUnauthorized, nil, "HTTP authorization required")
		return
	}

	var ev event.Event
	err := json.NewDecoder(req.Body).Decode(&ev)
	if err != nil {
		abort(http.StatusBadRequest, nil, "cannot parse JSON body: %v", err)
		return
	}

	ev.Time = time.Now()
	ev.SourceId = source.ID
	if ev.Type == "" {
		ev.Type = event.TypeState
	} else if !ev.Mute.Valid && ev.Type == event.TypeMute {
		ev.SetMute(true, ev.MuteReason)
	} else if !ev.Mute.Valid && ev.Type == event.TypeUnmute {
		ev.SetMute(false, ev.MuteReason)
	}

	if err := ev.Validate(); err != nil {
		abort(http.StatusBadRequest, &ev, err.Error())
		return
	}

	l.logger.Infow("Processing event", zap.String("event", ev.String()))
	err = incident.ProcessEvent(context.Background(), l.db, l.logs, l.runtimeConfig, &ev)
	if err != nil {
		abort(http.StatusInternalServerError, &ev, err.Error())
		return
	}

	l.logger.Infow("Successfully processed event", zap.String("event", ev.String()))

	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintln(w, "event processed successfully")
	_, _ = fmt.Fprintln(w)
}

// checkDebugPassword checks if the valid debug password was provided. If there is no password configured or the
// supplied password is incorrect, it sends an error code and returns false. True is returned if access is allowed.
func (l *Listener) checkDebugPassword(w http.ResponseWriter, r *http.Request) bool {
	expectedPassword := daemon.Config().DebugPassword
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

func (l *Listener) DumpSchedules(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		_, _ = fmt.Fprintln(w, "GET required")
		return
	}

	if !l.checkDebugPassword(w, r) {
		return
	}

	l.runtimeConfig.RLock()
	defer l.runtimeConfig.RUnlock()

	for _, schedule := range l.runtimeConfig.Schedules {
		fmt.Fprintf(w, "[id=%d] %q:\n", schedule.ID, schedule.Name)

		// Iterate in 30 minute steps as this is the granularity Icinga Notifications Web allows in the configuration.
		// Truncation to seconds happens only for a more readable output.
		step := 30 * time.Minute
		start := time.Now().Truncate(time.Second)
		for t := start; t.Before(start.Add(48 * time.Hour)); t = t.Add(step) {
			fmt.Fprintf(w, "\t%v: %v\n", t, schedule.GetContactsAt(t))
		}

		fmt.Fprintln(w)
	}
}
