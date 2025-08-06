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
	"github.com/icinga/icinga-notifications/internal/rule"
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

	debugMux := http.NewServeMux()
	debugMux.HandleFunc("/dump-config", l.DumpConfig)
	debugMux.HandleFunc("/dump-incidents", l.DumpIncidents)
	debugMux.HandleFunc("/dump-schedules", l.DumpSchedules)
	debugMux.HandleFunc("/dump-rules", l.DumpRules)

	l.mux.Handle("/debug/", http.StripPrefix("/debug", l.requireDebugAuth(debugMux)))
	l.mux.HandleFunc("/process-event", l.ProcessEvent)
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
		Addr:        listenAddr,
		Handler:     l,
		ReadTimeout: 10 * time.Second,
		IdleTimeout: 30 * time.Second,
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

// sourceFromAuthOrAbort extracts a *config.Source from the HTTP Basic Auth. If the credentials are wrong, (nil, false) is
// returned and 401 was written back to the response writer.
func (l *Listener) sourceFromAuthOrAbort(w http.ResponseWriter, r *http.Request) (*config.Source, bool) {
	if authUser, authPass, authOk := r.BasicAuth(); authOk {
		src := l.runtimeConfig.GetSourceFromCredentials(authUser, authPass, l.logger)
		if src != nil {
			return src, true
		}
	}

	w.Header().Set("WWW-Authenticate", `Basic realm="icinga-notifications"`)
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = fmt.Fprintln(w, "please provide the debug-password as basic auth credentials (user is ignored)")
	return nil, false
}

func (l *Listener) ProcessEvent(w http.ResponseWriter, r *http.Request) {
	// abort the current connection by sending the status code and an error both to the log and back to the client.
	abort := func(statusCode int, ev *event.Event, format string, a ...any) {
		msg := format
		if len(a) > 0 {
			msg = fmt.Sprintf(format, a...)
		}

		logger := l.logger.With(zap.Int("status_code", statusCode), zap.String("message", msg))
		if ev != nil {
			logger = logger.With(zap.Stringer("event", ev))
		}

		http.Error(w, msg, statusCode)
		logger.Debugw("Abort listener submitted event processing")
	}

	if r.Method != http.MethodPost {
		abort(http.StatusMethodNotAllowed, nil, "POST required")
		return
	}

	source, validAuth := l.sourceFromAuthOrAbort(w, r)
	if !validAuth {
		return
	}

	ruleIdsStr := r.Header.Get(notifications.XIcingaRulesId)
	ruleVersion := r.Header.Get(notifications.XIcingaRulesVersion)

	// If the client uses an outdated rules version, reject the request but send also the current rules version
	// and rules for this source back to the client, so it can retry the request with the updated rules.
	if latestRuleVersion := l.runtimeConfig.GetRulesVersionFor(source.ID); ruleVersion != latestRuleVersion {
		w.WriteHeader(http.StatusFailedDependency)
		l.writeSourceRulesInfo(w, source)

		l.logger.Debugw("Abort event processing due to outdated rules version",
			zap.String("current_version", latestRuleVersion),
			zap.String("provided_version", ruleVersion),
			zap.String("source", source.Name))
		return
	}

	var ev event.Event
	if err := ev.LoadMatchedRulesFromString(ruleIdsStr); err != nil {
		abort(http.StatusBadRequest, nil, "cannot parse %s header: %v", notifications.XIcingaRulesId, err)
		return
	}

	if err := json.NewDecoder(r.Body).Decode(&ev); err != nil {
		abort(http.StatusBadRequest, nil, "cannot parse JSON body: %v", err)
		return
	}

	ev.Time = time.Now()
	ev.SourceId = source.ID
	if ev.Type == baseEv.TypeUnknown {
		ev.Type = baseEv.TypeState
	} else if !ev.Mute.Valid && ev.Type == baseEv.TypeMute {
		ev.SetMute(true, ev.MuteReason)
	} else if !ev.Mute.Valid && ev.Type == baseEv.TypeUnmute {
		ev.SetMute(false, ev.MuteReason)
	}

	if err := ev.Validate(); err != nil {
		abort(http.StatusBadRequest, &ev, err.Error())
		return
	}

	l.logger.Infow("Processing event", zap.String("event", ev.String()))
	err := incident.ProcessEvent(context.Background(), l.db, l.logs, l.runtimeConfig, &ev)
	if errors.Is(err, event.ErrSuperfluousStateChange) || errors.Is(err, event.ErrSuperfluousMuteUnmuteEvent) {
		abort(http.StatusNotAcceptable, &ev, "%v", err)
		return
	} else if err != nil {
		l.logger.Errorw("Failed to successfully process event", zap.Stringer("event", &ev), zap.Error(err))
		abort(http.StatusInternalServerError, &ev, "event could not be processed successfully, see server logs for details")
		return
	}

	l.logger.Infow("Successfully processed event", zap.String("event", ev.String()))

	w.WriteHeader(http.StatusAccepted)
	_, _ = fmt.Fprintln(w, "event processed successfully")
	_, _ = fmt.Fprintln(w)
}

// requireDebugAuth is a middleware that checks if the valid debug password was provided. If there is no password
// configured or the supplied password is incorrect, it sends an error code and does not redirect the request.
func (l *Listener) requireDebugAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expectedPassword := daemon.Config().DebugPassword
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

	l.runtimeConfig.RLock()
	rules := l.runtimeConfig.Rules
	l.runtimeConfig.RUnlock()

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(rules)
}

// writeSourceRulesInfo writes the rules information for a specific source to the response writer.
//
// It includes the rules version and a map of rule IDs to their corresponding rule objects.
func (l *Listener) writeSourceRulesInfo(w http.ResponseWriter, source *config.Source) {
	type Response struct {
		Version string
		Rules   map[int64]*rule.Rule
	}

	var resp Response
	resp.Version = l.runtimeConfig.GetRulesVersionFor(source.ID)

	func() { // Use a function to ensure that the RLock and RUnlock are called before writing the response.
		l.runtimeConfig.RLock()
		defer l.runtimeConfig.RUnlock()

		sourceInfo := l.runtimeConfig.RulesBySource[source.ID]
		if sourceInfo != nil {
			resp.Rules = make(map[int64]*rule.Rule, len(sourceInfo.RuleIDs))
			for _, rID := range sourceInfo.RuleIDs {
				resp.Rules[rID] = l.runtimeConfig.Rules[rID]
			}
		}
	}()

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(resp)
}
