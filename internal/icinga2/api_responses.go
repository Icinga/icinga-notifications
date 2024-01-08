package icinga2

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

// UnixFloat is a custom time.Time type for millisecond Unix timestamp, as used in Icinga 2's API.
type UnixFloat time.Time

// Time returns the time.Time of UnixFloat.
func (t *UnixFloat) Time() time.Time {
	return time.Time(*t)
}

func (t *UnixFloat) UnmarshalJSON(data []byte) error {
	unixTs, err := strconv.ParseFloat(string(data), 64)
	if err != nil {
		return err
	}

	*t = UnixFloat(time.UnixMicro(int64(unixTs * 1_000_000)))
	return nil
}

// The following const values are representing constant integer values, e.g., 0 for an OK state service.
const (
	// ACKNOWLEDGEMENT_* consts are describing an acknowledgement, e.g., from HostServiceRuntimeAttributes.
	ACKNOWLEDGEMENT_NONE   = 0
	ACKNOWLEDGEMENT_NORMAL = 1
	ACKNOWLEDGEMENT_STICKY = 2

	// ENTRY_TYPE_* consts are describing an entry_type, e.g., from Comment.
	ENTRY_TYPE_USER            = 1
	ENTRY_TYPE_DOWNTIME        = 2
	ENTRY_TYPE_FLAPPING        = 3
	ENTRY_TYPE_ACKNOWLEDGEMENT = 4

	// STATE_HOST_* consts are describing a host state, e.g., from CheckResult.
	STATE_HOST_UP   = 0
	STATE_HOST_DOWN = 1

	// STATE_SERVICE_* consts are describing a service state, e.g., from CheckResult.
	STATE_SERVICE_OK       = 0
	STATE_SERVICE_WARNING  = 1
	STATE_SERVICE_CRITICAL = 2
	STATE_SERVICE_UNKNOWN  = 3

	// STATE_TYPE_* consts are describing a state type, e.g., from HostServiceRuntimeAttributes.
	STATE_TYPE_SOFT = 0
	STATE_TYPE_HARD = 1
)

// Comment represents the Icinga 2 API Comment object.
//
// NOTE:
//   - An empty Service field indicates a host comment.
//   - The optional EntryType should be User = ENTRY_TYPE_USER, Downtime = ENTRY_TYPE_DOWNTIME,
//     Flapping = ENTRY_TYPE_FLAPPING, Acknowledgement = ENTRY_TYPE_ACKNOWLEDGEMENT (ENTRY_TYPE_* consts)
//
// https://icinga.com/docs/icinga-2/latest/doc/09-object-types/#objecttype-comment
type Comment struct {
	Host      string    `json:"host_name"`
	Service   string    `json:"service_name"`
	Author    string    `json:"author"`
	Text      string    `json:"text"`
	EntryTime UnixFloat `json:"entry_time"`
	EntryType int       `json:"entry_type"`
}

