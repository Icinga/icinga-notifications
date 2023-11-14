package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/icinga/icinga-notifications/pkg/rpc"
	"github.com/icinga/icingadb/pkg/types"
	"io"
	"log"
	"os"
	"sync"
	"time"
)

const (
	MethodGetInfo          = "GetInfo"
	MethodSetConfig        = "SetConfig"
	MethodSendNotification = "SendNotification"
)

// ConfigOption describes a config element of the channel form
type ConfigOption struct {
	// Element name
	Name string `json:"name"`

	// Element type:
	//
	//  string = text, number = number, bool = checkbox, text = textarea, option = select, options = select[multiple], secret = password
	Type string `json:"type"`

	// Element label map. Locale in the standard format (language_REGION) as key and corresponding label as value.
	// Locale is assumed to be UTF-8 encoded (Without the suffix in the locale)
	//
	//  e.g. {"en_US": "Save", "de_DE": "Speichern"}
	//  An "en_US" locale must be given as a fallback
	Label map[string]string `json:"label"`

	// Element placeholder
	Placeholder string `json:"placeholder,omitempty"`

	// Element title: When the user moves the mouse pointer over an element, a tooltip is displayed with a given message.
	Help map[string]string `json:"help,omitempty"`

	// Element default value: bool for checkbox, string for other elements
	Default any `json:"default,omitempty"`

	// Set true if this element is required, omit otherwise
	Required bool `json:"required,omitempty"`

	// Options of a select element: key => value.
	// Only required for the type option or options
	//
	//  e.g., map[string]string{
	//			"1":   "January",
	//			"2":  "February",
	//		}
	Options map[string]string `json:"options,omitempty"`

	// Element's min option defines the minimum allowed number value. It can only be used for the type number.
	Min types.Int `json:"min,omitempty"`

	// Element's max option defines the maximum allowed number value. It can only be used for the type number.
	Max types.Int `json:"max,omitempty"`
}

// Info contains plugin information.
type Info struct {
	Type             string          `db:"type" json:"-"`
	Name             string          `db:"name" json:"name"`
	Version          string          `db:"version" json:"version"`
	Author           string          `db:"author" json:"author"`
	ConfigAttributes json.RawMessage `db:"config_attrs" json:"config_attrs"` // ConfigOption(s) as json-encoded list
}

// TableName implements the contracts.TableNamer interface.
func (i *Info) TableName() string {
	return "available_channel_type"
}

type Contact struct {
	FullName  string     `json:"full_name"`
	Addresses []*Address `json:"addresses"`
}

type Address struct {
	Type    string `json:"type"`
	Address string `json:"address"`
}

type Object struct {
	Name      string            `json:"name"`
	Url       string            `json:"url"`
	Tags      map[string]string `json:"tags"`
	ExtraTags map[string]string `json:"extra_tags"`
}

type Incident struct {
	Id  int64  `json:"id"`
	Url string `json:"url"`
}

type Event struct {
	Time     time.Time `json:"time"`
	Type     string    `json:"type"`
	Severity string    `json:"severity"`
	Username string    `json:"username"`
	Message  string    `json:"message"`
}

type NotificationRequest struct {
	Contact  *Contact  `json:"contact"`
	Object   *Object   `json:"object"`
	Incident *Incident `json:"incident"`
	Event    *Event    `json:"event"`
}

type Plugin interface {
	// GetInfo returns the corresponding plugin *Info
	GetInfo() *Info

	// SetConfig sets the plugin config, returns an error on failure
	SetConfig(jsonStr json.RawMessage) error

	// SendNotification sends the notification, returns an error on failure
	SendNotification(req *NotificationRequest) error
}

// RunPlugin reads the incoming stdin requests, processes and writes the responses to stdout
func RunPlugin(plugin Plugin) {
	encoder := json.NewEncoder(os.Stdout)
	decoder := json.NewDecoder(os.Stdin)
	var encoderMu sync.Mutex

	wg := sync.WaitGroup{}

	for {
		var req rpc.Request
		err := decoder.Decode(&req)
		if err != nil {
			if errors.Is(err, io.EOF) {
				// plugin shutdown requested
				break
			}

			log.Fatal("failed to read request:", err)
		}

		wg.Add(1)
		go func(request rpc.Request) {
			defer wg.Done()
			var response = rpc.Response{Id: request.Id}
			switch request.Method {
			case MethodGetInfo:
				result, err := json.Marshal(plugin.GetInfo())
				if err != nil {
					response.Error = fmt.Errorf("failed to collect plugin info: %w", err).Error()
				} else {
					response.Result = result
				}

			case MethodSetConfig:
				if err = plugin.SetConfig(request.Params); err != nil {
					response.Error = fmt.Errorf("failed to set plugin config: %w", err).Error()
				}

			case MethodSendNotification:
				var nr NotificationRequest
				if err = json.Unmarshal(request.Params, &nr); err != nil {
					response.Error = fmt.Errorf("failed to json.Unmarshal request: %w", err).Error()
				} else if err = plugin.SendNotification(&nr); err != nil {
					response.Error = err.Error()
				}

			default:
				response.Error = fmt.Sprintf("unknown method: %q", request.Method)
			}

			encoderMu.Lock()
			err = encoder.Encode(response)
			encoderMu.Unlock()
			if err != nil {
				panic(fmt.Errorf("failed to write response: %w", err))
			}
		}(req)
	}

	wg.Wait()
}

// FormatMessage formats a notification message and adds to the given io.Writer
func FormatMessage(writer io.Writer, req *NotificationRequest) {
	_, _ = fmt.Fprintf(writer, "Info: %s\n\n", req.Event.Message)
	_, _ = fmt.Fprintf(writer, "When: %s\n\n", req.Event.Time.Format("2006-01-02 15:04:05 MST"))

	if req.Event.Username != "" {
		_, _ = fmt.Fprintf(writer, "Commented by %s\n\n", req.Event.Username)
	}
	_, _ = fmt.Fprintf(writer, "Object: %s\n\n", req.Object.Url)
	_, _ = writer.Write([]byte("Tags:\n"))
	for k, v := range req.Object.Tags {
		_, _ = fmt.Fprintf(writer, "%s: %s\n", k, v)
	}

	if len(req.Object.ExtraTags) > 0 {
		_, _ = writer.Write([]byte("\nExtra Tags:\n"))
		for k, v := range req.Object.ExtraTags {
			_, _ = fmt.Fprintf(writer, "%s: %s\n", k, v)
		}
	}

	_, _ = fmt.Fprintf(writer, "\nIncident: %s", req.Incident.Url)
}
