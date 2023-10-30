package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/icinga/icinga-notifications/pkg/rpc"
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

type Info struct {
	Name             string          `json:"display_name"`
	ConfigAttributes json.RawMessage `json:"config_attrs"`
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
	GetInfo() *Info
	SetConfig(jsonStr json.RawMessage) error
	SendNotification(req *NotificationRequest) error
}

func RunPlugin(plugin Plugin) {
	encoder := json.NewEncoder(os.Stdout)
	decoder := json.NewDecoder(os.Stdin)
	var encoderMu sync.Mutex

	for {
		var req rpc.Request
		err := decoder.Decode(&req)
		if err != nil {
			if errors.Is(err, io.EOF) {
				// plugin shutdown requested
				return
			}

			log.Fatal("failed to read request:", err)
		}

		go func(request rpc.Request) {
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