// CheckResult represents the Icinga 2 API CheckResult object.
//
// https://icinga.com/docs/icinga-2/latest/doc/08-advanced-topics/#advanced-value-types-checkresult
type CheckResult struct {
	ExitStatus     int       `json:"exit_status"`
	Output         string    `json:"output"`
	State          int       `json:"state"`
	ExecutionStart UnixFloat `json:"execution_start"`
	ExecutionEnd   UnixFloat `json:"execution_end"`
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
// NOTE:
//   - Name is either the Host or the Service name.
//   - Host is empty for Host objects; Host contains the Service's Host object name for Services.
//   - State might be STATE_HOST_{UP,DOWN} for hosts or STATE_SERVICE_{OK,WARNING,CRITICAL,UNKNOWN} for services.
//   - StateType might be STATE_TYPE_SOFT or STATE_TYPE_HARD.
//   - Acknowledgement type might be ACKNOWLEDGEMENT_{NONE,NORMAL,STICKY}.
//
// https://icinga.com/docs/icinga-2/latest/doc/09-object-types/#host
// https://icinga.com/docs/icinga-2/latest/doc/09-object-types/#service
type HostServiceRuntimeAttributes struct {
	Name                      string      `json:"name"`
	Host                      string      `json:"host_name,omitempty"`
	Groups                    []string    `json:"groups"`
	State                     int         `json:"state"`
	StateType                 int         `json:"state_type"`
	LastCheckResult           CheckResult `json:"last_check_result"`
	LastStateChange           UnixFloat   `json:"last_state_change"`
	DowntimeDepth             int         `json:"downtime_depth"`
	Acknowledgement           int         `json:"acknowledgement"`
	AcknowledgementLastChange UnixFloat   `json:"acknowledgement_last_change"`
}

// ObjectQueriesResult represents the Icinga 2 API Object Queries Result wrapper object.
//
// https://icinga.com/docs/icinga-2/latest/doc/12-icinga2-api/#object-queries-result
type ObjectQueriesResult[T Comment | Downtime | HostServiceRuntimeAttributes] struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Attrs T      `json:"attrs"`
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
//   - State might be STATE_HOST_{UP,DOWN} for hosts or STATE_SERVICE_{OK,WARNING,CRITICAL,UNKNOWN} for services.
//   - StateType might be STATE_TYPE_SOFT or STATE_TYPE_HARD.
//
// https://icinga.com/docs/icinga-2/latest/doc/12-icinga2-api/#event-stream-type-statechange
type StateChange struct {
	Timestamp       UnixFloat   `json:"timestamp"`
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
//   - State might be STATE_HOST_{UP,DOWN} for hosts or STATE_SERVICE_{OK,WARNING,CRITICAL,UNKNOWN} for services.
//   - StateType might be STATE_TYPE_SOFT or STATE_TYPE_HARD.
//
// https://icinga.com/docs/icinga-2/latest/doc/12-icinga2-api/#event-stream-type-acknowledgementset
type AcknowledgementSet struct {
	Timestamp UnixFloat `json:"timestamp"`
	Host      string    `json:"host"`
	Service   string    `json:"service"`
	State     int       `json:"state"`
	StateType int       `json:"state_type"`
	Author    string    `json:"author"`
	Comment   string    `json:"comment"`
}

// AcknowledgementCleared represents the Icinga 2 API Event Stream AcknowledgementCleared response for acknowledgements cleared on hosts/services.
//
// NOTE:
//   - An empty Service field indicates a host acknowledgement.
//   - State might be STATE_HOST_{UP,DOWN} for hosts or STATE_SERVICE_{OK,WARNING,CRITICAL,UNKNOWN} for services.
//   - StateType might be STATE_TYPE_SOFT or STATE_TYPE_HARD.
//
// https://icinga.com/docs/icinga-2/latest/doc/12-icinga2-api/#event-stream-type-acknowledgementcleared
type AcknowledgementCleared struct {
	Timestamp UnixFloat `json:"timestamp"`
	Host      string    `json:"host"`
	Service   string    `json:"service"`
	State     int       `json:"state"`
	StateType int       `json:"state_type"`
}

// CommentAdded represents the Icinga 2 API Event Stream CommentAdded response for added host/service comments.
//
// https://icinga.com/docs/icinga-2/latest/doc/12-icinga2-api/#event-stream-type-commentadded
type CommentAdded struct {
	Timestamp UnixFloat `json:"timestamp"`
	Comment   Comment   `json:"comment"`
}

// CommentRemoved represents the Icinga 2 API Event Stream CommentRemoved response for removed host/service comments.
//
// https://icinga.com/docs/icinga-2/latest/doc/12-icinga2-api/#event-stream-type-commentremoved
type CommentRemoved struct {
	Timestamp UnixFloat `json:"timestamp"`
	Comment   Comment   `json:"comment"`
}

// DowntimeAdded represents the Icinga 2 API Event Stream DowntimeAdded response for added downtimes on host/services.
//
// https://icinga.com/docs/icinga-2/latest/doc/12-icinga2-api/#event-stream-type-downtimeadded
type DowntimeAdded struct {
	Timestamp UnixFloat `json:"timestamp"`
	Downtime  Downtime  `json:"downtime"`
}

// DowntimeRemoved represents the Icinga 2 API Event Stream DowntimeRemoved response for removed downtimes on host/services.
//
// https://icinga.com/docs/icinga-2/latest/doc/12-icinga2-api/#event-stream-type-commentremoved
type DowntimeRemoved struct {
	Timestamp UnixFloat `json:"timestamp"`
	Downtime  Downtime  `json:"downtime"`
}

// DowntimeStarted represents the Icinga 2 API Event Stream DowntimeStarted response for started downtimes on host/services.
//
// https://icinga.com/docs/icinga-2/latest/doc/12-icinga2-api/#event-stream-type-downtimestarted
type DowntimeStarted struct {
	Timestamp UnixFloat `json:"timestamp"`
	Downtime  Downtime  `json:"downtime"`
}

// DowntimeTriggered represents the Icinga 2 API Event Stream DowntimeTriggered response for triggered downtimes on host/services.
//
// https://icinga.com/docs/icinga-2/latest/doc/12-icinga2-api/#event-stream-type-downtimetriggered
type DowntimeTriggered struct {
	Timestamp UnixFloat `json:"timestamp"`
	Downtime  Downtime  `json:"downtime"`
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
