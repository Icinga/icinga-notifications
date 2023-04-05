package channel

import (
	"fmt"
	"github.com/icinga/noma/internal/event"
	"github.com/icinga/noma/internal/incident"
	"io"
)

// FormatMessage formats a notification message and adds to the given io.Writer
func FormatMessage(writer io.Writer, incident *incident.Incident, event *event.Event) {
	_, _ = fmt.Fprintf(writer, "%s is %s\n\n", incident.Object.DisplayName(), event.Severity.String())
	_, _ = fmt.Fprintf(writer, "Info: %s\n\n", event.Message)
	_, _ = fmt.Fprintf(writer, "When: %s\n", event.Time.Format("2006-01-02 15:04:05 MST"))

	if event.Username != "" {
		_, _ = fmt.Fprintf(writer, "\nCommented by %s\n\n", event.Username)
	}

	_, _ = writer.Write([]byte(event.URL))
}
