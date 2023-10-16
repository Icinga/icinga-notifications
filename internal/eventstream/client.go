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

	Ctx context.Context

	CallbackFn func(event event.Event)

	LastTimestamp time.Time
}

func (client *Client) uniqueQueueName() string {
	buff := make([]byte, 16)
	_, err := rand.Read(buff)
	if err != nil {
		// This error SHOULD NOT happen. Otherwise, it might be wise to crash.
		panic(err)
	}
	return fmt.Sprintf("icinga-notifications-%x", buff)
}

func (client *Client) handleStateChange(stateChange *StateChange) error {
	client.LastTimestamp = stateChange.Timestamp.Time

	var (
		eventName      string
		eventUrlSuffix string
		eventTags      map[string]string
		eventSeverity  event.Severity
	)

	if stateChange.Service != "" {
		eventName = stateChange.Host + "!" + stateChange.Service
		eventUrlSuffix = "/icingadb/service?q=" + url.QueryEscape(stateChange.Service) + "&host.name=" + url.QueryEscape(stateChange.Host)
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
		eventUrlSuffix = "/icingadb/host?name=" + url.QueryEscape(stateChange.Host)
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
		URL:       client.ApiHost + eventUrlSuffix,
		Tags:      eventTags,
		ExtraTags: nil, // TODO
		Type:      event.TypeState,
		Severity:  eventSeverity,
		Username:  "", // TODO: a StateChange has no user per se
		Message:   stateChange.CheckResult.Output,
	}
	client.CallbackFn(ev)

	return nil
}

func (client *Client) ListenEventStream() error {
	reqBody, err := json.Marshal(map[string]any{
		"queue": client.uniqueQueueName(),
		"types": []string{
			"StateChange",
			// "AcknowledgementSet",
			// "AcknowledgementCleared",
			// "CommentAdded",
			// "CommentRemoved",
			// "DowntimeAdded",
			// "DowntimeRemoved",
			// "DowntimeStarted",
			// "DowntimeTriggered",
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

		switch resp.(type) {
		case *StateChange:
			err = client.handleStateChange(resp.(*StateChange))
		// case *AcknowledgementSet:
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
	}
	err = lineScanner.Err()
	if err != nil {
		return err
	}

	return nil
}
