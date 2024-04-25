package icinga2

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"github.com/icinga/icinga-notifications/internal/event"
	"go.uber.org/zap"
	"io"
	"net/http"
	"net/url"
	"slices"
	"time"
)

// This file contains Icinga 2 API related methods.

// transport wraps http.Transport and overrides http.RoundTripper to set a custom User-Agent for all requests.
type transport struct {
	http.Transport
	userAgent string
}

// RoundTrip implements http.RoundTripper to set a custom User-Agent header.
func (trans *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("User-Agent", trans.userAgent)
	return trans.Transport.RoundTrip(req)
}

// extractObjectQueriesResult parses a typed ObjectQueriesResult array out of a JSON io.ReaderCloser.
//
// The generic type T is currently limited to all later needed types, even when the API might also return other known or
// unknown types. When another type becomes necessary, T can be exceeded.
//
// As Go 1.21 does not allow type parameters in methods[0], the logic was extracted into a function transforming the
// JSON response - passed as an io.ReaderCloser which will be closed within this function - into the typed response to
// be used within the methods below.
//
//	[0] https://github.com/golang/go/issues/49085
func extractObjectQueriesResult[T Comment | HostServiceRuntimeAttributes](jsonResp io.ReadCloser) ([]ObjectQueriesResult[T], error) {
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
	apiUrl, err := url.JoinPath(client.ApiBaseURL, urlPaths...)
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
		Transport: client.ApiHttpTransport,
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
		return nil, fmt.Errorf("expected exactly one result for %q as object type %q instead of %d",
			objName, objType, len(objQueriesResults))
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
	// comment.entry_type = 4 is an Acknowledgement comment; Comment.EntryType
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
		distA := a.Attrs.EntryTime.Time().Sub(ackTime).Abs()
		distB := b.Attrs.EntryTime.Time().Sub(ackTime).Abs()
		return cmp.Compare(distA, distB)
	})
	if objQueriesResults[0].Attrs.EntryTime.Time().Sub(ackTime).Abs() > time.Second {
		return nil, fmt.Errorf("found no ACK Comment for %q with %v close to %v", filterExpr, filterVars, ackTime)
	}

	return &objQueriesResults[0].Attrs, nil
}

// checkMissedChanges queries objType (host, service) from the Icinga 2 API to catch up on missed events.
//
// If the object's acknowledgement field is non-zero, an Acknowledgement Event will be constructed following the Host or
// Service object. Each event will be delivered to the channel.
func (client *Client) checkMissedChanges(ctx context.Context, objType string, catchupEventCh chan *catchupEventMsg) error {
	jsonRaw, err := client.queryObjectsApiDirect(ctx, objType, "")
	if err != nil {
		return err
	}
	objQueriesResults, err := extractObjectQueriesResult[HostServiceRuntimeAttributes](jsonRaw)
	if err != nil {
		return err
	}

	var stateChangeEvents, acknowledgementEvents int
	defer func() {
		client.Logger.Debugw("Querying API emitted events",
			zap.String("object type", objType),
			zap.Int("state changes", stateChangeEvents),
			zap.Int("acknowledgements", acknowledgementEvents))
	}()

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

		// Only process HARD states
		if objQueriesResult.Attrs.StateType == StateTypeSoft {
			client.Logger.Debugf("Skipping SOFT event, %#v", objQueriesResult.Attrs)
			continue
		}

		// First: State change event
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
		case catchupEventCh <- &catchupEventMsg{eventMsg: &eventMsg{ev, objQueriesResult.Attrs.LastStateChange.Time()}}:
			stateChangeEvents++
		}

		// Second: Optional acknowledgement event
		if objQueriesResult.Attrs.Acknowledgement == 0 {
			continue
		}

		ackComment, err := client.fetchAcknowledgementComment(
			ctx,
			hostName, serviceName,
			objQueriesResult.Attrs.AcknowledgementLastChange.Time())
		if err != nil {
			return fmt.Errorf("fetching acknowledgement comment for %v failed, %w", ev, err)
		}

		ev, err = client.buildAcknowledgementEvent(
			ctx,
			hostName, serviceName,
			ackComment.Author, ackComment.Text, false)
		if err != nil {
			return fmt.Errorf("failed to construct Event from Acknowledgement response, %w", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case catchupEventCh <- &catchupEventMsg{eventMsg: &eventMsg{ev, objQueriesResult.Attrs.LastStateChange.Time()}}:
			acknowledgementEvents++
		}
	}
	return nil
}

// connectEventStreamReadCloser wraps io.ReadCloser with a context.CancelFunc to be returned in connectEventStream.
type connectEventStreamReadCloser struct {
	io.ReadCloser
	cancel context.CancelFunc
}

// Close the internal ReadCloser with canceling the internal http.Request's context first.
func (e *connectEventStreamReadCloser) Close() error {
	e.cancel()
	return e.ReadCloser.Close()
}

