package eventstream

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"github.com/icinga/icinga-notifications/internal/config"
	"github.com/icinga/icinga-notifications/internal/daemon"
	"github.com/icinga/icinga-notifications/internal/event"
	"github.com/icinga/icingadb/pkg/icingadb"
	"github.com/icinga/icingadb/pkg/logging"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
	"net/http"
	"net/url"
	"os"
	"time"
)

// This file contains the main resp. common methods for the Client.

// eventMsg is an internal struct for passing events with additional information from producers to the dispatcher.
type eventMsg struct {
	event   *event.Event
	apiTime time.Time
}

// Client for the Icinga 2 Event Stream API with support for other Icinga 2 APIs to gather additional information and
// perform a catch-up of unknown events either when starting up to or in case of a connection loss.
//
// Within the icinga-notifications scope, one or multiple Client instances can be generated from the configuration by
// calling NewClientsFromConfig.
//
// A Client must be started by calling its Process method, which blocks until Ctx is marked as done. Reconnections and
// the necessary state replaying in an internal catch-up-phase from the Icinga 2 API will be taken care off. Internally,
// the Client executes a worker within its own goroutine, which dispatches event.Event to the CallbackFn and enforces
// order during catching up after (re-)connections.
type Client struct {
	// ApiHost et al. configure where and how the Icinga 2 API can be reached.
	ApiHost          string
	ApiBasicAuthUser string
	ApiBasicAuthPass string
	ApiHttpTransport http.Transport

	// IcingaNotificationsEventSourceId to be reflected in generated event.Events.
	IcingaNotificationsEventSourceId int64
	// IcingaWebRoot points to the Icinga Web 2 endpoint for generated URLs.
	IcingaWebRoot string

	// CallbackFn receives generated event.Event objects.
	CallbackFn func(*event.Event)
	// Ctx for all web requests as well as internal wait loops.
	Ctx context.Context
	// Logger to log to.
	Logger *logging.Logger

	// eventDispatcherEventStream communicates Events to be processed from the Event Stream API.
	eventDispatcherEventStream chan *eventMsg
	// catchupPhaseRequest requests the main worker to switch to the catch-up-phase to query the API for missed events.
	catchupPhaseRequest chan struct{}
}

// NewClientsFromConfig returns all Clients defined in the conf.ConfigFile.
//
// Those are prepared and just needed to be started by calling their Client.Process method.
func NewClientsFromConfig(
	ctx context.Context,
	logs *logging.Logging,
	db *icingadb.DB,
	runtimeConfig *config.RuntimeConfig,
	conf *daemon.ConfigFile,
) ([]*Client, error) {
	clients := make([]*Client, 0, len(conf.Icinga2Apis))

	for _, icinga2Api := range conf.Icinga2Apis {
		logger := logs.GetChildLogger(fmt.Sprintf("eventstream-%d", icinga2Api.NotificationsEventSourceId))
		callbackLogger := logs.GetChildLogger(fmt.Sprintf("eventstream-callback-%d", icinga2Api.NotificationsEventSourceId))

		client := &Client{
			ApiHost:          icinga2Api.Host,
			ApiBasicAuthUser: icinga2Api.AuthUser,
			ApiBasicAuthPass: icinga2Api.AuthPass,
			ApiHttpTransport: http.Transport{
				// Hardened TLS config adjusted to Icinga 2's configuration:
				// - https://icinga.com/docs/icinga-2/latest/doc/09-object-types/#objecttype-apilistener
				// - https://icinga.com/docs/icinga-2/latest/doc/12-icinga2-api/#security
				// - https://ssl-config.mozilla.org/#server=go&config=intermediate
				TLSClientConfig: &tls.Config{
					MinVersion: tls.VersionTLS12,
					CipherSuites: []uint16{
						tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
						tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
						tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
						tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
						tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
						tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
					},
				},
			},

			IcingaNotificationsEventSourceId: icinga2Api.NotificationsEventSourceId,
			IcingaWebRoot:                    conf.Icingaweb2URL,

			CallbackFn: makeProcessEvent(ctx, db, callbackLogger, logs, runtimeConfig),
			Ctx:        ctx,
			Logger:     logger,
		}

		if icinga2Api.IcingaCaFile != "" {
			caData, err := os.ReadFile(icinga2Api.IcingaCaFile)
			if err != nil {
				return nil, fmt.Errorf("cannot read CA file %q for Event Stream ID %d, %w",
					icinga2Api.IcingaCaFile, icinga2Api.NotificationsEventSourceId, err)
			}

			certPool := x509.NewCertPool()
			if !certPool.AppendCertsFromPEM(caData) {
				return nil, fmt.Errorf("cannot add custom CA file to CA pool for Event Stream ID %d, %w",
					icinga2Api.NotificationsEventSourceId, err)
			}

			client.ApiHttpTransport.TLSClientConfig.RootCAs = certPool
		}

		if icinga2Api.InsecureTls {
			client.ApiHttpTransport.TLSClientConfig.InsecureSkipVerify = true
		}

		clients = append(clients, client)
	}
	return clients, nil
}

