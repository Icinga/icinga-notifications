package eventstream

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/icinga/icinga-notifications/internal/config"
	"github.com/icinga/icinga-notifications/internal/daemon"
	"github.com/icinga/icinga-notifications/internal/event"
	"github.com/icinga/icingadb/pkg/icingadb"
	"github.com/icinga/icingadb/pkg/logging"
	"go.uber.org/zap"
	"net/http"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// This file contains the main resp. common methods for the Client.

// eventMsg is an internal struct for passing events with additional information from producers to the dispatcher.
type eventMsg struct {
	event   *event.Event
	apiTime time.Time
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

	// eventDispatcherEventStream communicates Events to be processed from the Event Stream API.
	eventDispatcherEventStream chan *eventMsg
	// eventDispatcherReplay communicates Events to be processed from the Icinga 2 API replay during replay phase.
	eventDispatcherReplay chan *eventMsg

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
	var (
		// replayBuffer holds Event Stream events to be replayed after the replay phase has finished.
		replayBuffer = make([]*event.Event, 0)
		// replayCache maps eventHash(ev) to API time to skip replaying outdated Event Stream events.
		replayCache = make(map[[sha256.Size]byte]time.Time)
	)

	// eventHash maps a subset of an event.Event to a hash. This is necessary for the replayCache below. As flapping
	// events should be ignored, only some of the event fields will be encoded. By excluding some, e.g., the severity,
	// events concerning the same host or service are grouped anyway.
	eventHash := func(ev *event.Event) [sha256.Size]byte {
		h := sha256.New()
		_ = binary.Write(h, binary.BigEndian, ev.SourceId)
		_, _ = fmt.Fprint(h, ev.Name)
		_, _ = fmt.Fprint(h, ev.Type)
		return [sha256.Size]byte(h.Sum(nil))
	}

	// eventHashUpdate updates the replayCache if this eventMsg seems to be the latest of its kind.
	eventHashUpdate := func(ev *eventMsg) {
		h := eventHash(ev.event)
		ts, ok := replayCache[h]
		if !ok || ev.apiTime.After(ts) {
			replayCache[h] = ev.apiTime
		}
	}

	for {
		select {
		case <-client.Ctx.Done():
			client.Logger.Warnw("Closing event dispatcher as its context is done", zap.Error(client.Ctx.Err()))
			return

		case <-client.replayTrigger:
			skipCounter := 0
			for _, ev := range replayBuffer {
				ts, ok := replayCache[eventHash(ev)]
				if ok && ev.Time.Before(ts) {
					client.Logger.Debugw("Skip replaying outdated Event Stream event", zap.Stringer("event", ev),
						zap.Time("event timestamp", ev.Time), zap.Time("cache timestamp", ts))
					skipCounter++
					continue
				}

				client.CallbackFn(ev)
			}
			client.Logger.Infow("Finished replay phase, returning to normal operation",
				zap.Int("cached events", len(replayBuffer)), zap.Int("skipped events", skipCounter))

			replayBuffer = make([]*event.Event, 0)
			replayCache = make(map[[sha256.Size]byte]time.Time)
			client.replayPhase.Store(false)

		case ev := <-client.eventDispatcherEventStream:
			if !client.replayPhase.Load() {
				client.CallbackFn(ev.event)
				continue
			}

			replayBuffer = append(replayBuffer, ev.event)
			eventHashUpdate(ev)

		case ev := <-client.eventDispatcherReplay:
			if !client.replayPhase.Load() {
				client.Logger.Errorw("Dispatcher received replay event during normal operation", zap.Stringer("event", ev.event))
				continue
			}

			client.CallbackFn(ev.event)
			eventHashUpdate(ev)
		}
	}
}

// enterReplayPhase enters the replay phase for the initial sync and after reconnections.
//
// This method starts multiple goroutines. First, some workers to query the Icinga 2 Objects API will be launched. When
// all of those have finished, the replayTrigger will be used to indicate that the buffered Events should be replayed.
func (client *Client) enterReplayPhase() {
	client.Logger.Info("Entering replay phase to replay stored events first")
	if !client.replayPhase.CompareAndSwap(false, true) {
		client.Logger.Error("The Event Stream Client is already in the replay phase")
		return
	}

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
		startTime := time.Now()
		replayWg.Wait()
		client.Logger.Debugw("All replay phase workers have finished", zap.Duration("duration", time.Since(startTime)))
		client.replayTrigger <- struct{}{}
	}()
}

// Process incoming objects and reconnect to the Event Stream with replaying objects if necessary.
//
// This method blocks as long as the Client runs, which, unless its context is cancelled, is forever. While its internal
// loop takes care of reconnections, all those events will be logged while generated Events will be dispatched to the
// callback function.
func (client *Client) Process() {
	client.eventDispatcherEventStream = make(chan *eventMsg)
	client.eventDispatcherReplay = make(chan *eventMsg)
	client.replayTrigger = make(chan struct{})

	go client.eventDispatcher()

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
