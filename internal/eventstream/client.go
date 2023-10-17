package eventstream

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"github.com/icinga/icinga-notifications/internal/event"
	"net/http"
	"net/url"
	"time"
)

const IcingaNotificationsEventSourceId = 1 // TODO

type Client struct {
	ApiHost          string
	ApiBasicAuthUser string
	ApiBasicAuthPass string
	ApiHttpTransport http.Transport

	IcingaWebRoot string

	Ctx context.Context

	CallbackFn func(event event.Event)

	LastTimestamp time.Time
}

// eventStreamHandleStateChange acts on a received Event Stream StateChange object.
func (client *Client) eventStreamHandleStateChange(stateChange *StateChange) (event.Event, error) {
	var (
		eventName      string
		eventUrlSuffix string
		eventTags      map[string]string
		eventSeverity  event.Severity
	)

	if stateChange.Service != "" {
		eventName = stateChange.Host + "!" + stateChange.Service
		eventUrlSuffix = "/icingadb/service?name=" + url.PathEscape(stateChange.Service) + "&host.name=" + url.PathEscape(stateChange.Host)
		eventTags = map[string]string{
			"host":    stateChange.Host,
			"service": stateChange.Service,
		}
		switch stateChange.State {
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
		eventName = stateChange.Host
		eventUrlSuffix = "/icingadb/host?name=" + url.PathEscape(stateChange.Host)
		eventTags = map[string]string{
			"host": stateChange.Host,
		}
		switch stateChange.State {
		case 0:
			eventSeverity = event.SeverityOK
		case 1:
			eventSeverity = event.SeverityCrit
		default:
			eventSeverity = event.SeverityErr
		}
	}

	ev := event.Event{
		Time:      stateChange.Timestamp.Time,
		SourceId:  IcingaNotificationsEventSourceId,
		Name:      eventName,
		URL:       client.IcingaWebRoot + eventUrlSuffix,
		Tags:      eventTags,
		ExtraTags: nil, // TODO
		Type:      event.TypeState,
		Severity:  eventSeverity,
		Username:  "", // NOTE: a StateChange has no user per se
		Message:   stateChange.CheckResult.Output,
	}
	return ev, nil
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
		SourceId:  IcingaNotificationsEventSourceId,
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

// ListenEventStream subscribes to the Icinga 2 API Event Stream and handles received objects.
func (client *Client) ListenEventStream() error {
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

	lineScanner := bufio.NewScanner(res.Body)
	for lineScanner.Scan() {
		rawJson := lineScanner.Bytes()

		resp, err := UnmarshalEventStreamResponse(rawJson)
		if err != nil {
			return err
		}

		var ev event.Event
		switch resp.(type) {
		case *StateChange:
			ev, err = client.eventStreamHandleStateChange(resp.(*StateChange))
		case *AcknowledgementSet:
			ev, err = client.eventStreamHandleAcknowledgementSet(resp.(*AcknowledgementSet))
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

		client.LastTimestamp = ev.Time
		client.CallbackFn(ev)
	}
	err = lineScanner.Err()
	if err != nil {
		return err
	}

	return nil
}
