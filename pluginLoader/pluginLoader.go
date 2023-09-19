package pluginLoader

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

type Response struct {
	Success bool   `json:"success"`
	Error   string `json:"error"`
}

type PluginLoader interface {
	Send(req *NotificationRequest) error
	LoadConfig(jsonStr string)
}

func RunPlugin(plugin PluginLoader) {
	reader := bufio.NewReader(os.Stdin)

	configStr, err := reader.ReadString('\n')
	if err != nil {
		log.Fatal("Failed to read config:", err)
	}

	plugin.LoadConfig(configStr)

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			log.Fatal("Failed to read request:", err)
		}

		var req NotificationRequest
		if err = json.Unmarshal([]byte(line), &req); err != nil {
			log.Fatal("Failed to json.Unmarshal request:", err)
		}

		var response = Response{Success: true, Error: ""}
		if err = plugin.Send(&req); err != nil {
			response.Success = false
			response.Error = err.Error()
		}

		marshal, err := json.Marshal(response)
		if err != nil {
			log.Fatal("Failed to prepare json response:", err)
		}
		_, err = fmt.Fprintln(os.Stdout, string(marshal))
		if err != nil {
			panic(err)
		}
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
