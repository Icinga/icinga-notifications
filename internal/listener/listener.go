package listener

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/user"
	"strconv"
	"time"

	"github.com/icinga/icinga-go-library/database"
	"github.com/icinga/icinga-go-library/logging"
	"github.com/icinga/icinga-go-library/notifications"
	baseEv "github.com/icinga/icinga-go-library/notifications/event"
	"github.com/icinga/icinga-go-library/notifications/source"
	"github.com/icinga/icinga-notifications/internal"
	"github.com/icinga/icinga-notifications/internal/config"
	"github.com/icinga/icinga-notifications/internal/daemon"
	"github.com/icinga/icinga-notifications/internal/event"
	"github.com/icinga/icinga-notifications/internal/incident"
	"github.com/icinga/icinga-notifications/internal/object"
	"github.com/jmoiron/sqlx"
	"go.uber.org/zap"
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

// peerUserLookupKey is the context key under which a [peerUserLookupFunc] is stored for Unix socket connections.
type peerUserLookupKey struct{}

// peerUserLookupFunc resolves the OS username of the peer process connected via a Unix domain socket.
type peerUserLookupFunc func() (string, error)

type Listener struct {
	db            *database.DB
	logger        *logging.Logger
	runtimeConfig *config.RuntimeConfig

	logs      *logging.Logging
	mux       http.ServeMux
	useSocket bool
}

func NewListener(db *database.DB, runtimeConfig *config.RuntimeConfig, logs *logging.Logging, useSocket bool) *Listener {
	l := &Listener{
		db:            db,
		logger:        logs.GetChildLogger("listener"),
		logs:          logs,
		runtimeConfig: runtimeConfig,
		useSocket:     useSocket,
	}

	debugMux := http.NewServeMux()
	debugMux.HandleFunc("/dump-config", l.DumpConfig)
	debugMux.HandleFunc("/dump-incidents", l.DumpIncidents)
	debugMux.HandleFunc("/dump-schedules", l.DumpSchedules)
	debugMux.HandleFunc("/dump-rules", l.DumpRules)

	l.mux.Handle("/debug/", http.StripPrefix("/debug", l.requireDebugAuth(debugMux)))
	l.mux.HandleFunc("/process-event", l.ProcessEvent)
	l.mux.HandleFunc("/incidents", l.IncidentsHandler)
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

// Run starts the HTTP listener and blocks until the server finishes.
//
// An error is returned in all cases except for a graceful shutdown triggered by the context being done within a
// hardcoded time limit.
func (l *Listener) Run(ctx context.Context) error {
	stdlogger, err := zap.NewStdLogAt(l.logger.Desugar(), zap.ErrorLevel)
	if err != nil {
		return err
	}

	server := &http.Server{
		Handler:     l,
		ReadTimeout: 10 * time.Second,
		IdleTimeout: 30 * time.Second,
		// Redirect the standard library's HTTP server error log to our logger with error level because these
		// errors are usually unexpected and indicate a problem with the server that should be investigated.
		ErrorLog: stdlogger,
	}

	var listeningFunc func() error
	if l.useSocket {
		listeningFunc, err = l.initSocketServer(server)
	} else {
		listeningFunc, err = l.initTcpServer(server)
	}
	if err != nil {
		return err
	}

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- listeningFunc()
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

// initTcpServer configures the given HTTP(S) server for the configured TCP address and returns a function that
// starts serving. The caller is responsible for actually calling that function and for graceful shutdown.
func (l *Listener) initTcpServer(server *http.Server) (func() error, error) {
	conf := daemon.Config().Listener
	tlsConfig, err := conf.GetTlsConfig()
	if err != nil {
		return nil, err
	}

	var https string
	if conf.TLSOptions.Enable {
		https = "s"
	}
	l.logger.Infof("Starting listener on http%s://%s", https, conf.Addr)

	server.Addr = conf.Addr
	server.TLSConfig = tlsConfig

	if conf.TLSOptions.Enable {
		// We've already created the TLS config for the server, so we can pass empty strings
		// for certFile and keyFile, which makes ListenAndServeTLS use the TLS config directly
		// instead of trying to load certs from files.
		return func() error { return server.ListenAndServeTLS("", "") }, nil
	}

	return func() error { return server.ListenAndServe() }, nil
}

// initSocketServer configures the given HTTP server to listen on the configured Unix socket path and returns a
// function that starts serving. If a socket file already exists at the path, it is removed before binding.
// The caller is responsible for actually calling that function and for graceful shutdown.
func (l *Listener) initSocketServer(server *http.Server) (fn func() error, retErr error) {
	conf := daemon.Config().Listener
	mode := *conf.SocketMode
	if mode&0o006 > 0 {
		l.logger.Warnw("Unix socket is world-accessible; consider restricting socket_mode and using socket_group instead",
			zap.String("socket_mode", fmt.Sprintf("%04o", mode)),
		)
	}

	path := conf.Socket
	info, err := os.Stat(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("cannot read socket path: %w", err)
	} else if err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return nil, fmt.Errorf("the configured socket path already exists and is not a socket: %q", path)
		}
		if err := os.Remove(path); err != nil {
			return nil, fmt.Errorf("cannot remove existing unix socket: %w", err)
		}
	}

	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("cannot listen on unix socket %q: %w", path, err)
	}

	defer func() {
		if retErr != nil {
			_ = listener.Close()
		}
	}()

	if groupName := conf.SocketGroup; groupName != "" {
		group, err := user.LookupGroup(groupName)
		if err != nil {
			return nil, fmt.Errorf("cannot find group %q: %w", groupName, err)
		}

		gid, err := strconv.Atoi(group.Gid)
		if err != nil {
			return nil, fmt.Errorf("cannot parse GID for group %q: %w", groupName, err)
		}

		if gid != -1 {
			if err := os.Chown(path, -1, gid); err != nil {
				return nil, fmt.Errorf("cannot change ownership of unix socket %q: %w", path, err)
			}
		}
	}

	if err := os.Chmod(path, fs.FileMode(mode)); err != nil {
		return nil, fmt.Errorf("cannot set permissions on unix socket %q: %w", path, err)
	}

	server.ConnContext = func(ctx context.Context, c net.Conn) context.Context {
		unixConn, ok := c.(*net.UnixConn)
		if !ok {
			l.logger.Fatalw("expected *net.UnixConn", zap.String("connection_type", fmt.Sprintf("%T", c)))
		}

		rawConn, err := unixConn.SyscallConn()
		if err != nil {
			l.logger.Fatalw("Cannot extract RawConnection", zap.Error(err))
		}

		lookUpUser := func() (string, error) {
			uid, err := socketPeerUid(rawConn)
			if err != nil {
				return "", fmt.Errorf("cannot obtain peer credentials: %w", err)
			}

			u, err := user.LookupId(strconv.FormatUint(uint64(uid), 10))
			if err != nil {
				return "", fmt.Errorf("cannot obtain user id: %w", err)
			}

			return u.Username, nil
		}

		return context.WithValue(ctx, peerUserLookupKey{}, peerUserLookupFunc(lookUpUser))
	}

	l.logger.Infof("Starting listener on unix socket %s", path)

	return func() error { return server.Serve(listener) }, nil
}

