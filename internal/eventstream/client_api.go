package eventstream

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"github.com/icinga/icinga-notifications/internal/event"
	"go.uber.org/zap"
	"io"
	"math"
	"net/http"
	"net/url"
	"slices"
	"time"
)

// This file contains Icinga 2 API related methods.

// extractObjectQueriesResult parses a typed ObjectQueriesResult array out of a JSON io.ReaderCloser.
//
// As Go 1.21 does not allow type parameters in methods[0], the logic was extracted into a function transforming the
// JSON response - passed as an io.ReaderCloser which will be closed within this function - into the typed response to
// be used within the methods below.
//
// [0] https://github.com/golang/go/issues/49085
func extractObjectQueriesResult[T Comment | Downtime | HostServiceRuntimeAttributes](jsonResp io.ReadCloser) ([]ObjectQueriesResult[T], error) {
	defer func() {
		_, _ = io.Copy(io.Discard, jsonResp)
		_ = jsonResp.Close()
	}()

	var objQueriesResults []ObjectQueriesResult[T]
	err := json.NewDecoder(jsonResp).Decode(&struct {
		Results *[]ObjectQueriesResult[T] `json:"results"`
	}{&objQueriesResults})
	if err != nil {
		return nil, err
	}
	return objQueriesResults, nil
}

// queryObjectsApi performs a configurable HTTP request against the Icinga 2 API and returns its raw response.
//
// The returned io.ReaderCloser MUST be both read to completion and closed to reuse connections.
func (client *Client) queryObjectsApi(
	ctx context.Context,
	urlPaths []string,
	method string,
	body io.Reader,
	headers map[string]string,
) (io.ReadCloser, error) {
	apiUrl, err := url.JoinPath(client.ApiHost, urlPaths...)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, apiUrl, body)
	if err != nil {
		return nil, err
	}

	req.SetBasicAuth(client.ApiBasicAuthUser, client.ApiBasicAuthPass)
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	// The underlying network connection is reused by using client.ApiHttpTransport.
	httpClient := &http.Client{
		Transport: &client.ApiHttpTransport,
		Timeout:   3 * time.Second,
	}
	res, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	if res.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, res.Body)
		_ = res.Body.Close()
		return nil, fmt.Errorf("unexpected HTTP status code %d", res.StatusCode)
	}

	return res.Body, nil
}

// queryObjectsApiDirect performs a direct resp. "fast" API query against an object, optionally identified by its name.
func (client *Client) queryObjectsApiDirect(ctx context.Context, objType, objName string) (io.ReadCloser, error) {
	return client.queryObjectsApi(
		ctx,
		[]string{"/v1/objects/", objType + "s/", url.PathEscape(objName)},
		http.MethodGet,
		nil,
		map[string]string{"Accept": "application/json"})
}

// queryObjectsApiQuery sends a query to the Icinga 2 API /v1/objects to receive data of the given objType.
func (client *Client) queryObjectsApiQuery(ctx context.Context, objType string, query map[string]any) (io.ReadCloser, error) {
	reqBody, err := json.Marshal(query)
	if err != nil {
		return nil, err
	}

	return client.queryObjectsApi(
		ctx,
		[]string{"/v1/objects/", objType + "s"},
		http.MethodPost,
		bytes.NewReader(reqBody),
		map[string]string{
			"Accept":                 "application/json",
			"Content-Type":           "application/json",
			"X-Http-Method-Override": "GET",
		})
}

// fetchHostServiceGroups fetches all Host or, if service is not empty, Service groups.
func (client *Client) fetchHostServiceGroups(ctx context.Context, host, service string) ([]string, error) {
	objType, objName := "host", host
	if service != "" {
		objType = "service"
		objName += "!" + service
	}

	jsonRaw, err := client.queryObjectsApiDirect(ctx, objType, objName)
	if err != nil {
		return nil, err
	}
	objQueriesResults, err := extractObjectQueriesResult[HostServiceRuntimeAttributes](jsonRaw)
	if err != nil {
		return nil, err
	}

	if len(objQueriesResults) != 1 {
		return nil, fmt.Errorf("expected exactly one result for object type %q and %q instead of %d",
			objType, objName, len(objQueriesResults))
	}

	return objQueriesResults[0].Attrs.Groups, nil
}

