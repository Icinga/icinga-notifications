package icinga2

import (
	"encoding/json"
	"fmt"
	"go.uber.org/zap/zapcore"
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
	// Acknowledgement* consts are describing an acknowledgement, e.g., from HostServiceRuntimeAttributes.
	AcknowledgementNone   = 0
	AcknowledgementNormal = 1
	AcknowledgementSticky = 2

	// EntryType* consts are describing an entry_type, e.g., from Comment.
	EntryTypeUser            = 1
	EntryTypeDowntime        = 2
	EntryTypeFlapping        = 3
	EntryTypeAcknowledgement = 4

	// StateHost* consts are describing a host state, e.g., from CheckResult.
	StateHostUp   = 0
	StateHostDown = 1

	// StateService* consts are describing a service state, e.g., from CheckResult.
	StateServiceOk       = 0
	StateServiceWarning  = 1
	StateServiceCritical = 2
	StateServiceUnknown  = 3

	// StateType* consts are describing a state type, e.g., from HostServiceRuntimeAttributes.
	StateTypeSoft = 0
	StateTypeHard = 1
)

// Comment represents the Icinga 2 API Comment object.
//
// NOTE:
//   - An empty Service field indicates a host comment.
//   - The optional EntryType should be represented by one of the EntryType* consts.
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

// MarshalLogObject implements the zapcore.ObjectMarshaler interface.
func (cr *CheckResult) MarshalLogObject(encoder zapcore.ObjectEncoder) error {
	encoder.AddInt("exit_status", cr.ExitStatus)
	encoder.AddInt("state", cr.State)
	encoder.AddString("output", cr.Output)
	return nil
}

// Downtime represents the Icinga 2 API Downtime object.
//
// NOTE:
//   - An empty Service field indicates a host downtime.
//   - If a downtime was added by a ScheduledDowntime object, ConfigOwner is set to the name of that object and can
//     only be cancelled by its owner. Otherwise, it is empty and indicates user-created downtimes (via API or/and UI).
//
// https://icinga.com/docs/icinga-2/latest/doc/09-object-types/#objecttype-downtime
type Downtime struct {
	Host        string `json:"host_name"`
	Service     string `json:"service_name"`
	Author      string `json:"author"`
	Comment     string `json:"comment"`
	ConfigOwner string `json:"config_owner"`

	// RemoveTime is used to indicate whether a downtime was ended automatically or cancelled prematurely by a user.
	// It is set to zero time for the former case, otherwise to the timestamp at which time has been cancelled.
	RemoveTime UnixFloat `json:"remove_time"`

	// IsFixed is used to differentiate between fixed and flexible downtimes.
	// Fixed downtimes always emits a start and triggered event and cause two notifications being sent
	// for the very (same) event. Flexible downtimes, on the other hand, only emits a trigger event, and
	// don't produce duplicates for the same event.
	IsFixed bool `json:"fixed"`
}

// MarshalLogObject implements the zapcore.ObjectMarshaler interface.
func (d *Downtime) MarshalLogObject(encoder zapcore.ObjectEncoder) error {
	encoder.AddString("host", d.Host)
	if d.Service != "" {
		encoder.AddString("service", d.Service)
	}

	encoder.AddString("author", d.Author)
	encoder.AddBool("fixed", d.IsFixed)
	encoder.AddBool("was_cancelled", d.WasCancelled())
	encoder.AddTime("remove_time", d.RemoveTime.Time())
	encoder.AddString("comment", d.Comment)
	return nil
}

// WasCancelled returns true when the current downtime was cancelled prematurely by a user.
func (d *Downtime) WasCancelled() bool {
	return d.RemoveTime.Time().After(time.UnixMilli(0))
}