// buildCommonEvent creates an event.Event based on Host and (optional) Service attributes to be specified later.
//
// The new Event's Time will be the current timestamp.
//
// The following fields will NOT be populated and might be altered later:
//   - Type
//   - Severity
//   - Username
//   - Message
//   - ID
func (client *Client) buildCommonEvent(ctx context.Context, host, service string) (*event.Event, error) {
	var (
		eventName      string
		eventUrl       *url.URL
		eventTags      map[string]string
		eventExtraTags = make(map[string]string)
	)

	eventUrl, err := url.Parse(client.IcingaWebRoot)
	if err != nil {
		return nil, err
	}

	if service != "" {
		eventName = host + "!" + service

		eventUrl = eventUrl.JoinPath("/icingadb/service")
		eventUrl.RawQuery = "name=" + rawurlencode(service) + "&host.name=" + rawurlencode(host)

		eventTags = map[string]string{
			"host":    host,
			"service": service,
		}

		serviceGroups, err := client.fetchHostServiceGroups(ctx, host, service)
		if err != nil {
			return nil, err
		}
		for _, serviceGroup := range serviceGroups {
			eventExtraTags["servicegroup/"+serviceGroup] = ""
		}
	} else {
		eventName = host

		eventUrl = eventUrl.JoinPath("/icingadb/host")
		eventUrl.RawQuery = "name=" + rawurlencode(host)

		eventTags = map[string]string{
			"host": host,
		}
	}

	hostGroups, err := client.fetchHostServiceGroups(ctx, host, "")
	if err != nil {
		return nil, err
	}
	for _, hostGroup := range hostGroups {
		eventExtraTags["hostgroup/"+hostGroup] = ""
	}

	return &event.Event{
		Time:      time.Now(),
		SourceId:  client.IcingaNotificationsEventSourceId,
		Name:      eventName,
		URL:       eventUrl.String(),
		Tags:      eventTags,
		ExtraTags: eventExtraTags,
	}, nil
}

// buildHostServiceEvent constructs an event.Event based on a CheckResult, a Host or Service state, a Host name and an
// optional Service name if the Event should represent a Service object.
func (client *Client) buildHostServiceEvent(ctx context.Context, result CheckResult, state int, host, service string) (*event.Event, error) {
	var eventSeverity event.Severity

	if service != "" {
		switch state {
		case 0: // OK
			eventSeverity = event.SeverityOK
		case 1: // WARNING
			eventSeverity = event.SeverityWarning
		case 2: // CRITICAL
			eventSeverity = event.SeverityCrit
		default: // UNKNOWN or faulty
			eventSeverity = event.SeverityErr
		}
	} else {
		switch state {
		case 0: // UP
			eventSeverity = event.SeverityOK
		case 1: // DOWN
			eventSeverity = event.SeverityCrit
		default: // faulty
			eventSeverity = event.SeverityErr
		}
	}

	ev, err := client.buildCommonEvent(ctx, host, service)
	if err != nil {
		return nil, err
	}

	ev.Type = event.TypeState
	ev.Severity = eventSeverity
	ev.Message = result.Output

	return ev, nil
}

