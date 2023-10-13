package eventstream

import (
	"encoding/json"
	"fmt"
)

// Comment represents the Icinga 2 API Comment object.
//
// NOTE: An empty Service field indicates a host comment.
//
// https://icinga.com/docs/icinga-2/latest/doc/09-object-types/#objecttype-comment
type Comment struct {
	Host    string `json:"host_name"`
	Service string `json:"service_name,omitempty"`
	Author  string `json:"author"`
	Text    string `json:"text"`
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
// https://icinga.com/docs/icinga-2/latest/doc/09-object-types/#objecttype-downtime
type Downtime struct {
	Host    string `json:"host_name"`
	Service string `json:"service_name,omitempty"`
	Author  string `json:"author"`
	Comment string `json:"comment"`
}

// StateChange represents the Icinga 2 API Event Stream StateChange response for host/service state changes.
//
// NOTE: An empty Service field indicates a host service.
//
// https://icinga.com/docs/icinga-2/latest/doc/12-icinga2-api/#event-stream-type-statechange
type StateChange struct {
	Timestamp       float64     `json:"timestamp"` // TODO: own type for float64 UNIX time stamp
	Host            string      `json:"host"`
	Service         string      `json:"service,omitempty"`
	State           int         `json:"state"`      // TODO: own type for states (OK Warning Critical Unknown Up Down)
	StateType       int         `json:"state_type"` // TODO: own type for state types (0 = SOFT, 1 = HARD)
	CheckResult     CheckResult `json:"check_result"`
	DowntimeDepth   int         `json:"downtime_depth"`
	Acknowledgement bool        `json:"acknowledgement"`
}

// AcknowledgementSet represents the Icinga 2 API Event Stream AcknowledgementSet response for acknowledgements set on hosts/services.
//
// NOTE: An empty Service field indicates a host acknowledgement.
//
// https://icinga.com/docs/icinga-2/latest/doc/12-icinga2-api/#event-stream-type-acknowledgementset
type AcknowledgementSet struct {
	Timestamp float64 `json:"timestamp"` // TODO: own type for float64 UNIX time stamp
	Host      string  `json:"host"`
	Service   string  `json:"service,omitempty"`
	State     int     `json:"state"`      // TODO: own type for states (OK Warning Critical Unknown Up Down)
	StateType int     `json:"state_type"` // TODO: own type for state types (0 = SOFT, 1 = HARD)
	Author    string  `json:"author"`
	Comment   string  `json:"comment"`
}

// AcknowledgementCleared represents the Icinga 2 API Event Stream AcknowledgementCleared response for acknowledgements cleared on hosts/services.
//
// NOTE: An empty Service field indicates a host acknowledgement.
//
// https://icinga.com/docs/icinga-2/latest/doc/12-icinga2-api/#event-stream-type-acknowledgementcleared
type AcknowledgementCleared struct {
	Timestamp float64 `json:"timestamp"` // TODO: own type for float64 UNIX time stamp
	Host      string  `json:"host"`
	Service   string  `json:"service,omitempty"`
	State     int     `json:"state"`      // TODO: own type for states (OK Warning Critical Unknown Up Down)
	StateType int     `json:"state_type"` // TODO: own type for state types (0 = SOFT, 1 = HARD)
}

// CommentAdded represents the Icinga 2 API Event Stream CommentAdded response for added host/service comments.
//
// https://icinga.com/docs/icinga-2/latest/doc/12-icinga2-api/#event-stream-type-commentadded
type CommentAdded struct {
	Timestamp float64 `json:"timestamp"` // TODO: own type for float64 UNIX time stamp
	Comment   Comment `json:"comment"`
}

// CommentRemoved represents the Icinga 2 API Event Stream CommentRemoved response for removed host/service comments.
//
// https://icinga.com/docs/icinga-2/latest/doc/12-icinga2-api/#event-stream-type-commentremoved
type CommentRemoved struct {
	Timestamp float64 `json:"timestamp"` // TODO: own type for float64 UNIX time stamp
	Comment   Comment `json:"comment"`
}

// DowntimeAdded represents the Icinga 2 API Event Stream DowntimeAdded response for added downtimes on host/services.
//
// https://icinga.com/docs/icinga-2/latest/doc/12-icinga2-api/#event-stream-type-downtimeadded
type DowntimeAdded struct {
	Timestamp float64  `json:"timestamp"` // TODO: own type for float64 UNIX time stamp
	Downtime  Downtime `json:"downtime"`
}

// DowntimeRemoved represents the Icinga 2 API Event Stream DowntimeRemoved response for removed downtimes on host/services.
//
// https://icinga.com/docs/icinga-2/latest/doc/12-icinga2-api/#event-stream-type-commentremoved
type DowntimeRemoved struct {
	Timestamp float64  `json:"timestamp"` // TODO: own type for float64 UNIX time stamp
	Downtime  Downtime `json:"downtime"`
}

// DowntimeStarted represents the Icinga 2 API Event Stream DowntimeStarted response for started downtimes on host/services.
//
// https://icinga.com/docs/icinga-2/latest/doc/12-icinga2-api/#event-stream-type-downtimestarted
type DowntimeStarted struct {
	Timestamp float64  `json:"timestamp"` // TODO: own type for float64 UNIX time stamp
	Downtime  Downtime `json:"downtime"`
}

// DowntimeTriggered represents the Icinga 2 API Event Stream DowntimeTriggered response for triggered downtimes on host/services.
//
// https://icinga.com/docs/icinga-2/latest/doc/12-icinga2-api/#event-stream-type-downtimetriggered
type DowntimeTriggered struct {
	Timestamp float64  `json:"timestamp"` // TODO: own type for float64 UNIX time stamp
	Downtime  Downtime `json:"downtime"`
}

// UnmarshalEventStreamResponse unmarshal a JSON response line from the Icinga 2 API Event Stream.
func UnmarshalEventStreamResponse(data []byte) (any, error) {
	// Due to the overlapping fields of the different Event Stream response objects, a struct composition with
	// decompositions in different variables will result in multiple manual fixes. Thus, a two-way deserialization
	// was chosen which selects the target type based on the first parsed type field.

	var responseType string
	err := json.Unmarshal(data, &struct {
		Type *string `json:"type"`
	}{&responseType})
	if err != nil {
		return nil, err
	}

	switch responseType {
	case "StateChange":
		resp := StateChange{}
		err = json.Unmarshal(data, &resp)
		return resp, err

	case "AcknowledgementSet":
		resp := AcknowledgementSet{}
		err = json.Unmarshal(data, &resp)
		return resp, err

	case "AcknowledgementCleared":
		resp := AcknowledgementCleared{}
		err = json.Unmarshal(data, &resp)
		return resp, err

	case "CommentAdded":
		resp := CommentAdded{}
		err = json.Unmarshal(data, &resp)
		return resp, err

	case "CommentRemoved":
		resp := CommentRemoved{}
		err = json.Unmarshal(data, &resp)
		return resp, err

	case "DowntimeAdded":
		resp := DowntimeAdded{}
		err = json.Unmarshal(data, &resp)
		return resp, err

	case "DowntimeRemoved":
		resp := DowntimeRemoved{}
		err = json.Unmarshal(data, &resp)
		return resp, err

	case "DowntimeStarted":
		resp := DowntimeStarted{}
		err = json.Unmarshal(data, &resp)
		return resp, err

	case "DowntimeTriggered":
		resp := DowntimeTriggered{}
		err = json.Unmarshal(data, &resp)
		return resp, err

	default:
		return nil, fmt.Errorf("unsupported type %q", responseType)
	}
}