// sourceFromAuthOrAbort extracts a *config.Source from the request. For Unix socket connections, the source is looked
// up by the OS username of the connecting process via peer credentials. For TCP connections, HTTP Basic Auth is used.
// If no matching source is found, nil is returned and 401 is written to the response.
func (l *Listener) sourceFromAuthOrAbort(w http.ResponseWriter, r *http.Request) (src *config.Source) {
	errFunc := func(errMsg string) *config.Source {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprintln(w, errMsg)
		return nil
	}

	if l.useSocket {
		l.logger.Debugw("Source is authenticated via socket connection")
		value := r.Context().Value(peerUserLookupKey{})
		if value == nil {
			return errFunc("no value found in context")
		}

		lookup, ok := value.(peerUserLookupFunc)
		if !ok {
			return errFunc("no lookup function present")
		}

		username, err := lookup()
		if err != nil {
			return errFunc(err.Error())
		}

		src = l.runtimeConfig.GetSourceByUsername(username)
		if src == nil {
			return errFunc(fmt.Sprintf("system user %q is not registered as a source username", username))
		}
	} else {
		l.logger.Debugw("Source is authenticated via HTTP Basic Auth")
		authUser, authPass, authOk := r.BasicAuth()
		if !authOk {
			w.Header().Set("WWW-Authenticate", `Basic realm="icinga-notifications source"`)
			return errFunc("missing or malformed basic auth credentials")
		}

		src = l.runtimeConfig.GetSourceFromCredentials(authUser, authPass, l.logger)
		if src == nil {
			w.Header().Set("WWW-Authenticate", `Basic realm="icinga-notifications source"`)
			return errFunc("expected valid icinga-notifications source basic auth credentials")
		}
	}

	return src
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

	src := l.sourceFromAuthOrAbort(w, r)
	if src == nil {
		// Listener.sourceFromAuthOrAbort writes 401 response by itself; no abort() necessary.
		return
	}

	var innerEv baseEv.Event
	if err := json.NewDecoder(r.Body).Decode(&innerEv); err != nil {
		l.abort(w, http.StatusBadRequest, nil, "cannot parse JSON body: %v", err)
		return
	}

	ev := event.Event{
		Time:     time.Now(),
		SourceId: src.ID,
		Event:    innerEv,
	}
	ev.CompleteURL(daemon.Config().IcingaWeb2UrlParsed)

	if err := ev.Validate(); err != nil {
		l.abort(w, http.StatusBadRequest, src, "%v", err)
		return
	}

	l.logger.Debugw("Received event", zap.String("source", src.Name), zap.Object("event", &ev))

	// Submitting an event without the "incident" field won't cause any new event rules to be evaluated or
	// escalations to be triggered, but only updates the state of an existing incident without a severity change.
	if ev.OpenOrEscalate() {
		filterColumns, hasRulesWithoutFilter := l.runtimeConfig.GetRulesFilterColumnsForSource(src)
		missingRelations := ev.ExtractMissingRelations(filterColumns...)
		if len(missingRelations) > 0 && ShouldRejectRequestOnIncompleteRelations(r, hasRulesWithoutFilter) {
			l.sendMissingAttrsError(w, src, missingRelations)
			return
		}
	}

	// Enqueue the event restricted to the HTTP request context, and restrict it further to well-guessed ten seconds.
	//
	// Doing so reduces the chance that event queueing works, while the HTTP client disappears. Unfortunately, this
	// cannot be done in reverse - like with a callback, writing the HTTP response within the transaction -, as at this
	// moment it is unknown if the transaction will succeed. This approach binds the transaction to the request context
	// and has a minimum interval between COMMIT and writing back a response.
	//
	// Of course, the client might disappear without disconnecting. Unless we have some super aggressive TCP heartbeat,
	// this will fall through. But, for the moment, I am totally fine with this.
	//
	// Nevertheless, event.Event submissions should be unique, especially due to the Event.ID field. Thus, if a client
	// resubmits an identical event, its event.Queue.ID should be identical as well, resulting in a no-op.
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	err := event.Enqueue(ctx, l.db, &ev, object.ID(ev.SourceId, ev.Tags))
	if err != nil {
		l.logger.Errorw("Failed to enqueue event into event queue",
			zap.String("source", src.Name),
			zap.String("event_name", ev.Name),
			zap.Error(err))

		l.abort(w, http.StatusInternalServerError, src, "internal error: see server logs for details")
		return
	}

	l.logger.Debugw("Successfully enqueued event for future processing",
		zap.String("source", src.Name),
		zap.String("event_name", ev.Name))

	w.WriteHeader(http.StatusAccepted)
	_, _ = fmt.Fprintln(w, "event accepted for processing")
	_, _ = fmt.Fprintln(w)
}