// buildAcknowledgementEvent from the given fields.
func (client *Client) buildAcknowledgementEvent(ctx context.Context, host, service, author, comment string) (*event.Event, error) {
	ev, err := client.buildCommonEvent(ctx, host, service)
	if err != nil {
		return nil, err
	}

	ev.Type = event.TypeAcknowledgement
	ev.Username = author
	ev.Message = comment

	return ev, nil
}

// startCatchupWorkers launches goroutines for catching up the Icinga 2 API state.
//
// Each event will be sent to the returned channel. When all launched workers have finished - either because all are
// done or one has failed and the others were interrupted -, the channel will be closed. Those workers honor a context
// derived from the Client.Ctx and would either stop when this context is done or when the context.CancelFunc is called.
func (client *Client) startCatchupWorkers() (chan *eventMsg, context.CancelFunc) {
	startTime := time.Now()
	eventMsgCh := make(chan *eventMsg)

	// Unfortunately, the errgroup context is hidden, that's why another context is necessary.
	ctx, cancel := context.WithCancel(client.Ctx)
	group, groupCtx := errgroup.WithContext(ctx)

	objTypes := []string{"host", "service"}
	for _, objType := range objTypes {
		objType := objType // https://go.dev/doc/faq#closures_and_goroutines
		group.Go(func() error {
			err := client.checkMissedChanges(groupCtx, objType, eventMsgCh)
			if err != nil {
				client.Logger.Errorw("Catch-up-phase event worker failed", zap.String("object type", objType), zap.Error(err))
			}
			return err
		})
	}

	go func() {
		err := group.Wait()
		if err != nil {
			client.Logger.Errorw("Catching up the API failed", zap.Error(err), zap.Duration("duration", time.Since(startTime)))
		} else {
			client.Logger.Infow("Catching up the API has finished", zap.Duration("duration", time.Since(startTime)))
		}

		cancel()
		close(eventMsgCh)
	}()

	return eventMsgCh, cancel
}