// HostServiceRuntimeAttributes are common attributes of both Host and Service objects.
//
// When catching up potentially missed changes, the following fields are holding relevant changes which, fortunately,
// are identical for Icinga 2 Host and Service objects.
//
// NOTE:
//   - Name is either the Host or the Service name.
//   - Host is empty for Host objects; Host contains the Service's Host object name for Services.
//   - State might be StateHost{Up,Down} for hosts or StateService{Ok,Warning,Critical,Unknown} for services.
//   - StateType might be StateTypeSoft or StateTypeHard.
//   - Acknowledgement type might be acknowledgement{None,Normal,Sticky}.
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
	IsFlapping                bool        `json:"flapping"`
	AcknowledgementLastChange UnixFloat   `json:"acknowledgement_last_change"`
	EnableFlapping            bool        `json:"enable_flapping"`
}

// MarshalLogObject implements the zapcore.ObjectMarshaler interface.
func (hsra *HostServiceRuntimeAttributes) MarshalLogObject(encoder zapcore.ObjectEncoder) error {
	encoder.AddString("name", hsra.Name)
	if hsra.Host != "" {
		encoder.AddString("host", hsra.Host)
	}

	encoder.AddInt("state", hsra.State)
	encoder.AddInt("state_type", hsra.StateType)
	encoder.AddBool("in_downtime", hsra.DowntimeDepth != 0)
	encoder.AddBool("acknowledged", hsra.Acknowledgement != AcknowledgementNone)
	encoder.AddTime("last_state_change", hsra.LastStateChange.Time())
	err := encoder.AddArray("groups", zapcore.ArrayMarshalerFunc(func(encoder zapcore.ArrayEncoder) error {
		for _, group := range hsra.Groups {
			encoder.AppendString(group)
		}
		return nil
	}))
	if err != nil {
		return err
	}

	return encoder.AddObject("check_result", &hsra.LastCheckResult)
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
	typeFlapping               = "Flapping"
	typeObjectCreated          = "ObjectCreated"
	typeObjectDeleted          = "ObjectDeleted"
)

// StateChange represents the Icinga 2 API Event Stream StateChange response for host/service state changes.
//
// NOTE:
//   - An empty Service field indicates a host state change.
//   - State might be StateHost{Up,Down} for hosts or StateService{Ok,Warning,Critical,Unknown} for services.
//   - StateType might be StateTypeSoft or StateTypeHard.
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

// MarshalLogObject implements the zapcore.ObjectMarshaler interface.
func (sc *StateChange) MarshalLogObject(encoder zapcore.ObjectEncoder) error {
	encoder.AddTime("timestamp", sc.Timestamp.Time())
	encoder.AddString("host", sc.Host)
	if sc.Service != "" {
		encoder.AddString("service", sc.Service)
	}

	encoder.AddInt("state", sc.State)
	encoder.AddInt("state_type", sc.StateType)
	encoder.AddBool("in_downtime", sc.DowntimeDepth != 0)
	encoder.AddBool("acknowledged", sc.Acknowledgement)
	return encoder.AddObject("check_result", &sc.CheckResult)
}

