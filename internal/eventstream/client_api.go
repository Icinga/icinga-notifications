package eventstream

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
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
	req, err := http.NewRequestWithContext(client.Ctx, http.MethodGet, client.ApiHost+"/v1/objects/"+objType+"s/"+objName, nil)
	if err != nil {
		return nil, err
	}

	req.SetBasicAuth(client.ApiBasicAuthUser, client.ApiBasicAuthPass)
	req.Header.Set("Accept", "application/json")

	return client.queryObjectsApi(req)
}

// queryObjectsApiQuery sends a query to the Icinga 2 API /v1/objects to receive data of the given objType.
func (client *Client) queryObjectsApiQuery(objType string, payload map[string]any) ([]ObjectQueriesResult, error) {
	reqBody, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(client.Ctx, http.MethodPost, client.ApiHost+"/v1/objects/"+objType+"s", bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}

	req.SetBasicAuth(client.ApiBasicAuthUser, client.ApiBasicAuthPass)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Http-Method-Override", "GET")

	return client.queryObjectsApi(req)
}

// queryObjectApiSince retrieves all objects of the given type, e.g., "host" or "service", with a state change after the
// passed time.
func (client *Client) queryObjectApiSince(objType string, since time.Time) ([]ObjectQueriesResult, error) {
	return client.queryObjectsApiQuery(
		objType,
		map[string]any{
			"filter": fmt.Sprintf("%s.last_state_change>%f", objType, float64(since.UnixMicro())/1_000_000.0),
		})
}

// checkMissedObjects fetches all objects of the requested objType (host or service) from the API and sends those to the
// handleEvent method to be eventually dispatched to the callback.
func (client *Client) checkMissedObjects(objType string) {
	client.eventsHandlerMutex.RLock()
	objQueriesResults, err := client.queryObjectApiSince(objType, client.eventsLastTs)
	client.eventsHandlerMutex.RUnlock()

	if err != nil {
		client.Logger.Errorf("Quering %ss from API failed, %v", objType, err)
		return
	}
	if len(objQueriesResults) == 0 {
		return
	}

	client.Logger.Infof("Querying %ss from API resulted in %d objects to replay", objType, len(objQueriesResults))

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
			serviceName = attrs.Name[len(attrs.Host+"!"):]

		default:
			client.Logger.Errorf("Querying API delivered a %q object when expecting %s", objQueriesResult.Type, objType)
			continue
		}

		ev := client.buildHostServiceEvent(attrs.LastCheckResult, attrs.State, hostName, serviceName)
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
		if client.Ctx.Err() != nil {
			return client.Ctx.Err()
		}
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