// connectEventStream connects to the EventStream, retries until a connection was established.
//
// The esTypes is a string array of required Event Stream types.
//
// An error will only be returned if reconnecting - retrying the (almost) same thing - will not help.
func (client *Client) connectEventStream(esTypes []string) (io.ReadCloser, error) {
	apiUrl, err := url.JoinPath(client.ApiBaseURL, "/v1/events")
	if err != nil {
		return nil, err
	}

	for retryDelay := time.Second; ; retryDelay = min(3*time.Minute, 2*retryDelay) {
		// Always ensure an unique queue name to mitigate possible naming conflicts.
		queueNameRndBuff := make([]byte, 16)
		_, _ = rand.Read(queueNameRndBuff)

		reqBody, err := json.Marshal(map[string]any{
			"queue": fmt.Sprintf("icinga-notifications-%x", queueNameRndBuff),
			"types": esTypes,
		})
		if err != nil {
			return nil, err
		}

		// Sub-context which might get canceled early if connecting takes to long.
		// The reqCancel function will be called after the select below or when leaving the function with an error.
		// When leaving the function without an error, it is being called in connectEventStreamReadCloser.Close().
		reqCtx, reqCancel := context.WithCancel(client.Ctx)

		req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, apiUrl, bytes.NewReader(reqBody))
		if err != nil {
			reqCancel()
			return nil, err
		}

		req.SetBasicAuth(client.ApiBasicAuthUser, client.ApiBasicAuthPass)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Content-Type", "application/json")

		resCh := make(chan *http.Response)

		go func() {
			defer close(resCh)

			client.Logger.Debug("Try to establish an Event Stream API connection")
			httpClient := &http.Client{Transport: client.ApiHttpTransport}
			res, err := httpClient.Do(req)
			if err != nil {
				client.Logger.Warnw("Establishing an Event Stream API connection failed, will be retried",
					zap.Error(err),
					zap.Duration("delay", retryDelay))
				return
			}

			select {
			case <-reqCtx.Done():
				// This case might happen when this httpClient.Do and the time.After in the select below finish at round
				// about the exact same time, but httpClient.Do was slightly faster than reqCancel().
				_, _ = io.Copy(io.Discard, res.Body)
				_ = res.Body.Close()
			case resCh <- res:
			}
		}()

		select {
		case res, ok := <-resCh:
			if ok {
				esReadCloser := &connectEventStreamReadCloser{
					ReadCloser: res.Body,
					cancel:     reqCancel,
				}
				return esReadCloser, nil
			}

		case <-time.After(3 * time.Second):
		}
		reqCancel()

		// Rate limit API reconnections: slow down for successive failed attempts but limit to three minutes.
		// 1s, 2s, 4s, 8s, 16s, 32s, 1m4s, 2m8s, 3m, 3m, 3m, ...
		select {
		case <-time.After(retryDelay):
		case <-client.Ctx.Done():
			return nil, client.Ctx.Err()
		}
	}
}

// listenEventStream subscribes to the Icinga 2 API Event Stream and handles received objects.
//
// In case of a parsing or handling error, this error will be returned. If the server closes the connection, nil will
// be returned.
func (client *Client) listenEventStream() error {
	// Ensure to implement a handler case in the type switch below for each requested type.
	eventStream, err := client.connectEventStream([]string{
		typeStateChange,
		typeAcknowledgementSet,
		typeAcknowledgementCleared,
		// typeCommentAdded,
		// typeCommentRemoved,
		// typeDowntimeAdded,
		typeDowntimeRemoved,
		typeDowntimeStarted,
		typeDowntimeTriggered,
		typeFlapping,
	})
	if err != nil {
		return err
	}
	defer func() { _ = eventStream.Close() }()

	select {
	case <-client.Ctx.Done():
		client.Logger.Warnw("Cannot request catch-up-phase as context is finished", zap.Error(client.Ctx.Err()))
		return client.Ctx.Err()
	case client.catchupPhaseRequest <- struct{}{}:
	}

	client.Logger.Info("Start listening on Icinga 2 Event Stream")

	lineScanner := bufio.NewScanner(eventStream)
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
			// Only process HARD states
			if respT.StateType == StateTypeSoft {
				client.Logger.Debugf("Skipping SOFT State Change, %#v", respT)
				continue
			}

			ev, err = client.buildHostServiceEvent(client.Ctx, respT.CheckResult, respT.State, respT.Host, respT.Service)
			evTime = respT.Timestamp.Time()
		case *AcknowledgementSet:
			ev, err = client.buildAcknowledgementEvent(client.Ctx, respT.Host, respT.Service, respT.Author, respT.Comment, false)
			evTime = respT.Timestamp.Time()
		case *AcknowledgementCleared:
			ev, err = client.buildAcknowledgementEvent(client.Ctx, respT.Host, respT.Service, "", "", true)
			evTime = respT.Timestamp.Time()
		// case *CommentAdded:
		// case *CommentRemoved:
		// case *DowntimeAdded:
		case *DowntimeRemoved:
			ev, err = client.buildDowntimeEvent(client.Ctx, respT.Downtime, false)
			evTime = respT.Timestamp.Time()
		case *DowntimeStarted:
			if !respT.Downtime.IsFixed {
				// This may never happen, but Icinga 2 does the same thing, and we need to ignore the start
				// event for flexible downtime, as there will definitely be a triggered event for it.
				client.Logger.Debugf("Skipping flexible downtime start event, %#v", respT)
				continue
			}

			ev, err = client.buildDowntimeEvent(client.Ctx, respT.Downtime, true)
			evTime = respT.Timestamp.Time()
		case *DowntimeTriggered:
			if respT.Downtime.IsFixed {
				// Fixed downtimes generate two events (start, triggered), the latter applies here and must
				// be ignored, since we're going to process its start event to avoid duplicated notifications.
				client.Logger.Debugf("Skipping fixed downtime triggered event, %#v", respT)
				continue
			}

			ev, err = client.buildDowntimeEvent(client.Ctx, respT.Downtime, true)
			evTime = respT.Timestamp.Time()
		case *Flapping:
			ev, err = client.buildFlappingEvent(client.Ctx, respT.Host, respT.Service, respT.IsFlapping)
			evTime = respT.Timestamp.Time()
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