// IncidentsHandler handles GET and POST requests to the /incidents endpoint.
//
// It performs authentication using HTTP Basic Auth and delegates the actual handling to [Listener.getIncidents]
// for GET requests and [Listener.modifyIncident] for POST requests. If the request method is neither GET nor POST,
// it responds with a 405 Method Not Allowed status.
func (l *Listener) IncidentsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		l.abort(w, http.StatusMethodNotAllowed, nil, "GET or POST required")
		return
	}

	src := l.sourceFromAuthOrAbort(w, r)
	if src == nil {
		// Listener.sourceFromAuthOrAbort writes 401 response by itself; no abort() necessary.
		return
	}

	qs := r.URL.Query().Get("filter")
	if qs == "" {
		l.abort(w, http.StatusBadRequest, nil, "missing required filter query parameter")
		return
	}

	filter, err := ParseQueryFilter(qs)
	if err != nil {
		l.logger.Warnw("Error parsing filter", zap.String("qs", qs), zap.Error(err))
		l.abort(w, http.StatusBadRequest, src, "failed to parse provided query string: %v", err)
		return
	}

	if r.Method == http.MethodGet {
		l.getIncidents(w, src, filter)
	} else {
		l.modifyIncidents(w, r, src, filter)
	}
}

// modifyIncidents handles POST requests to the /incidents endpoint.
//
// It retrieves the current incidents for the authenticated source, applies any filters provided in the query params,
// and modifies the filtered incidents based on the JSON body of the request. The JSON body can contain a "message"
// field to update the incident message and a "close" field to close the incident. If there is an error parsing the
// filters or evaluating them against the incidents, it responds with an appropriate HTTP status code and error message.
func (l *Listener) modifyIncidents(w http.ResponseWriter, r *http.Request, src *config.Source, filter any) {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	var attrs source.ModifiableIncidentAttrs
	if err := dec.Decode(&attrs); err != nil {
		l.abort(w, http.StatusBadRequest, src, "cannot parse JSON body: %v", err)
		return
	}

	if err := attrs.Validate(); err != nil {
		l.abort(w, http.StatusBadRequest, src, "invalid request body: %v", err)
		return
	}

	var results []any
	status := http.StatusOK
	for _, i := range incident.GetCurrentIncidentsForSource(src.ID) {
		if match, err := EvaluateQueryFilter(filter, i.Object.Tags); err != nil {
			l.abort(w, http.StatusBadRequest, src, "invalid query string filter: %v", err)
			return
		} else if match {
			err := l.db.ExecTx(r.Context(), nil, func(ctx context.Context, tx *sqlx.Tx) error {
				i.Lock()
				defer i.Unlock()

				// Note, currently if we fail to commit the tx, the incident will be left in a modified state in
				// memory, but not in the database. This is a known limitation, but it is acceptable for the time
				// being until https://github.com/Icinga/icinga-notifications/pull/463 gets merged.
				if attrs.Message.Valid {
					i.Message = attrs.Message
				}

				if attrs.Close.Valid {
					return i.Close(ctx, tx, true)
				}
				return i.Sync(ctx, tx)
			})
			if err != nil {
				l.logger.Errorw("Failed to modify incident", zap.String("source", src.Name), zap.Error(err))
				status = http.StatusInternalServerError
				results = append(results, map[string]any{
					"object_tags": i.Object.Tags,
					"code":        status,
					"status":      "failed to modify incident, see server logs for details",
				})
			} else {
				results = append(results, map[string]any{
					"object_tags": i.Object.Tags,
					"code":        http.StatusOK,
					"status":      "incident modified successfully",
				})
			}
		}
	}

	w.Header().Add("Content-Type", "application/json")
	w.WriteHeader(status)

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(results); err != nil {
		l.logger.Errorw("Failed to serialize modify incidents response", zap.String("source", src.Name), zap.Error(err))
	}
}

