package eventstream

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

// Icinga2Time is a custom time.Time type for JSON unmarshalling from Icinga 2's unix timestamp type.
type Icinga2Time struct {
	time.Time
}

func (iciTime *Icinga2Time) UnmarshalJSON(data []byte) error {
	unixTs, err := strconv.ParseFloat(string(data), 64)
	if err != nil {
		return err
	}

	unixMicro := int64(unixTs * 1_000_000)
	iciTime.Time = time.UnixMicro(unixMicro)
	return nil
}

// Comment represents the Icinga 2 API Comment object.
//
// NOTE:
//   - An empty Service field indicates a host comment.
//   - The optional EntryType should be User = 1, Downtime = 2, Flapping = 3, Acknowledgement = 4.
//
// https://icinga.com/docs/icinga-2/latest/doc/09-object-types/#objecttype-comment
type Comment struct {
	Host      string `json:"host_name"`
	Service   string `json:"service_name"`
	Author    string `json:"author"`
	Text      string `json:"text"`
	EntryType int    `json:"entry_type"`
}

// CheckResult represents the Icinga 2 API CheckResult object.
//
// https://icinga.com/docs/icinga-2/latest/doc/08-advanced-topics/#advanced-value-types-checkresult
type CheckResult struct {
	ExitStatus int    `json:"exit_status"`
	Output     string `json:"output"`
}

// Downtime represents the Icinga 2 API Downtime object.
//
// NOTE:
//   - An empty Service field indicates a host downtime.
//
// https://icinga.com/docs/icinga-2/latest/doc/09-object-types/#objecttype-downtime
type Downtime struct {
	Host    string `json:"host_name"`
	Service string `json:"service_name"`
	Author  string `json:"author"`
	Comment string `json:"comment"`
}

// HostServiceRuntimeAttributes are common attributes of both Host and Service objects.
//
// When catching up potentially missed changes, the following fields are holding relevant changes which, fortunately,
// are identical for Icinga 2 Host and Service objects.
//
// According to the documentation, neither the Host nor the Service name is part of the attributes for Host resp.
// Service objects. However, next to being part of the wrapping API response, see ObjectQueriesResult, it is also
// available in the "__name" attribute, reflected in the Name field. For Service objects, it is "${host}!${service}".
// Furthermore, Service objects have a required non-empty reference to their Host.
//
// NOTE:
//   - Host is empty for Host objects; Host contains the Service's Host object name for Services.
//   - State might be 0 = UP, 1 = DOWN for hosts and 0 = OK, 1 = WARNING, 2 = CRITICAL, 3 = UNKNOWN for services.
//   - Acknowledgement type is 0 = NONE, 1 = NORMAL, 2 = STICKY.
//
// https://icinga.com/docs/icinga-2/latest/doc/09-object-types/#host
// https://icinga.com/docs/icinga-2/latest/doc/09-object-types/#service
type HostServiceRuntimeAttributes struct {
	Name            string      `json:"__name"`
	Host            string      `json:"host_name,omitempty"`
	State           int         `json:"state"`
	LastCheckResult CheckResult `json:"last_check_result"`
	LastStateChange Icinga2Time `json:"last_state_change"`
	DowntimeDepth   int         `json:"downtime_depth"`
	Acknowledgement int         `json:"acknowledgement"`
}

// ObjectQueriesResult represents the Icinga 2 API Object Queries Result wrapper object.
//
// https://icinga.com/docs/icinga-2/latest/doc/12-icinga2-api/#object-queries-result
type ObjectQueriesResult struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Attrs any    `json:"attrs"`
}

