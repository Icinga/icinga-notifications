package channel

import (
	"fmt"
	"github.com/icinga/icinga-notifications/internal/contracts"
	"github.com/icinga/icinga-notifications/internal/event"
	"io"
	"strings"
)

// FormatMessage formats a notification message and adds to the given io.Writer
func FormatMessage(writer io.Writer, incident contracts.Incident, event *event.Event, icingaweb2Url string) {
	_, _ = fmt.Fprintf(writer, "Info: %s\n\n", event.Message)
	_, _ = fmt.Fprintf(writer, "When: %s\n", event.Time.Format("2006-01-02 15:04:05 MST"))

	if event.Username != "" {
		_, _ = fmt.Fprintf(writer, "\nCommented by %s\n\n", event.Username)
	}

	_, _ = writer.Write([]byte(event.URL + "\n\n"))
	incidentUrl := icingaweb2Url
	if strings.HasSuffix(incidentUrl, "/") {
		incidentUrl = fmt.Sprintf("Incident: %snotifications/incident?id=%d\n", icingaweb2Url, incident.ID())
	} else {
		incidentUrl = fmt.Sprintf("Incident: %s/notifications/incident?id=%d\n", icingaweb2Url, incident.ID())
	}

	_, _ = writer.Write([]byte(incidentUrl))
}