// getIncidents handles GET requests to the /incidents endpoint.
//
// It retrieves the current incidents for the authenticated source, applies any filters provided in the query
// parameters, and returns the filtered incidents as a JSON response. If there is an error parsing the filters
// or evaluating them against the incidents, it responds with an appropriate HTTP status code and error message.
//
// The filters are evaluated against the object tags of each incident, and only incidents that match the filters
// are included in the response.
func (l *Listener) getIncidents(w http.ResponseWriter, src *config.Source, filter any) {
	incidents := incident.GetCurrentIncidentsForSource(src.ID)
	result := make([]*source.Incident, 0, len(incidents))
	for _, inc := range incidents {
		if match, err := EvaluateQueryFilter(filter, inc.Object.Tags); err != nil {
			l.abort(w, http.StatusBadRequest, src, "%+v", err)
			return
		} else if match {
			inc.Lock()
			result = append(result, &source.Incident{
				IsMuted:    inc.IsMuted(),
				ObjectTags: inc.Object.Tags,
				Severity:   inc.Severity,
			})
			inc.Unlock()
		}
	}
	w.Header().Add("Content-Type", "application/json")

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(result); err != nil {
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
// [notifications.XIcingaRejectIfRelationsIncomplete] HTTP header. Otherwise, if there are rules without a filter,
// it returns false as such rules match unconditionally and thus don't necessarily require the missing relations.
//
// Note that this function assumes that it's guarded by a [baseEv.Event.OpenOrEscalate] check, since only
// events that open or escalate an incident are subject to relation completeness checks in the first place.
func ShouldRejectRequestOnIncompleteRelations(r *http.Request, hasRulesWithoutFilter bool) bool {
	if r.Header.Get(notifications.XIcingaRejectIfRelationsIncomplete) == "true" {
		return true
	}
	if hasRulesWithoutFilter {
		return false
	}
	return true
}
