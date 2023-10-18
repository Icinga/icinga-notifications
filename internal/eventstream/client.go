package eventstream

import (
	"context"
	"encoding/json"
	"github.com/icinga/icinga-notifications/internal/event"
	"github.com/icinga/icingadb/pkg/logging"
	"hash/fnv"
	"net/http"
	"net/url"
	"slices"
	"sync"
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

	// All those variables are used internally to keep at least some state.
	eventsHandlerMutex  sync.RWMutex
	eventsRingBuffer    []uint64
	eventsRingBufferPos int
	eventsLastTs        time.Time
}

// buildCommonEvent creates an event.Event based on Host and (optional) Service attributes to be specified later.
//
// The following fields will NOT be populated and might be altered later:
//   - Time
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

	ev.Time = result.ExecutionEnd.Time
	ev.Type = event.TypeState
	ev.Severity = eventSeverity
	ev.Message = result.Output

	return ev, nil
}

// buildAcknowledgementEvent from the given fields.
func (client *Client) buildAcknowledgementEvent(ts time.Time, host, service, author, comment string) (*event.Event, error) {
	ev, err := client.buildCommonEvent(host, service)
	if err != nil {
		return nil, err
	}

	ev.Time = ts
	ev.Type = event.TypeAcknowledgement
	ev.Username = author
	ev.Message = comment

	return ev, nil
}

// handleEvent checks and dispatches generated Events.
func (client *Client) handleEvent(ev *event.Event, source string) {
	h := fnv.New64a()
	_ = json.NewEncoder(h).Encode(ev)
	evHash := h.Sum64()

	client.Logger.Debugf("Start handling event %s received from %s", ev, source)

	client.eventsHandlerMutex.RLock()
	inCache := slices.Contains(client.eventsRingBuffer, evHash)
	client.eventsHandlerMutex.RUnlock()
	if inCache {
		client.Logger.Warnf("Event %s received from %s is already in cache and will not be processed", ev, source)
		return
	}

	client.eventsHandlerMutex.Lock()
	client.eventsRingBuffer[client.eventsRingBufferPos] = evHash
	client.eventsRingBufferPos = (client.eventsRingBufferPos + 1) % len(client.eventsRingBuffer)

	if ev.Time.Before(client.eventsLastTs) {
		client.Logger.Infof("Event %s received from %s generated at %v before last known timestamp %v; might be a replay",
			ev, source, ev.Time, client.eventsLastTs)
	}
	client.eventsLastTs = ev.Time
	client.eventsHandlerMutex.Unlock()

	client.CallbackFn(ev)
}

// Process incoming objects and reconnect to the Event Stream with replaying objects if necessary.
//
// This method blocks as long as the Client runs, which, unless its context is cancelled, is forever. While its internal
// loop takes care of reconnections, all those events will be logged while generated Events will be dispatched to the
// callback function.
func (client *Client) Process() {
	client.eventsRingBuffer = make([]uint64, 1024)
	client.eventsRingBufferPos = 0

	defer client.Logger.Info("Event Stream Client has stopped")

	for {
		client.Logger.Info("Start listening on Icinga 2 Event Stream..")
		err := client.listenEventStream()
		if err != nil {
			client.Logger.Errorf("Event Stream processing failed: %v", err)
		} else {
			client.Logger.Warn("Event Stream closed stream; maybe Icinga 2 is reloading")
		}

		for {
			if client.Ctx.Err() != nil {
				client.Logger.Info("Abort Icinga 2 API reconnections as context is finished")
				return
			}

			err := client.reestablishApiConnection()
			if err == nil {
				break
			}
			client.Logger.Errorf("Cannot reestablish an API connection: %v", err)
		}

		go client.checkMissedStateChanges("host")
		go client.checkMissedStateChanges("service")
		go client.checkMissedAcknowledgements("host")
		go client.checkMissedAcknowledgements("service")
	}
}