// Acknowledgement represents the Icinga 2 API Event Stream AcknowledgementSet or AcknowledgementCleared
// response for acknowledgements set/cleared on/from hosts/services.
//
// NOTE:
//   - An empty Service field indicates a host acknowledgement.
//   - State might be StateHost{Up,Down} for hosts or StateService{Ok,Warning,Critical,Unknown} for services.
//   - StateType might be StateTypeSoft or StateTypeHard.
//   - EventType is either set to typeAcknowledgementSet or typeAcknowledgementCleared.
//   - Author and Comment fields are always empty when EventType is set to typeAcknowledgementCleared.
//
// https://icinga.com/docs/icinga-2/latest/doc/12-icinga2-api/#event-stream-type-acknowledgementset
// https://icinga.com/docs/icinga-2/latest/doc/12-icinga2-api/#event-stream-type-acknowledgementcleared
type Acknowledgement struct {
	Timestamp UnixFloat `json:"timestamp"`
	Host      string    `json:"host"`
	Service   string    `json:"service"`
	State     int       `json:"state"`
	StateType int       `json:"state_type"`
	Author    string    `json:"author"`
	Comment   string    `json:"comment"`
	EventType string    `json:"type"`
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

// Flapping represents the Icinga 2 API Event Stream Flapping response for flapping host/services.
//
// NOTE:
//   - An empty Service field indicates a host being in flapping state.
//   - State includes the current state of the Checkable at the point in time at which it enters or exits the flapping state.
//   - CurrentFlapping indicates the current flapping value of a Checkable in percent.
//   - ThresholdLow is the low/min flapping threshold value set by the user (CurrentFlapping < ThresholdLow = flapping end).
//   - ThresholdHigh is the high/max flapping threshold value set by the user (CurrentFlapping > ThresholdHigh = flapping start).
//
// https://icinga.com/docs/icinga-2/latest/doc/12-icinga2-api/#event-stream-type-flapping
type Flapping struct {
	Timestamp       UnixFloat `json:"timestamp"`
	Host            string    `json:"host"`
	Service         string    `json:"service"`
	IsFlapping      bool      `json:"is_flapping"`
	State           int       `json:"state"`
	CurrentFlapping int       `json:"current_flapping"`
	ThresholdLow    int       `json:"threshold_low"`
	ThresholdHigh   int       `json:"threshold_high"`
}

// ObjectCreatedDeleted represents the Icinga 2 API stream object created/deleted response.
//
// NOTE:
//   - The ObjectName field already contains the composed name of the checkable if the ObjectType is `Service`.
//   - The EventType field indicates which event type is currently being streamed and is either
//     set to typeObjectCreated or typeObjectDeleted.
type ObjectCreatedDeleted struct {
	ObjectName string `json:"object_name"`
	ObjectType string `json:"object_type"`
	EventType  string `json:"type"`
}

// IcingaApplication represents the Icinga 2 API status endpoint query result of type IcingaApplication.
// https://icinga.com/docs/icinga-2/latest/doc/12-icinga2-api/#status-and-statistics
type IcingaApplication struct {
	App struct {
		EnableFlapping bool `json:"enable_flapping"`
	} `json:"app"`
}

// UnmarshalEventStreamResponse unmarshal a JSON response line from the Icinga 2 API Event Stream.
//
// The function expects an Icinga 2 API Event Stream Response in its JSON form and tries to unmarshal it into one of the
// implemented types based on its type argument. Thus, the returned any value will be a pointer to such a struct type.
func UnmarshalEventStreamResponse(bytes []byte) (any, error) {
	// Due to the overlapping fields of the different Event Stream response objects, a struct composition with
	// decompositions in different variables will result in multiple manual fixes. Thus, a two-way deserialization
	// was chosen which selects the target type based on the first parsed type field.

	var (
		responseType  string
		responseError int
	)
	err := json.Unmarshal(bytes, &struct {
		Type  *string `json:"type"`
		Error *int    `json:"error"`
	}{&responseType, &responseError})
	if err != nil {
		return nil, err
	}

	// Please note: An Event Stream Response SHOULD NOT contain an error field. However, it might be possible that a
	// message not produced by the Event Stream API might end up here, e.g., a generic API error message. There are
	// already checks for HTTP error codes in place, so this is more like a second layer of protection.
	if responseError > 0 {
		return nil, fmt.Errorf("error field is present, faulty message is %q", bytes)
	}

	var resp any
	switch responseType {
	case typeStateChange:
		resp = new(StateChange)
	case typeAcknowledgementSet, typeAcknowledgementCleared:
		resp = new(Acknowledgement)
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
	case typeFlapping:
		resp = new(Flapping)
	case typeObjectCreated, typeObjectDeleted:
		resp = new(ObjectCreatedDeleted)
	default:
		return nil, fmt.Errorf("unsupported type %q", responseType)
	}
	err = json.Unmarshal(bytes, resp)
	return resp, err
}