func (objQueriesRes *ObjectQueriesResult) UnmarshalJSON(bytes []byte) error {
	var responseAttrs json.RawMessage
	err := json.Unmarshal(bytes, &struct {
		Name  *string          `json:"name"`
		Type  *string          `json:"type"`
		Attrs *json.RawMessage `json:"attrs"`
	}{&objQueriesRes.Name, &objQueriesRes.Type, &responseAttrs})
	if err != nil {
		return err
	}

	switch objQueriesRes.Type {
	case "Comment":
		objQueriesRes.Attrs = new(Comment)
	case "Downtime":
		objQueriesRes.Attrs = new(Downtime)
	case "Host", "Service":
		objQueriesRes.Attrs = new(HostServiceRuntimeAttributes)
	default:
		return fmt.Errorf("unsupported type %q", objQueriesRes.Type)
	}

	return json.Unmarshal(responseAttrs, objQueriesRes.Attrs)
}

// The following constants list all implemented Icinga 2 API Event Stream Types to be used as a const instead of
// (mis)typing the name at multiple places.
const (
	typeStateChange            = "StateChange"
	typeAcknowledgementSet     = "AcknowledgementSet"
	typeAcknowledgementCleared = "AcknowledgementCleared"
	typeCommentAdded           = "CommentAdded"
	typeCommentRemoved         = "CommentRemoved"
	typeDowntimeAdded          = "DowntimeAdded"
	typeDowntimeRemoved        = "DowntimeRemoved"
	typeDowntimeStarted        = "DowntimeStarted"
	typeDowntimeTriggered      = "DowntimeTriggered"
)

// StateChange represents the Icinga 2 API Event Stream StateChange response for host/service state changes.
//
// NOTE:
//   - An empty Service field indicates a host state change.
//   - State might be 0 = UP, 1 = DOWN for hosts and 0 = OK, 1 = WARNING, 2 = CRITICAL, 3 = UNKNOWN for services.
//   - StateType might be 0 = SOFT, 1 = HARD.
//
// https://icinga.com/docs/icinga-2/latest/doc/12-icinga2-api/#event-stream-type-statechange
type StateChange struct {
	Timestamp       Icinga2Time `json:"timestamp"`
	Host            string      `json:"host"`
	Service         string      `json:"service"`
	State           int         `json:"state"`
	StateType       int         `json:"state_type"`
	CheckResult     CheckResult `json:"check_result"`
	DowntimeDepth   int         `json:"downtime_depth"`
	Acknowledgement bool        `json:"acknowledgement"`
}

// AcknowledgementSet represents the Icinga 2 API Event Stream AcknowledgementSet response for acknowledgements set on hosts/services.
//
// NOTE:
//   - An empty Service field indicates a host acknowledgement.
//   - State might be 0 = UP, 1 = DOWN for hosts and 0 = OK, 1 = WARNING, 2 = CRITICAL, 3 = UNKNOWN for services.
//   - StateType might be 0 = SOFT, 1 = HARD.
//
// https://icinga.com/docs/icinga-2/latest/doc/12-icinga2-api/#event-stream-type-acknowledgementset
type AcknowledgementSet struct {
	Timestamp Icinga2Time `json:"timestamp"`
	Host      string      `json:"host"`
	Service   string      `json:"service"`
	State     int         `json:"state"`
	StateType int         `json:"state_type"`
	Author    string      `json:"author"`
	Comment   string      `json:"comment"`
}

// AcknowledgementCleared represents the Icinga 2 API Event Stream AcknowledgementCleared response for acknowledgements cleared on hosts/services.
//
// NOTE:
//   - An empty Service field indicates a host acknowledgement.
//   - State might be 0 = UP, 1 = DOWN for hosts and 0 = OK, 1 = WARNING, 2 = CRITICAL, 3 = UNKNOWN for services.
//   - StateType might be 0 = SOFT, 1 = HARD.
//
// https://icinga.com/docs/icinga-2/latest/doc/12-icinga2-api/#event-stream-type-acknowledgementcleared
type AcknowledgementCleared struct {
	Timestamp Icinga2Time `json:"timestamp"`
	Host      string      `json:"host"`
	Service   string      `json:"service"`
	State     int         `json:"state"`
	StateType int         `json:"state_type"`
}

