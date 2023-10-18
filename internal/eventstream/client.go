package eventstream

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"github.com/icinga/icinga-notifications/internal/event"
	"github.com/icinga/icingadb/pkg/logging"
	"hash/fnv"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
)

type Client struct {
	ApiHost          string
	ApiBasicAuthUser string
	ApiBasicAuthPass string
	ApiHttpTransport http.Transport

	IcingaNotificationsEventSourceId int64
	IcingaWebRoot                    string

	CallbackFn func(event event.Event)
	Ctx        context.Context
	Logger     *logging.Logger

	eventsHandlerMutex  sync.RWMutex
	eventsRingBuffer    []uint64
	eventsRingBufferPos int
	eventsLastTs        time.Time
}

// buildHostServiceEvent constructs an event.Event based on a CheckResult, a host name and an optional service name.
func (client *Client) buildHostServiceEvent(result CheckResult, hostName, serviceName string) event.Event {
	var (
		eventName      string
		eventUrlSuffix string
		eventTags      map[string]string
		eventSeverity  event.Severity
	)

	if serviceName != "" {
		eventName = hostName + "!" + serviceName
		eventUrlSuffix = "/icingadb/service?name=" + url.PathEscape(serviceName) + "&host.name=" + url.PathEscape(hostName)
		eventTags = map[string]string{
			"host":    hostName,
			"service": serviceName,
		}
		switch result.State {
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
		eventName = hostName
		eventUrlSuffix = "/icingadb/host?name=" + url.PathEscape(hostName)
		eventTags = map[string]string{
			"host": hostName,
		}
		switch result.State {
		case 0:
			eventSeverity = event.SeverityOK
		case 1:
			eventSeverity = event.SeverityCrit
		default:
			eventSeverity = event.SeverityErr
		}
	}

	return event.Event{
		Time:      result.ExecutionEnd.Time,
		SourceId:  client.IcingaNotificationsEventSourceId,
		Name:      eventName,
		URL:       client.IcingaWebRoot + eventUrlSuffix,
		Tags:      eventTags,
		ExtraTags: nil, // TODO
		Type:      event.TypeState,
		Severity:  eventSeverity,
		Username:  "", // NOTE: a StateChange has no user per se
		Message:   result.Output,
	}
}

// handleEvent checks and dispatches generated Events.
func (client *Client) handleEvent(ev event.Event, source string) {
	h := fnv.New64a()
	_ = json.NewEncoder(h).Encode(ev)
	evHash := h.Sum64()

	client.Logger.Debugf("Start handling event %s as %x received from %s", ev.String(), evHash, source)

	client.eventsHandlerMutex.RLock()
	inCache := slices.Contains(client.eventsRingBuffer, evHash)
	client.eventsHandlerMutex.RUnlock()
	if inCache {
		client.Logger.Warnf("Event %s is already in cache and will not be processed", ev.String())
		return
	}

	client.eventsHandlerMutex.Lock()
	client.eventsRingBuffer[client.eventsRingBufferPos] = evHash
	client.eventsRingBufferPos = (client.eventsRingBufferPos + 1) % len(client.eventsRingBuffer)

	if ev.Time.Before(client.eventsLastTs) {
		client.Logger.Warnf("Received Event %s generated before last known timestamp %v; turn back the clock",
			ev.String(), client.eventsLastTs)
	}
	client.eventsLastTs = ev.Time
	client.eventsHandlerMutex.Unlock()

	client.Logger.Debugf("Forward event %s to callback function", ev.String())
	client.CallbackFn(ev)
}

// eventStreamHandleStateChange acts on a received Event Stream StateChange object.
func (client *Client) eventStreamHandleStateChange(stateChange *StateChange) (event.Event, error) {
	return client.buildHostServiceEvent(stateChange.CheckResult, stateChange.Host, stateChange.Service), nil
}

// eventStreamHandleAcknowledgementSet acts on a received Event Stream AcknowledgementSet object.
func (client *Client) eventStreamHandleAcknowledgementSet(ackSet *AcknowledgementSet) (event.Event, error) {
	var (
		eventName      string
		eventUrlSuffix string
		eventTags      map[string]string
	)

	if ackSet.Service != "" {
		eventName = ackSet.Host + "!" + ackSet.Service
		eventUrlSuffix = "/icingadb/service?name=" + url.PathEscape(ackSet.Service) + "&host.name=" + url.PathEscape(ackSet.Host)
		eventTags = map[string]string{
			"host":    ackSet.Host,
			"service": ackSet.Service,
		}
	} else {
		eventName = ackSet.Host
		eventUrlSuffix = "/icingadb/host?name=" + url.PathEscape(ackSet.Host)
		eventTags = map[string]string{
			"host": ackSet.Host,
		}
	}

	ev := event.Event{
		Time:      ackSet.Timestamp.Time,
		SourceId:  client.IcingaNotificationsEventSourceId,
		Name:      eventName,
		URL:       client.IcingaWebRoot + eventUrlSuffix,
		Tags:      eventTags,
		ExtraTags: nil, // TODO
		Type:      event.TypeAcknowledgement,
		Username:  ackSet.Author,
		Message:   ackSet.Comment,
	}
	return ev, nil
}

// listenEventStream subscribes to the Icinga 2 API Event Stream and handles received objects.
//
// In case of a parsing or handling error, this error will be returned. If the server closes the connection, nil will
// be returned.
func (client *Client) listenEventStream() error {
	queueNameRndBuff := make([]byte, 16)
	_, _ = rand.Read(queueNameRndBuff)

	reqBody, err := json.Marshal(map[string]any{
		"queue": fmt.Sprintf("icinga-notifications-%x", queueNameRndBuff),
		"types": []string{
			typeStateChange,
			typeAcknowledgementSet,
			// typeAcknowledgementCleared,
			// typeCommentAdded,
			// typeCommentRemoved,
			// typeDowntimeAdded,
			// typeDowntimeRemoved,
			// typeDowntimeStarted,
			// typeDowntimeTriggered,
		},
	})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(client.Ctx, http.MethodPost, client.ApiHost+"/v1/events", bytes.NewReader(reqBody))
	if err != nil {
		return err
	}

	req.SetBasicAuth(client.ApiBasicAuthUser, client.ApiBasicAuthPass)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	httpClient := &http.Client{Transport: &client.ApiHttpTransport}
	res, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()

	lineScanner := bufio.NewScanner(res.Body)
	for lineScanner.Scan() {
		rawJson := lineScanner.Bytes()

		resp, err := UnmarshalEventStreamResponse(rawJson)
		if err != nil {
			return err
		}

		var ev event.Event
		switch respT := resp.(type) {
		case *StateChange:
			ev, err = client.eventStreamHandleStateChange(respT)
		case *AcknowledgementSet:
			ev, err = client.eventStreamHandleAcknowledgementSet(respT)
		// case *AcknowledgementCleared:
		// case *CommentAdded:
		// case *CommentRemoved:
		// case *DowntimeAdded:
		// case *DowntimeRemoved:
		// case *DowntimeStarted:
		// case *DowntimeTriggered:
		default:
			err = fmt.Errorf("unsupported type %T", resp)
		}
		if err != nil {
			return err
		}

		client.handleEvent(ev, "Event Stream")
	}
	err = lineScanner.Err()
	if err != nil {
		return err
	}

	return nil
}

// queryObjectsApi sends a query to the Icinga 2 API /v1/objects to receive data of the given objType.
func (client *Client) queryObjectsApi(objType string, payload map[string]any) ([]ObjectQueriesResult, error) {
	reqBody, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(client.Ctx, http.MethodPost, client.ApiHost+"/v1/objects/"+objType, bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}

	req.SetBasicAuth(client.ApiBasicAuthUser, client.ApiBasicAuthPass)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Http-Method-Override", "GET")

	httpClient := &http.Client{Transport: &client.ApiHttpTransport}
	res, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()

	var objQueriesResults []ObjectQueriesResult
	err = json.NewDecoder(res.Body).Decode(&struct {
		Results *[]ObjectQueriesResult `json:"results"`
	}{&objQueriesResults})
	if err != nil {
		return nil, err
	}

	return objQueriesResults, nil
}

// queryObjectApiSince retrieves all objects of the given type, e.g., "host" or "service", with a state change after the
// passed time.
func (client *Client) queryObjectApiSince(objType string, since time.Time) ([]ObjectQueriesResult, error) {
	return client.queryObjectsApi(
		objType+"s",
		map[string]any{
			"filter": fmt.Sprintf("%s.last_state_change>%f", objType, float64(since.UnixMicro())/1_000_000.0),
		})
}

func (client *Client) checkMissedObjects(objType string) {
	client.eventsHandlerMutex.RLock()
	objQueriesResults, err := client.queryObjectApiSince(objType, client.eventsLastTs.Add(-time.Minute))
	client.eventsHandlerMutex.RUnlock()

	if err != nil {
		client.Logger.Errorf("Quering %ss from API failed, %v", objType, err)
		return
	}

	client.Logger.Infof("Querying %ss from API resulted in %d objects", objType, len(objQueriesResults))

	for _, objQueriesResult := range objQueriesResults {
		if client.Ctx.Err() != nil {
			client.Logger.Info("Stopping %s API response processing as context is finished", objType)
			return
		}

		attrs := objQueriesResult.Attrs.(*HostServiceRuntimeAttributes)

		var hostName, serviceName string
		switch objQueriesResult.Type {
		case "Host":
			hostName = attrs.Name

		case "Service":
			if !strings.HasPrefix(attrs.Name, attrs.Host+"!") {
				client.Logger.Errorf("Queried API Service object's name mismatches, %q is no prefix of %q", attrs.Host, attrs.Name)
				continue
			}
			hostName = attrs.Host
			serviceName = attrs.Name[len(attrs.Host)+1:]

		default:
			client.Logger.Errorf("Querying API delivered a %q object when expecting %s", objQueriesResult.Type, objType)
			continue
		}

		ev := client.buildHostServiceEvent(attrs.LastCheckResult, hostName, serviceName)
		client.handleEvent(ev, "API "+objType)
	}
}

// reestablishApiConnection tries to access the Icinga 2 API with an exponential backoff.
//
// With 10 retries, it might block up to (2^10 - 1) * 10 / 1_000 = 10.23 seconds.
func (client *Client) reestablishApiConnection() error {
	const maxRetries = 10

	req, err := http.NewRequestWithContext(client.Ctx, http.MethodGet, client.ApiHost+"/v1/", nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(client.ApiBasicAuthUser, client.ApiBasicAuthPass)

	var lastErr error
	for i := 0; i < maxRetries; i++ {
		time.Sleep((time.Duration)(math.Exp2(float64(i))) * 10 * time.Millisecond)

		client.Logger.Debugf("Try to reestablish an API connection, %d/%d tries..", i+1, maxRetries)

		httpClient := &http.Client{Transport: &client.ApiHttpTransport}
		res, err := httpClient.Do(req)
		if err != nil {
			lastErr = err
			client.Logger.Debugf("API probing failed: %v", lastErr)
			continue
		}
		_ = res.Body.Close()

		if res.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("expected HTTP status %d, got %d", http.StatusOK, res.StatusCode)
			client.Logger.Debugf("API probing failed: %v", lastErr)
			continue
		}
		return nil
	}
	return fmt.Errorf("cannot query API backend in %d tries, %w", maxRetries, lastErr)
}

func (client *Client) Process() {
	client.eventsRingBuffer = make([]uint64, 1024)
	client.eventsRingBufferPos = 0

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

		go client.checkMissedObjects("host")
		go client.checkMissedObjects("service")
	}
}
