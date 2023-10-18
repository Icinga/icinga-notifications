package eventstream

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"github.com/icinga/icinga-notifications/internal/event"
	"net/http"
)

// This file contains Event Stream related methods of the Client.

// eventStreamHandleStateChange acts on a received Event Stream StateChange object.
func (client *Client) eventStreamHandleStateChange(stateChange *StateChange) (*event.Event, error) {
	return client.buildHostServiceEvent(stateChange.CheckResult, stateChange.State, stateChange.Host, stateChange.Service)
}

// eventStreamHandleAcknowledgementSet acts on a received Event Stream AcknowledgementSet object.
func (client *Client) eventStreamHandleAcknowledgementSet(ackSet *AcknowledgementSet) (*event.Event, error) {
	return client.buildAcknowledgementEvent(
		ackSet.Timestamp.Time,
		ackSet.Host, ackSet.Service,
		ackSet.Author, ackSet.Comment)
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

		var ev *event.Event
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
	return lineScanner.Err()
}
