package icinga2

import (
	"context"
	"net/url"
	"strings"
)

// rawurlencode mimics PHP's rawurlencode to be used for parameter encoding.
//
// Icinga Web uses rawurldecode instead of urldecode, which, as its main difference, does not honor the plus char ('+')
// as a valid substitution for space (' '). Unfortunately, Go's url.QueryEscape does this very substitution and
// url.PathEscape does a bit too less and has a misleading name on top.
//
//   - https://www.php.net/manual/en/function.rawurlencode.php
//   - https://github.com/php/php-src/blob/php-8.2.12/ext/standard/url.c#L538
func rawurlencode(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}

// isMuted returns true if the given checkable is either in Downtime, Flapping or acknowledged, otherwise false.
//
// When the checkable is Flapping, and neither the flapping detection for that Checkable nor for the entire zone is
// enabled, this will always return false.
//
// Returns an error if it fails to query the status of IcingaApplication from the /v1/status endpoint.
func isMuted(ctx context.Context, client *Client, checkable *ObjectQueriesResult[HostServiceRuntimeAttributes]) (bool, error) {
	if checkable.Attrs.Acknowledgement != AcknowledgementNone || checkable.Attrs.DowntimeDepth != 0 {
		return true, nil
	}

	if checkable.Attrs.IsFlapping && checkable.Attrs.EnableFlapping {
		status, err := client.fetchIcingaAppStatus(ctx)
		if err != nil {
			return false, err
		}

		return status.App.EnableFlapping, nil
	}

	return false, nil
}