// fetchAcknowledgementComment fetches an Acknowledgement Comment for a Host (empty service) or for a Service at a Host.
//
// Unfortunately, there is no direct link between ACK'ed Host or Service objects and their acknowledgement Comment. The
// closest we can do, is query for Comments with the Acknowledgement Service Type and the host/service name. In addition,
// the Host's resp. Service's AcknowledgementLastChange field has NOT the same timestamp as the Comment; there is a
// difference of some milliseconds. As there might be even multiple ACK comments, we have to find the closest one.
func (client *Client) fetchAcknowledgementComment(ctx context.Context, host, service string, ackTime time.Time) (*Comment, error) {
	filterExpr := "comment.entry_type == 4 && comment.host_name == comment_host_name"
	filterVars := map[string]string{"comment_host_name": host}
	if service != "" {
		filterExpr += " && comment.service_name == comment_service_name"
		filterVars["comment_service_name"] = service
	}

	jsonRaw, err := client.queryObjectsApiQuery(ctx, "comment", map[string]any{"filter": filterExpr, "filter_vars": filterVars})
	if err != nil {
		return nil, err
	}
	objQueriesResults, err := extractObjectQueriesResult[Comment](jsonRaw)
	if err != nil {
		return nil, err
	}

	if len(objQueriesResults) == 0 {
		return nil, fmt.Errorf("found no ACK Comments for %q with %v", filterExpr, filterVars)
	}

	slices.SortFunc(objQueriesResults, func(a, b ObjectQueriesResult[Comment]) int {
		distA := a.Attrs.EntryTime.Time.Sub(ackTime).Abs()
		distB := b.Attrs.EntryTime.Time.Sub(ackTime).Abs()
		return int(distA - distB)
	})
	if objQueriesResults[0].Attrs.EntryTime.Sub(ackTime).Abs() > time.Second {
		return nil, fmt.Errorf("found no ACK Comment for %q with %v close to %v", filterExpr, filterVars, ackTime)
	}

	return &objQueriesResults[0].Attrs, nil
}

