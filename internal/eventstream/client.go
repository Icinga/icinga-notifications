package eventstream

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
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

// Client for the Icinga 2 Event Stream API with extended support for other Icinga 2 APIs to gather additional
// information and a replay either when starting up to catch up the Icinga's state or in case of a connection loss.
//
// Within the icinga-notifications scope, one or multiple Client instances can be generated from the configuration by
// calling NewClientsFromConfig.
//
// A Client must be started by calling its Process method, which blocks until Ctx is marked as done. Reconnections and
// the necessary state replaying from the Icinga 2 API will be taken care off. Internally, the Client executes a worker
// within its own goroutine, which dispatches event.Event to the CallbackFn and enforces event.Event order during
// replaying after (re-)connections.
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

	// CallbackFn receives generated event.Events.
	CallbackFn func(*event.Event)
	// Ctx for all web requests as well as internal wait loops.
	Ctx context.Context
	// Logger to log to.
	Logger *logging.Logger

	// eventDispatcherEventStream communicates Events to be processed from the Event Stream API.
	eventDispatcherEventStream chan *eventMsg
	// replayPhaseRequest requests the main worker to switch to the replay phase and re-request the Icinga 2 API.
	replayPhaseRequest chan struct{}
}

// NewClientsFromConfig returns all Clients defined in the conf.ConfigFile.
//
// Those are prepared and just needed to be started by calling their Process method.
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

			CallbackFn: makeProcessEvent(ctx, db, logger, logs, runtimeConfig),
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

// startReplayWorker launches goroutines for replaying the Icinga 2 API state.
//
// Each event will be sent to the returned channel. When all launched workers have finished - either because all are
// done or one has failed and the others were interrupted -, the channel will be closed. Those workers honor a context
// derived from the Client.Ctx and would either stop when the main context is done or when the returned
// context.CancelFunc is called.
func (client *Client) startReplayWorker() (chan *eventMsg, context.CancelFunc) {
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
				client.Logger.Errorw("Replaying API events failed", zap.String("object type", objType), zap.Error(err))
			}
			return err
		})
	}

	go func() {
		err := group.Wait()
		if err != nil {
			client.Logger.Errorw("Replaying the API failed", zap.Error(err), zap.Duration("duration", time.Since(startTime)))
		} else {
			client.Logger.Infow("Replaying the API has finished", zap.Duration("duration", time.Since(startTime)))
		}

		cancel()
		close(eventMsgCh)
	}()

	return eventMsgCh, cancel
}

// worker is the Client's main background worker, taking care of event.Event dispatching and mode switching.
//
// When the Client is in the replay phase, requested by replayPhaseRequest, events from the Event Stream API will
// be cached until the replay phase has finished, while replayed events will be delivered directly.
//
// Communication takes place over the eventDispatcherEventStream and replayPhaseRequest channels.
func (client *Client) worker() {
	var (
		// replayEventCh emits events generated during the replay phase from the replay worker. It will be closed when
		// replaying is finished, which indicates the select below to switch phases. When this variable is nil, the
		// Client is in the normal operating phase.
		replayEventCh chan *eventMsg
		// replayCancel cancels, if not nil, the currently running replay worker, e.g., when restarting the replay.
		replayCancel context.CancelFunc

		// replayBuffer holds Event Stream events to be replayed after the replay phase has finished.
		replayBuffer = make([]*event.Event, 0)
		// replayCache maps event.Events.Name to API time to skip replaying outdated events.
		replayCache = make(map[string]time.Time)
	)

	// replayCacheUpdate updates the replayCache if this eventMsg seems to be the latest of its kind.
	replayCacheUpdate := func(ev *eventMsg) {
		ts, ok := replayCache[ev.event.Name]
		if !ok || ev.apiTime.After(ts) {
			replayCache[ev.event.Name] = ev.apiTime
		}
	}

	for {
		select {
		case <-client.Ctx.Done():
			client.Logger.Warnw("Closing down main worker as context is finished", zap.Error(client.Ctx.Err()))
			return

		case <-client.replayPhaseRequest:
			if replayEventCh != nil {
				client.Logger.Warn("Replaying was requested while already being in the replay phase; restart replay")

				// Drain the old replay phase producer's channel until it is closed as its context was canceled.
				go func(replayEventCh chan *eventMsg) {
					for _, ok := <-replayEventCh; ok; {
					}
				}(replayEventCh)
				replayCancel()
			}

			client.Logger.Debug("Worker enters replay phase, starting caching Event Stream events")
			replayEventCh, replayCancel = client.startReplayWorker()

		case ev, ok := <-replayEventCh:
			// Process an incoming event
			if ok {
				client.CallbackFn(ev.event)
				replayCacheUpdate(ev)
				break
			}

			// The channel was closed - replay and switch modes
			skipCounter := 0
			for _, ev := range replayBuffer {
				ts, ok := replayCache[ev.Name]
				if ok && ev.Time.Before(ts) {
					client.Logger.Debugw("Skip replaying outdated Event Stream event", zap.Stringer("event", ev),
						zap.Time("event timestamp", ev.Time), zap.Time("cache timestamp", ts))
					skipCounter++
					continue
				}

				client.CallbackFn(ev)
			}
			client.Logger.Infow("Worker leaves replay phase, returning to normal operation",
				zap.Int("cached events", len(replayBuffer)), zap.Int("skipped events", skipCounter))

			replayEventCh, replayCancel = nil, nil
			replayBuffer = make([]*event.Event, 0)
			replayCache = make(map[string]time.Time)

		case ev := <-client.eventDispatcherEventStream:
			// During replay phase, buffer Event Stream events
			if replayEventCh != nil {
				replayBuffer = append(replayBuffer, ev.event)
				replayCacheUpdate(ev)
				break
			}

			client.CallbackFn(ev.event)
		}
	}
}

// Process incoming objects and reconnect to the Event Stream with replaying objects if necessary.
//
// This method blocks as long as the Client runs, which, unless its context is cancelled, is forever. While its internal
// loop takes care of reconnections, all those events will be logged while generated Events will be dispatched to the
// callback function.
func (client *Client) Process() {
	client.eventDispatcherEventStream = make(chan *eventMsg)
	client.replayPhaseRequest = make(chan struct{})

	go client.worker()

	for {
		err := client.listenEventStream()
		switch {
		case errors.Is(err, context.Canceled):
			client.Logger.Warnw("Stopping Event Stream Client as its context is done", zap.Error(err))
			return

		case err != nil:
			client.Logger.Errorw("Event Stream processing failed", zap.Error(err))

		default:
			client.Logger.Warn("Event Stream closed stream; maybe Icinga 2 is reloading")
		}
	}
}
