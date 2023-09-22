package plugin

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"time"
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

type Incident struct {
	Id                int64  `json:"id"`
	ObjectDisplayName string `json:"object_display_name"`
}

type Event struct {
	Time     time.Time `json:"time"`
	URL      string    `json:"url"`
	Type     string    `json:"type"`
	Severity string    `json:"severity"`
	Username string    `json:"username"`
	Message  string    `json:"message"`
}

type NotificationRequest struct {
	Contact       *Contact `json:"contact"`
	Incident      Incident `json:"incident"`
	Event         *Event   `json:"event"`
	IcingaWeb2Url string   `json:"icingaweb2_url"`
}

type JsonRpcRequest struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Id     uint64          `json:"id"`
}

type JsonRpcResponse struct {
	Result json.RawMessage `json:"result"`
	Error  string          `json:"error"`
	Id     uint64          `json:"id"`
}

type Plugin interface {
	Send(req *NotificationRequest) error
	LoadConfig(jsonStr string) error
	GetInfo() *Info
}

func RunPlugin(plugin Plugin) {
	encoder := json.NewEncoder(os.Stdout)
	decoder := json.NewDecoder(os.Stdin)
	var encoderMu sync.Mutex

	for {
		var req JsonRpcRequest
		err := decoder.Decode(&req)
		if err != nil {
			log.Fatal("Failed to json.Decode request:", err)
		}

		go func(request JsonRpcRequest) {
			var response = JsonRpcResponse{Id: request.Id}
			switch request.Method {
			case "GetInfo":
				result, err := json.Marshal(plugin.GetInfo())
				if err != nil {
					response.Error = fmt.Errorf("failed to collect plugin info: %w", err).Error()
				} else {
					response.Result = result
				}

			case "SetConfig":
				if err = plugin.LoadConfig(string(request.Params)); err != nil {
					response.Error = fmt.Errorf("failed to set plugin config: %w", err).Error()
				}

			case "SendNotification":
				var nr NotificationRequest
				if err = json.Unmarshal(request.Params, &nr); err != nil {
					response.Error = fmt.Errorf("failed to json.Unmarshal request: %w", err).Error()
				} else if err = plugin.Send(&nr); err != nil {
					response.Error = err.Error()
				}

			default:
				response.Error = "unknown json-rpc method given"
			}

			encoderMu.Lock()
			err = encoder.Encode(response)
			encoderMu.Unlock()
			if err != nil {
				panic(fmt.Errorf("failed to write json response: %w", err))
			}
		}(req)
	}
}

// FormatMessage formats a notification message and adds to the given io.Writer
func FormatMessage(writer io.Writer, req *NotificationRequest) {
	_, _ = fmt.Fprintf(writer, "Info: %s\n\n", req.Event.Message)
	_, _ = fmt.Fprintf(writer, "When: %s\n", req.Event.Time.Format("2006-01-02 15:04:05 MST"))

	if req.Event.Username != "" {
		_, _ = fmt.Fprintf(writer, "\nCommented by %s\n\n", req.Event.Username)
	}

	_, _ = writer.Write([]byte(req.Event.URL + "\n\n"))
	incidentUrl := req.IcingaWeb2Url
	if strings.HasSuffix(incidentUrl, "/") {
		incidentUrl = fmt.Sprintf("Incident: %snotifications/incident?id=%d\n", req.IcingaWeb2Url, req.Incident.Id)
	} else {
		incidentUrl = fmt.Sprintf("Incident: %s/notifications/incident?id=%d\n", req.IcingaWeb2Url, req.Incident.Id)
	}

	_, _ = writer.Write([]byte(incidentUrl))
}
