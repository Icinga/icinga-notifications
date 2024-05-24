package icinga2

import (
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
func isMuted(checkable *ObjectQueriesResult[HostServiceRuntimeAttributes]) bool {
	return checkable.Attrs.IsFlapping || checkable.Attrs.Acknowledgement != AcknowledgementNone || checkable.Attrs.DowntimeDepth != 0
}