// checkMissedChanges queries objType (host, service) from the Icinga 2 API for replaying events.
//
// If the object's acknowledgement field is non-zero, an Acknowledgement Event will be constructed following the Host or
// Service object. Each event will be delivered to the channel.
func (client *Client) checkMissedChanges(ctx context.Context, objType string, eventCh chan *eventMsg) error {
	jsonRaw, err := client.queryObjectsApiDirect(ctx, objType, "")
	if err != nil {
		return err
	}
	objQueriesResults, err := extractObjectQueriesResult[HostServiceRuntimeAttributes](jsonRaw)
	if err != nil {
		return err
	}

	var stateChangeEvents, acknowledgementEvents int
	for _, objQueriesResult := range objQueriesResults {
		var hostName, serviceName string
		switch objQueriesResult.Type {
		case "Host":
			hostName = objQueriesResult.Attrs.Name

		case "Service":
			hostName = objQueriesResult.Attrs.Host
			serviceName = objQueriesResult.Attrs.Name

		default:
			return fmt.Errorf("querying API delivered a wrong object type %q", objQueriesResult.Type)
		}

		// State change event first
		ev, err := client.buildHostServiceEvent(
			ctx,
			objQueriesResult.Attrs.LastCheckResult, objQueriesResult.Attrs.State,
			hostName, serviceName)
		if err != nil {
			return fmt.Errorf("failed to construct Event from Host/Service response, %w", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case eventCh <- &eventMsg{ev, objQueriesResult.Attrs.LastStateChange.Time}:
			stateChangeEvents++
		}

		// Optional acknowledgement event second
		if objQueriesResult.Attrs.Acknowledgement == 0 {
			continue
		}

		ackComment, err := client.fetchAcknowledgementComment(
			ctx,
			hostName, serviceName,
			objQueriesResult.Attrs.AcknowledgementLastChange.Time)
		if err != nil {
			return fmt.Errorf("fetching acknowledgement comment for %v failed, %w", ev, err)
		}

		ev, err = client.buildAcknowledgementEvent(
			ctx,
			hostName, serviceName,
			ackComment.Author, ackComment.Text)
		if err != nil {
			return fmt.Errorf("failed to construct Event from Acknowledgement response, %w", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case eventCh <- &eventMsg{ev, objQueriesResult.Attrs.LastStateChange.Time}:
			acknowledgementEvents++
		}
	}

	client.Logger.Infow("Replaying API emitted state changes",
		zap.String("object type", objType),
		zap.Int("state changes", stateChangeEvents),
		zap.Int("acknowledgements", acknowledgementEvents))

	return nil
}

// connectEventStream connects to the EventStream within an infinite loop until a connection was established.
//
// The esTypes is a string array of required Event Stream types.
//
// An error will be returned if reconnecting resp. retrying the (almost) same thing will not help fix it.
func (client *Client) connectEventStream(esTypes []string) (*http.Response, context.CancelFunc, error) {
	apiUrl, err := url.JoinPath(client.ApiHost, "/v1/events")
	if err != nil {
		return nil, nil, err
	}

	for i := 0; ; i++ {
		// Always ensure an unique queue name to ensure no conflicts might occur.
		queueNameRndBuff := make([]byte, 16)
		_, _ = rand.Read(queueNameRndBuff)

		reqBody, err := json.Marshal(map[string]any{
			"queue": fmt.Sprintf("icinga-notifications-%x", queueNameRndBuff),
			"types": esTypes,
		})
		if err != nil {
			return nil, nil, err
		}

		// Sub-context which might get canceled early if connecting takes to long.
		// The reqCancel function will be called after the select below or when leaving the function with an error.
		reqCtx, reqCancel := context.WithCancel(client.Ctx)

		req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, apiUrl, bytes.NewReader(reqBody))
		if err != nil {
			reqCancel()
			return nil, nil, err
		}

		req.SetBasicAuth(client.ApiBasicAuthUser, client.ApiBasicAuthPass)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Content-Type", "application/json")

		resCh := make(chan *http.Response)

		go func() {
			defer close(resCh)

			client.Logger.Info("Try to establish an Event Stream API connection")
			httpClient := &http.Client{Transport: &client.ApiHttpTransport}
			res, err := httpClient.Do(req)
			if err != nil {
				client.Logger.Warnw("Establishing an Event Stream API connection failed; will be retried", zap.Error(err))
				return
			}

			select {
			case resCh <- res:

			case <-reqCtx.Done():
				// This case might happen when this httpClient.Do and the time.After in the select below finish at round
				// about the exact same time, but httpClient.Do was slightly faster than reqCancel().
				_, _ = io.Copy(io.Discard, res.Body)
				_ = res.Body.Close()
			}
		}()

		select {
		case res, ok := <-resCh:
			if ok {
				return res, reqCancel, nil
			}

		case <-time.After(3 * time.Second):
		}
		reqCancel()

		// Rate limit API reconnections: slow down for successive failed attempts but limit to three minutes.
		// 1s, 2s, 4s, 8s, 16s, 32s, 1m4s, 2m8s, 3m, 3m, 3m, ...
		select {
		case <-time.After(min(3*time.Minute, time.Duration(math.Exp2(float64(i)))*time.Second)):
		case <-client.Ctx.Done():
			return nil, nil, client.Ctx.Err()
		}
	}
}

// listenEventStream subscribes to the Icinga 2 API Event Stream and handles received objects.
//
// In case of a parsing or handling error, this error will be returned. If the server closes the connection, nil will
// be returned.
func (client *Client) listenEventStream() error {
	response, cancel, err := client.connectEventStream([]string{
		typeStateChange,
		typeAcknowledgementSet,
		// typeAcknowledgementCleared,
		// typeCommentAdded,
		// typeCommentRemoved,
		// typeDowntimeAdded,
		// typeDowntimeRemoved,
		// typeDowntimeStarted,
		// typeDowntimeTriggered,
	})
	if err != nil {
		return err
	}
	defer func() {
		cancel()

		_ = response.Body.Close()
	}()

	select {
	case <-client.Ctx.Done():
		client.Logger.Warnw("Cannot request starting replay phase as context is finished", zap.Error(client.Ctx.Err()))
		return client.Ctx.Err()
	case client.replayPhaseRequest <- struct{}{}:
	}

	client.Logger.Info("Start listening on Icinga 2 Event Stream..")

	lineScanner := bufio.NewScanner(response.Body)
	for lineScanner.Scan() {
		rawJson := lineScanner.Bytes()

		resp, err := UnmarshalEventStreamResponse(rawJson)
		if err != nil {
			return err
		}

		var (
			ev     *event.Event
			evTime time.Time
		)
		switch respT := resp.(type) {
		case *StateChange:
			ev, err = client.buildHostServiceEvent(client.Ctx, respT.CheckResult, respT.State, respT.Host, respT.Service)
			evTime = respT.Timestamp.Time

		case *AcknowledgementSet:
			ev, err = client.buildAcknowledgementEvent(client.Ctx, respT.Host, respT.Service, respT.Author, respT.Comment)
			evTime = respT.Timestamp.Time

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

		select {
		case <-client.Ctx.Done():
			client.Logger.Warnw("Cannot dispatch Event Stream event as context is finished", zap.Error(client.Ctx.Err()))
			return client.Ctx.Err()
		case client.eventDispatcherEventStream <- &eventMsg{ev, evTime}:
		}
	}
	return lineScanner.Err()
}
