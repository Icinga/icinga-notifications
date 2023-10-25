package eventstream

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go.uber.org/zap"
	"io"
	"math"
	"net/http"
	"net/url"
	"slices"
	"time"
)

// This method contains Icinga 2 API related methods which are not directly related to the Event Stream.

// extractObjectQueriesResult parses a typed ObjectQueriesResult array out of a JSON io.ReaderCloser.
//
// As Go 1.21 does not allow type parameters in methods[0], the logic was extracted into a function transforming the
// JSON response - passed as an io.ReaderCloser which will be closed within this function - into the typed response to
// be used within the methods below.
//
// [0] https://github.com/golang/go/issues/49085
func extractObjectQueriesResult[T Comment | Downtime | HostServiceRuntimeAttributes](jsonResp io.ReadCloser) ([]ObjectQueriesResult[T], error) {
	defer func() { _ = jsonResp.Close() }()

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
func (client *Client) queryObjectsApi(urlPaths []string, method string, body io.Reader, headers map[string]string) (io.ReadCloser, error) {
	apiUrl, err := url.JoinPath(client.ApiHost, urlPaths...)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(client.Ctx, method, apiUrl, body)
	if err != nil {
		return nil, err
	}

	req.SetBasicAuth(client.ApiBasicAuthUser, client.ApiBasicAuthPass)
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	httpClient := &http.Client{Transport: &client.ApiHttpTransport}
	res, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	if res.StatusCode != http.StatusOK {
		_ = res.Body.Close()
		return nil, fmt.Errorf("unexpected status code %d", res.StatusCode)
	}

	return res.Body, nil
}

// queryObjectsApiDirect performs a direct resp. "fast" API query against a specific object identified by its name.
func (client *Client) queryObjectsApiDirect(objType, objName string) (io.ReadCloser, error) {
	return client.queryObjectsApi(
		[]string{"/v1/objects/", objType + "s/", objName},
		http.MethodGet,
		nil,
		map[string]string{"Accept": "application/json"})
}

// queryObjectsApiQuery sends a query to the Icinga 2 API /v1/objects to receive data of the given objType.
func (client *Client) queryObjectsApiQuery(objType string, query map[string]any) (io.ReadCloser, error) {
	reqBody, err := json.Marshal(query)
	if err != nil {
		return nil, err
	}

	return client.queryObjectsApi(
		[]string{"/v1/objects/", objType + "s"},
		http.MethodPost,
		bytes.NewReader(reqBody),
		map[string]string{
			"Accept":                 "application/json",
			"Content-Type":           "application/json",
			"X-Http-Method-Override": "GET",
		})
}

// fetchHostGroup fetches all Host Groups for this host.
func (client *Client) fetchHostGroups(host string) ([]string, error) {
	jsonRaw, err := client.queryObjectsApiDirect("host", host)
	if err != nil {
		return nil, err
	}
	objQueriesResults, err := extractObjectQueriesResult[HostServiceRuntimeAttributes](jsonRaw)
	if err != nil {
		return nil, err
	}

	if len(objQueriesResults) != 1 {
		return nil, fmt.Errorf("expected exactly one result for host %q instead of %d", host, len(objQueriesResults))
	}

	return objQueriesResults[0].Attrs.Groups, nil
}

// fetchServiceGroups fetches all Service Groups for this service on this host.
func (client *Client) fetchServiceGroups(host, service string) ([]string, error) {
	jsonRaw, err := client.queryObjectsApiDirect("host", host)
	if err != nil {
		return nil, err
	}
	objQueriesResults, err := extractObjectQueriesResult[HostServiceRuntimeAttributes](jsonRaw)
	if err != nil {
		return nil, err
	}

	if len(objQueriesResults) != 1 {
		return nil, fmt.Errorf("expected exactly one result for service %q instead of %d", host+"!"+service, len(objQueriesResults))
	}

	return objQueriesResults[0].Attrs.Groups, nil
}

// fetchAcknowledgementComment fetches an Acknowledgement Comment for a Host (empty service) or for a Service at a Host.
//
// Unfortunately, there is no direct link between ACK'ed Host or Service objects and their acknowledgement Comment. The
// closest we can do, is query for Comments with the Acknowledgement Service Type and the host/service name. In addition,
// the Host's resp. Service's AcknowledgementLastChange field has NOT the same timestamp as the Comment; there is a
// difference of some milliseconds. As there might be even multiple ACK comments, we have to find the closest one.
func (client *Client) fetchAcknowledgementComment(host, service string, ackTime time.Time) (*Comment, error) {
	filterExpr := "comment.entry_type == 4 && comment.host_name == comment_host_name"
	filterVars := map[string]string{"comment_host_name": host}
	if service != "" {
		filterExpr += " && comment.service_name == comment_service_name"
		filterVars["comment_service_name"] = service
	}

	jsonRaw, err := client.queryObjectsApiQuery("comment", map[string]any{"filter": filterExpr, "filter_vars": filterVars})
	if err != nil {
		return nil, err
	}
	objQueriesResults, err := extractObjectQueriesResult[Comment](jsonRaw)
	if err != nil {
		return nil, err
	}

	if len(objQueriesResults) == 0 {
		return nil, fmt.Errorf("found no ACK Comments found for %q", filterExpr)
	}

	slices.SortFunc(objQueriesResults, func(a, b ObjectQueriesResult[Comment]) int {
		distA := a.Attrs.EntryTime.Time.Sub(ackTime).Abs()
		distB := b.Attrs.EntryTime.Time.Sub(ackTime).Abs()
		return int(distA - distB)
	})
	if objQueriesResults[0].Attrs.EntryTime.Sub(ackTime).Abs() > time.Second {
		return nil, fmt.Errorf("found no ACK Comment for %q close to %v", filterExpr, ackTime)
	}

	return &objQueriesResults[0].Attrs, nil
}

// checkMissedChanges queries for Service or Host objects with a specific filter to handle missed elements.
func (client *Client) checkMissedChanges(objType, filterExpr string, attrsCallbackFn func(attrs HostServiceRuntimeAttributes, host, service string)) {
	jsonRaw, err := client.queryObjectsApiQuery(objType, map[string]any{"filter": filterExpr})
	if err != nil {
		client.Logger.Errorf("Quering %ss from API failed, %v", objType, err)
		return
	}
	objQueriesResults, err := extractObjectQueriesResult[HostServiceRuntimeAttributes](jsonRaw)
	if err != nil {
		client.Logger.Errorf("Parsing %ss from API failed, %v", objType, err)
		return
	}

	if len(objQueriesResults) == 0 {
		return
	}

	client.Logger.Infof("Querying %ss from API resulted in %d state changes to replay", objType, len(objQueriesResults))

	for _, objQueriesResult := range objQueriesResults {
		if client.Ctx.Err() != nil {
			client.Logger.Infof("Stopping %s API response processing as context is finished", objType)
			return
		}

		var hostName, serviceName string
		switch objQueriesResult.Type {
		case "Host":
			hostName = objQueriesResult.Attrs.Name

		case "Service":
			hostName = objQueriesResult.Attrs.Host
			serviceName = objQueriesResult.Attrs.Name

		default:
			client.Logger.Errorf("Querying API delivered a %q object when expecting %s", objQueriesResult.Type, objType)
			continue
		}

		attrsCallbackFn(objQueriesResult.Attrs, hostName, serviceName)
	}
}

// checkMissedStateChanges fetches missed Host or Service state changes and feeds them into the handler.
func (client *Client) checkMissedStateChanges(objType string, since time.Time) {
	filterExpr := fmt.Sprintf("%s.last_state_change > %f", objType, float64(since.UnixMicro())/1_000_000.0)

	client.checkMissedChanges(objType, filterExpr, func(attrs HostServiceRuntimeAttributes, host, service string) {
		ev, err := client.buildHostServiceEvent(attrs.LastCheckResult, attrs.State, host, service)
		if err != nil {
			client.Logger.Errorf("Failed to construct Event from %s API: %v", objType, err)
			return
		}

		client.handleEvent(ev, "API "+objType)
	})
}

// checkMissedAcknowledgements fetches missed set Host or Service Acknowledgements and feeds them into the handler.
func (client *Client) checkMissedAcknowledgements(objType string, since time.Time) {
	filterExpr := fmt.Sprintf("%s.acknowledgement && %s.acknowledgement_last_change > %f",
		objType, objType, float64(since.UnixMicro())/1_000_000.0)

	client.checkMissedChanges(objType, filterExpr, func(attrs HostServiceRuntimeAttributes, host, service string) {
		ackComment, err := client.fetchAcknowledgementComment(host, service, attrs.AcknowledgementLastChange.Time)
		if err != nil {
			client.Logger.Errorf("Cannot fetch ACK Comment for Acknowledgement, %v", err)
			return
		}

		ev, err := client.buildAcknowledgementEvent(
			attrs.AcknowledgementLastChange.Time,
			host, service,
			ackComment.Author, ackComment.Text)
		if err != nil {
			client.Logger.Errorf("Failed to construct Event from Acknowledgement %s API: %v", objType, err)
			return
		}

		client.handleEvent(ev, "ACK API "+objType)
	})
}

// waitForApiAvailability reconnects to the Icinga 2 API until it either becomes available or the Client context is done.
func (client *Client) waitForApiAvailability() error {
	apiUrl, err := url.JoinPath(client.ApiHost, "/v1/")
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(client.Ctx, http.MethodGet, apiUrl, nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(client.ApiBasicAuthUser, client.ApiBasicAuthPass)

	// To neither flood the API nor have to wait unnecessary long, at first the exponential function for the backoff
	// time calculation will be used. When numbers are starting to get big, a logarithm will be used instead.
	// 10ms, 27ms, 73ms, 200ms, 545ms, 1.484s, 2.584s, 2.807s, 3s, 3.169s, ...
	backoffDelay := func(i int) time.Duration {
		if i <= 5 {
			return time.Duration(math.Exp(float64(i)) * 10 * float64(time.Millisecond))
		}
		return time.Duration(math.Log2(float64(i)) * float64(time.Second))
	}

	for i := 0; client.Ctx.Err() == nil; i++ {
		time.Sleep(backoffDelay(i))
		client.Logger.Debugw("Try to reestablish an API connection", zap.Int("try", i+1))

		httpClient := &http.Client{
			Transport: &client.ApiHttpTransport,
			Timeout:   100 * time.Millisecond,
		}
		res, err := httpClient.Do(req)
		if err != nil {
			client.Logger.Errorw("Reestablishing an API connection failed", zap.Error(err))
			continue
		}
		_ = res.Body.Close()

		if res.StatusCode != http.StatusOK {
			client.Logger.Errorw("API returns unexpected status code during API reconnection", zap.Int("status", res.StatusCode))
			continue
		}

		client.Logger.Debugw("Successfully reconnected to API", zap.Int("try", i+1))
		return nil
	}
	return client.Ctx.Err()
}