package plugin

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
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
	ObjectDisplayName string `json:"objectDisplayName"`
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
	IcingaWeb2Url string   `json:"icingaWeb2Url"`
}

type JsonRpcRequest struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Id     string          `json:"id"`
}

type JsonRpcResponse struct {
	Result json.RawMessage `json:"result"`
	Error  string          `json:"error"`
	Id     string          `json:"id"`
}

type Plugin interface {
	Send(req *NotificationRequest) error
	LoadConfig(jsonStr string) error
	GetInfo() *Info
}

func RunPlugin(plugin Plugin) {
	reader := bufio.NewReader(os.Stdin)

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			log.Fatal("Failed to read request:", err)
		}

		go func(req string) {
			request := JsonRpcRequest{}
			if err = json.Unmarshal([]byte(req), &request); err != nil {
				log.Fatal("Failed to json.Unmarshal request:", err)
			}
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

			marshal, err := json.Marshal(response)
			if err != nil {
				panic(fmt.Errorf("failed to prepare json response: %w", err))
			}

			if _, err = fmt.Fprintln(os.Stdout, string(marshal)); err != nil {
				panic(fmt.Errorf("failed to write json response: %w", err))
			}
		}(line)
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