// worker is the Client's main background worker, taking care of event.Event dispatching and mode switching.
//
// When the Client is in the catch-up-phase, requested by catchupPhaseRequest, events from the Event Stream API will
// be cached until the catch-up-phase has finished, while replayed events will be delivered directly.
//
// Communication takes place over the eventDispatcherEventStream and catchupPhaseRequest channels.
func (client *Client) worker() {
	var (
		// catchupEventCh emits events generated during the catch-up-phase from catch-up-workers. It will be closed when
		// catching up is done, which indicates the select below to switch phases. When this variable is nil, this
		// Client is in the normal operating phase.
		catchupEventCh chan *eventMsg
		// catchupCancel cancels, if not nil, all running catch-up-workers, e.g., when restarting catching-up.
		catchupCancel context.CancelFunc

		// catchupBuffer holds Event Stream events to be replayed after the catch-up-phase has finished.
		catchupBuffer = make([]*event.Event, 0)
		// catchupCache maps event.Events.Name to API time to skip replaying outdated events.
		catchupCache = make(map[string]time.Time)

		// dispatchQueue is a FIFO-like queue for events to be dispatched to the callback function without having to
		// wait for the callback to finish, which, as being database-bound, might take some time during bulk phases.
		dispatchQueue = make(chan *event.Event, 1<<16)
	)

	// While the worker's main loop fills the dispatchQueue for outgoing events, this small goroutine drains the
	// buffered channel and forwards each request to the callback function.
	go func() {
		for {
			select {
			case <-client.Ctx.Done():
				return

			case ev := <-dispatchQueue:
				client.CallbackFn(ev)
			}
		}
	}()

	// dispatchEvent enqueues the event to the dispatchQueue while honoring the Client.Ctx. It returns true when
	// enqueueing worked and false either if the buffered queue is stuck for a whole minute or, more likely, the
	// context is done.
	dispatchEvent := func(ev *event.Event) bool {
		select {
		case <-client.Ctx.Done():
			return false

		case <-time.After(time.Minute):
			client.Logger.Errorw("Abort event enqueueing for dispatching due to a timeout", zap.Stringer("event", ev))
			return false

		case dispatchQueue <- ev:
			return true
		}
	}

	// catchupCacheUpdate updates the catchupCache if this eventMsg seems to be the latest of its kind.
	catchupCacheUpdate := func(ev *eventMsg) {
		ts, ok := catchupCache[ev.event.Name]
		if !ok || ev.apiTime.After(ts) {
			catchupCache[ev.event.Name] = ev.apiTime
		}
	}

	for {
		select {
		case <-client.Ctx.Done():
			client.Logger.Warnw("Closing down main worker as context is finished", zap.Error(client.Ctx.Err()))
			return

		case <-client.catchupPhaseRequest:
			if catchupEventCh != nil {
				client.Logger.Warn("Switching to catch-up-phase was requested while already catching up, restarting phase")

				// Drain the old catch-up-phase producer channel until it is closed as its context will be canceled.
				go func(catchupEventCh chan *eventMsg) {
					for _, ok := <-catchupEventCh; ok; {
					}
				}(catchupEventCh)
				catchupCancel()
			}

			client.Logger.Info("Worker enters catch-up-phase, start caching up on Event Stream events")
			catchupEventCh, catchupCancel = client.startCatchupWorkers()

		case ev, ok := <-catchupEventCh:
			// Process an incoming event
			if ok {
				_ = dispatchEvent(ev.event)
				catchupCacheUpdate(ev)
				break
			}

			// The channel was closed - replay cache and switch modes
			skipCounter := 0

			for _, ev := range catchupBuffer {
				ts, ok := catchupCache[ev.Name]
				if ok && ev.Time.Before(ts) {
					client.Logger.Debugw("Skip replaying outdated Event Stream event", zap.Stringer("event", ev),
						zap.Time("event timestamp", ev.Time), zap.Time("cache timestamp", ts))
					skipCounter++
					continue
				}

				if !dispatchEvent(ev) {
					client.Logger.Error("Aborting Event Stream replay as an event could not be enqueued for dispatching")
					break
				}
			}
			client.Logger.Infow("Worker leaves catch-up-phase, returning to normal operation",
				zap.Int("cached events", len(catchupBuffer)), zap.Int("skipped cached events", skipCounter))

			catchupEventCh, catchupCancel = nil, nil
			catchupBuffer = make([]*event.Event, 0)
			catchupCache = make(map[string]time.Time)

		case ev := <-client.eventDispatcherEventStream:
			// During catch-up-phase, buffer Event Stream events
			if catchupEventCh != nil {
				catchupBuffer = append(catchupBuffer, ev.event)
				catchupCacheUpdate(ev)
				break
			}

			_ = dispatchEvent(ev.event)
		}
	}
}

// Process incoming events and reconnect to the Event Stream with catching up on missed objects if necessary.
//
// This method blocks as long as the Client runs, which, unless Ctx is cancelled, is forever. While its internal loop
// takes care of reconnections, messages are being logged while generated event.Event will be dispatched to the
// CallbackFn function.
func (client *Client) Process() {
	client.eventDispatcherEventStream = make(chan *eventMsg)
	client.catchupPhaseRequest = make(chan struct{})

	go client.worker()

	for client.Ctx.Err() == nil {
		err := client.listenEventStream()
		if err != nil {
			client.Logger.Errorw("Event Stream processing was interrupted", zap.Error(err))
		} else {
			client.Logger.Errorw("Event Stream processing was closed")
		}
	}
}
