package eventstream

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go.uber.org/zap"
	"math"
	"net/http"
	"net/url"
	"slices"
	"time"
)

// This method contains Icinga 2 API related methods which are not directly related to the Event Stream.

// queryObjectsApi takes a Request, executes it and hopefully returns an array of .
func (client *Client) queryObjectsApi(req *http.Request) ([]ObjectQueriesResult, error) {
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

// queryObjectsApiDirect performs a direct resp. "fast" API query against a specific object identified by its name.
func (client *Client) queryObjectsApiDirect(objType, objName string) ([]ObjectQueriesResult, error) {
	apiUrl, err := url.JoinPath(client.ApiHost, "/v1/objects/", objType+"s/", objName)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(client.Ctx, http.MethodGet, apiUrl, nil)
	if err != nil {
		return nil, err
	}

	req.SetBasicAuth(client.ApiBasicAuthUser, client.ApiBasicAuthPass)
	req.Header.Set("Accept", "application/json")

	return client.queryObjectsApi(req)
}

// queryObjectsApiQuery sends a query to the Icinga 2 API /v1/objects to receive data of the given objType.
func (client *Client) queryObjectsApiQuery(objType string, query map[string]any) ([]ObjectQueriesResult, error) {
	reqBody, err := json.Marshal(query)
	if err != nil {
		return nil, err
	}

	apiUrl, err := url.JoinPath(client.ApiHost, "/v1/objects/", objType+"s")
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(client.Ctx, http.MethodPost, apiUrl, bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}

	req.SetBasicAuth(client.ApiBasicAuthUser, client.ApiBasicAuthPass)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Http-Method-Override", "GET")

	return client.queryObjectsApi(req)
}

// fetchHostGroup fetches all Host Groups for this host.
func (client *Client) fetchHostGroups(host string) ([]string, error) {
	objQueriesResults, err := client.queryObjectsApiDirect("host", host)
	if err != nil {
		return nil, err
	}
	if len(objQueriesResults) != 1 {
		return nil, fmt.Errorf("expected exactly one result for host %q instead of %d", host, len(objQueriesResults))
	}

	attrs, ok := objQueriesResults[0].Attrs.(*HostServiceRuntimeAttributes)
	if !ok {
		return nil, fmt.Errorf("queried object's attrs are of wrong type %T", attrs)
	}
	return attrs.Groups, nil
}

// fetchServiceGroups fetches all Service Groups for this service on this host.
func (client *Client) fetchServiceGroups(host, service string) ([]string, error) {
	objQueriesResults, err := client.queryObjectsApiDirect("service", host+"!"+service)
	if err != nil {
		return nil, err
	}
	if len(objQueriesResults) != 1 {
		return nil, fmt.Errorf("expected exactly one result for service %q instead of %d", host+"!"+service, len(objQueriesResults))
	}

	attrs, ok := objQueriesResults[0].Attrs.(*HostServiceRuntimeAttributes)
	if !ok {
		return nil, fmt.Errorf("queried object's attrs are of wrong type %T", attrs)
	}
	return attrs.Groups, nil
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

	objQueriesResults, err := client.queryObjectsApiQuery("comment",
		map[string]any{"filter": filterExpr, "filter_vars": filterVars})
	if err != nil {
		return nil, err
	}
	if len(objQueriesResults) == 0 {
		return nil, fmt.Errorf("found no ACK Comments found for %q", filterExpr)
	}

	comments := make([]*Comment, len(objQueriesResults))
	for i, objQueriesResult := range objQueriesResults {
		c, ok := objQueriesResult.Attrs.(*Comment)
		if !ok {
			return nil, fmt.Errorf("queried object's attrs are of wrong type %T", c)
		}
		comments[i] = c
	}

	slices.SortFunc(comments, func(a, b *Comment) int {
		distA := a.EntryTime.Time.Sub(ackTime).Abs()
		distB := b.EntryTime.Time.Sub(ackTime).Abs()
		return int(distA - distB)
	})
	if comments[0].EntryTime.Sub(ackTime).Abs() > time.Second {
		return nil, fmt.Errorf("found no ACK Comment for %q close to %v", filterExpr, ackTime)
	}

	return comments[0], nil
}

// checkMissedChanges queries for Service or Host objects with a specific filter to handle missed elements.
func (client *Client) checkMissedChanges(objType, filterExpr string, attrsCallbackFn func(attrs *HostServiceRuntimeAttributes, host, service string)) {
	objQueriesResults, err := client.queryObjectsApiQuery(objType, map[string]any{"filter": filterExpr})
	if err != nil {
		client.Logger.Errorf("Quering %ss from API failed, %v", objType, err)
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

		attrs, ok := objQueriesResult.Attrs.(*HostServiceRuntimeAttributes)
		if !ok {
			client.Logger.Errorf("Queried %s API response object's attrs are of wrong type %T", objType, attrs)
			continue
		}

		var hostName, serviceName string
		switch objQueriesResult.Type {
		case "Host":
			hostName = attrs.Name

		case "Service":
			hostName = attrs.Host
			serviceName = attrs.Name

		default:
			client.Logger.Errorf("Querying API delivered a %q object when expecting %s", objQueriesResult.Type, objType)
			continue
		}

		attrsCallbackFn(attrs, hostName, serviceName)
	}
}

// checkMissedStateChanges fetches missed Host or Service state changes and feeds them into the handler.
func (client *Client) checkMissedStateChanges(objType string, since time.Time) {
	filterExpr := fmt.Sprintf("%s.last_state_change > %f", objType, float64(since.UnixMicro())/1_000_000.0)

	client.checkMissedChanges(objType, filterExpr, func(attrs *HostServiceRuntimeAttributes, host, service string) {
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

	client.checkMissedChanges(objType, filterExpr, func(attrs *HostServiceRuntimeAttributes, host, service string) {
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
