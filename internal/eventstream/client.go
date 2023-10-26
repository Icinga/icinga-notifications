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
	"net/http"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// This file contains the main resp. common methods for the Client.

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

	// replayPhase indicates that Events will be cached as the Event Stream Client is in the reconnection phase.
	replayPhase atomic.Bool
	// replayBuffer is the cache being populated during the reconnection phase and its mutex.
	replayBuffer      []*event.Event
	replayBufferMutex sync.Mutex
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

// handleEvent checks and dispatches generated Events.
func (client *Client) handleEvent(ev *event.Event) {
	if client.replayPhase.Load() {
		client.replayBufferMutex.Lock()
		client.replayBuffer = append(client.replayBuffer, ev)
		client.replayBufferMutex.Unlock()
		return
	}

	client.CallbackFn(ev)
}

func (client *Client) replayBufferedEvents() {
	client.replayBufferMutex.Lock()
	client.replayBuffer = make([]*event.Event, 0, 1024)
	client.replayBufferMutex.Unlock()
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

	// Fork off the synchronization in a background goroutine to wait for all producers to finish. As the producers
	// check the Client's context, they should finish early and this should not deadlock.
	go func() {
		replayWg.Wait()
		client.Logger.Debug("Querying the Objects API for replaying finished")

		if client.Ctx.Err() != nil {
			client.Logger.Warn("Aborting Objects API replaying as the context is done")
			return
		}

		for {
			// Here is a race between filling the buffer from incoming Event Stream events and processing the buffered
			// events. Thus, the buffer will be reset to catch up what happened in between, as otherwise Events would be
			// processed out of order. Only when the buffer is empty, the replay mode will be reset.
			client.replayBufferMutex.Lock()
			tmpReplayBuffer := client.replayBuffer
			client.replayBuffer = make([]*event.Event, 0, 1024)
			client.replayBufferMutex.Unlock()

			if len(tmpReplayBuffer) == 0 {
				break
			}

			for _, ev := range tmpReplayBuffer {
				client.CallbackFn(ev)
			}
			client.Logger.Debugf("Replayed %d events", len(tmpReplayBuffer))
		}

		client.replayPhase.Store(false)
		client.Logger.Debug("Finished replay")
	}()
}

// Process incoming objects and reconnect to the Event Stream with replaying objects if necessary.
//
// This method blocks as long as the Client runs, which, unless its context is cancelled, is forever. While its internal
// loop takes care of reconnections, all those events will be logged while generated Events will be dispatched to the
// callback function.
func (client *Client) Process() {
	defer client.Logger.Info("Event Stream Client has stopped")

	for {
		client.Logger.Info("Start listening on Icinga 2 Event Stream..")
		err := client.listenEventStream()
		if err != nil {
			client.Logger.Errorf("Event Stream processing failed: %v", err)
		} else {
			client.Logger.Warn("Event Stream closed stream; maybe Icinga 2 is reloading")
		}

		err = client.waitForApiAvailability()
		if err != nil {
			client.Logger.Errorw("Cannot reestablish an API connection", zap.Error(err))
			return
		}

		client.replayBufferedEvents()
	}
}