// CommentAdded represents the Icinga 2 API Event Stream CommentAdded response for added host/service comments.
//
// https://icinga.com/docs/icinga-2/latest/doc/12-icinga2-api/#event-stream-type-commentadded
type CommentAdded struct {
	Timestamp Icinga2Time `json:"timestamp"`
	Comment   Comment     `json:"comment"`
}

// CommentRemoved represents the Icinga 2 API Event Stream CommentRemoved response for removed host/service comments.
//
// https://icinga.com/docs/icinga-2/latest/doc/12-icinga2-api/#event-stream-type-commentremoved
type CommentRemoved struct {
	Timestamp Icinga2Time `json:"timestamp"`
	Comment   Comment     `json:"comment"`
}

// DowntimeAdded represents the Icinga 2 API Event Stream DowntimeAdded response for added downtimes on host/services.
//
// https://icinga.com/docs/icinga-2/latest/doc/12-icinga2-api/#event-stream-type-downtimeadded
type DowntimeAdded struct {
	Timestamp Icinga2Time `json:"timestamp"`
	Downtime  Downtime    `json:"downtime"`
}

// DowntimeRemoved represents the Icinga 2 API Event Stream DowntimeRemoved response for removed downtimes on host/services.
//
// https://icinga.com/docs/icinga-2/latest/doc/12-icinga2-api/#event-stream-type-commentremoved
type DowntimeRemoved struct {
	Timestamp Icinga2Time `json:"timestamp"`
	Downtime  Downtime    `json:"downtime"`
}

// DowntimeStarted represents the Icinga 2 API Event Stream DowntimeStarted response for started downtimes on host/services.
//
// https://icinga.com/docs/icinga-2/latest/doc/12-icinga2-api/#event-stream-type-downtimestarted
type DowntimeStarted struct {
	Timestamp Icinga2Time `json:"timestamp"`
	Downtime  Downtime    `json:"downtime"`
}

// DowntimeTriggered represents the Icinga 2 API Event Stream DowntimeTriggered response for triggered downtimes on host/services.
//
// https://icinga.com/docs/icinga-2/latest/doc/12-icinga2-api/#event-stream-type-downtimetriggered
type DowntimeTriggered struct {
	Timestamp Icinga2Time `json:"timestamp"`
	Downtime  Downtime    `json:"downtime"`
}

// UnmarshalEventStreamResponse unmarshal a JSON response line from the Icinga 2 API Event Stream.
//
// The function expects an Icinga 2 API Event Stream Response in its JSON form and tries to unmarshal it into one of the
// implemented types based on its type argument. Thus, the returned any value will be a pointer to such a struct type.
func UnmarshalEventStreamResponse(bytes []byte) (any, error) {
	// Due to the overlapping fields of the different Event Stream response objects, a struct composition with
	// decompositions in different variables will result in multiple manual fixes. Thus, a two-way deserialization
	// was chosen which selects the target type based on the first parsed type field.

	var responseType string
	err := json.Unmarshal(bytes, &struct {
		Type *string `json:"type"`
	}{&responseType})
	if err != nil {
		return nil, err
	}

	var resp any
	switch responseType {
	case typeStateChange:
		resp = new(StateChange)
	case typeAcknowledgementSet:
		resp = new(AcknowledgementSet)
	case typeAcknowledgementCleared:
		resp = new(AcknowledgementCleared)
	case typeCommentAdded:
		resp = new(CommentAdded)
	case typeCommentRemoved:
		resp = new(CommentRemoved)
	case typeDowntimeAdded:
		resp = new(DowntimeAdded)
	case typeDowntimeRemoved:
		resp = new(DowntimeRemoved)
	case typeDowntimeStarted:
		resp = new(DowntimeStarted)
	case typeDowntimeTriggered:
		resp = new(DowntimeTriggered)
	default:
		return nil, fmt.Errorf("unsupported type %q", responseType)
	}
	err = json.Unmarshal(bytes, resp)
	return resp, err
}
