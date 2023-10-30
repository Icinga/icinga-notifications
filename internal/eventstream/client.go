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
	"net"
	"net/http"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// This file contains the main resp. common methods for the Client.

// outgoingEvent is a wrapper around an event.Event and its producer's origin to be sent to the eventDispatcher.
type outgoingEvent struct {
	event           *event.Event
	fromEventStream bool
}

// Client for the Icinga 2 Event Stream API with extended support for other Icinga 2 APIs to gather additional
// information and allow a replay in case of a connection loss.
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

	// eventDispatch communicates Events to be processed between producer and consumer.
	eventDispatch chan *outgoingEvent
	// replayTrigger signals the eventDispatcher method that the replay phase is finished.
	replayTrigger chan struct{}
	// replayPhase indicates that Events will be cached as the Event Stream Client is in the replay phase.
	replayPhase atomic.Bool
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
				// Limit the initial Dial timeout to enable a fast retry when the Icinga 2 API is offline. Due to the
				// need for very long-lived connections against the Event Stream API afterward, Client.Timeout would
				// limit the whole connection, which would be fatal.
				//
				// Check the "Client Timeout" section of the following (slightly outdated) blog post:
				// https://blog.cloudflare.com/the-complete-guide-to-golang-net-http-timeouts/
				DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
					dialer := &net.Dialer{Timeout: 3 * time.Second}
					return dialer.DialContext(ctx, network, addr)
				},
				TLSClientConfig: &tls.Config{
					MinVersion: tls.VersionTLS13,
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
func (client *Client) buildCommonEvent(host, service string) (*event.Event, error) {
	var (
		eventName      string
		eventUrlSuffix string
		eventTags      map[string]string
		eventExtraTags = make(map[string]string)
	)

	if service != "" {
		eventName = host + "!" + service
		eventUrlSuffix = "/icingadb/service?name=" + url.PathEscape(service) + "&host.name=" + url.PathEscape(host)

		eventTags = map[string]string{
			"host":    host,
			"service": service,
		}

		serviceGroups, err := client.fetchServiceGroups(host, service)
		if err != nil {
			return nil, err
		}
		for _, serviceGroup := range serviceGroups {
			eventExtraTags["servicegroup/"+serviceGroup] = ""
		}
	} else {
		eventName = host
		eventUrlSuffix = "/icingadb/host?name=" + url.PathEscape(host)

		eventTags = map[string]string{
			"host": host,
		}
	}

	hostGroups, err := client.fetchHostGroups(host)
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
		URL:       client.IcingaWebRoot + eventUrlSuffix,
		Tags:      eventTags,
		ExtraTags: eventExtraTags,
	}, nil
}

// buildHostServiceEvent constructs an event.Event based on a CheckResult, a Host or Service state, a Host name and an
// optional Service name if the Event should represent a Service object.
func (client *Client) buildHostServiceEvent(result CheckResult, state int, host, service string) (*event.Event, error) {
	var eventSeverity event.Severity

	if service != "" {
		switch state {
		case 0:
			eventSeverity = event.SeverityOK
		case 1:
			eventSeverity = event.SeverityWarning
		case 2:
			eventSeverity = event.SeverityCrit
		default:
			eventSeverity = event.SeverityErr
		}
	} else {
		switch state {
		case 0:
			eventSeverity = event.SeverityOK
		case 1:
			eventSeverity = event.SeverityCrit
		default:
			eventSeverity = event.SeverityErr
		}
	}

	ev, err := client.buildCommonEvent(host, service)
	if err != nil {
		return nil, err
	}

	ev.Type = event.TypeState
	ev.Severity = eventSeverity
	ev.Message = result.Output

	return ev, nil
}

// buildAcknowledgementEvent from the given fields.
func (client *Client) buildAcknowledgementEvent(host, service, author, comment string) (*event.Event, error) {
	ev, err := client.buildCommonEvent(host, service)
	if err != nil {
		return nil, err
	}

	ev.Type = event.TypeAcknowledgement
	ev.Username = author
	ev.Message = comment

	return ev, nil
}

// eventDispatcher receives generated event.Events to be either buffered or directly delivered to the CallbackFn.
//
// When the Client is in the replay phase, events from the Event Stream API will be cached until the replay phase has
// finished, while replayed events will be delivered directly.
func (client *Client) eventDispatcher() {
	var replayBuffer []*event.Event

	for {
		select {
		case <-client.Ctx.Done():
			client.Logger.Warnw("Closing event dispatcher as context is done", zap.Error(client.Ctx.Err()))
			return

		case <-client.replayTrigger:
			for _, ev := range replayBuffer {
				client.CallbackFn(ev)
			}
			client.Logger.Debugf("Replayed %d events during replay phase", len(replayBuffer))
			client.replayPhase.Store(false)
			replayBuffer = []*event.Event{}
			client.Logger.Info("Finished replay phase and returning to normal operation")

		case ev := <-client.eventDispatch:
			if client.replayPhase.Load() && ev.fromEventStream {
				replayBuffer = append(replayBuffer, ev.event)
			} else {
				client.CallbackFn(ev.event)
			}
		}
	}
}

// enterReplayPhase enters the replay phase for the initial sync and after reconnections.
//
// This method starts multiple goroutines. First, some workers to query the Icinga 2 Objects API will be launched. When
// all of those have finished, the replayTrigger will be used to indicate that the buffered Events should be replayed.
func (client *Client) enterReplayPhase() {
	client.Logger.Info("Entering replay phase to replay events")
	client.replayPhase.Store(true)

	queryFns := []func(string){client.checkMissedAcknowledgements, client.checkMissedStateChanges}
	objTypes := []string{"host", "service"}

	var replayWg sync.WaitGroup
	replayWg.Add(len(queryFns) * len(objTypes))

	for _, fn := range queryFns {
		for _, objType := range objTypes {
			go func(fn func(string), objType string) {
				fn(objType)
				replayWg.Done()
			}(fn, objType)
		}
	}

	go func() {
		replayWg.Wait()
		client.replayTrigger <- struct{}{}
	}()
}

// Process incoming objects and reconnect to the Event Stream with replaying objects if necessary.
//
// This method blocks as long as the Client runs, which, unless its context is cancelled, is forever. While its internal
// loop takes care of reconnections, all those events will be logged while generated Events will be dispatched to the
// callback function.
func (client *Client) Process() {
	// These two channels will be used to communicate the Events and are crucial. As there are multiple producers and
	// only one consumer, eventDispatcher, there is no ideal closer. However, producers and the consumer will be
	// finished by the Client's context. When this happens, the main application should either be stopped or the Client
	// is restarted, and we can hope for the GC. To make sure that nothing gets stuck, make the event channel buffered.
	client.eventDispatch = make(chan *outgoingEvent, 1024)
	client.replayTrigger = make(chan struct{})

	defer client.Logger.Info("Event Stream Client has stopped")

	go client.eventDispatcher()

	for {
		err := client.listenEventStream()
		if err != nil {
			client.Logger.Errorw("Event Stream processing failed", zap.Error(err))
		} else {
			client.Logger.Warn("Event Stream closed stream; maybe Icinga 2 is reloading")
		}
	}
}
