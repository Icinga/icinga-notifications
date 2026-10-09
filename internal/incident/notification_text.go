package incident

import (
	"fmt"
	"time"

	baseEv "github.com/icinga/icinga-go-library/notifications/event"
)

// renderCtx carries the incident/object details needed to render a default notification summary and body.
type renderCtx struct {
	ObjectName string
	ObjectURL  string

	Severity        baseEv.Severity
	IncidentMessage string
	IncidentSummary string

	SourceName string

	StartedAt   time.Time
	RecoveredAt time.Time // zero if the incident is still open
	Age         time.Duration

	ManagerName string // "" if the incident is currently unmanaged
	MuteReason  string
}

const timeLayout = "2006-01-02 15:04:05 -0700"

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(timeLayout)
}

func formatDuration(d time.Duration) string {
	return d.Round(time.Second).String()
}

func managerOrUnassigned(name string) string {
	if name == "" {
		return "Unassigned"
	}
	return name
}

// severityDisplay renders a Severity as expected by the default template.
func severityDisplay(s baseEv.Severity) string {
	switch s {
	case baseEv.SeverityOK:
		return "OK"
	case baseEv.SeverityDebug:
		return "Debug"
	case baseEv.SeverityInfo:
		return "Info"
	case baseEv.SeverityNotice:
		return "Notice"
	case baseEv.SeverityWarning:
		return "Warning"
	case baseEv.SeverityErr:
		return "Error"
	case baseEv.SeverityCrit:
		return "Critical"
	case baseEv.SeverityAlert:
		return "Alert"
	case baseEv.SeverityEmerg:
		return "Emergency"
	default:
		return s.String()
	}
}

// renderNotification builds the default one-line summary and multi-line body for a notification.
//
// The text is based on <https://github.com/Icinga/icinga-notifications/issues/536>.
func renderNotification(r notifyReason, ctx renderCtx) (summary, body string) {
	if ctx.IncidentSummary == "" {
		ctx.IncidentSummary = ctx.IncidentMessage
	}

	switch r.Kind {
	case ReasonOpened:
		summary = fmt.Sprintf("[%s] Incident opened: %s", ctx.ObjectName, ctx.IncidentSummary)

		switch {
		case r.Closing:
			body = fmt.Sprintf(
				"A new incident has been opened by %s for %s.\n\n"+
					"This will remain the only notification as the incident has already been closed.\n\n"+
					"Severity:    %s\n"+
					"Opened:      %s\n"+
					"Details:     %s\n\n"+
					"%s",
				ctx.SourceName, ctx.ObjectName, severityDisplay(ctx.Severity), formatTime(ctx.StartedAt), ctx.ObjectURL, ctx.IncidentMessage)

		case r.CurrentlyMuted:
			body = fmt.Sprintf(
				"A new incident has been opened by %s for %s.\n\n"+
					"The incident is muted. You will not receive further notifications until it is unmuted.\n\n"+
					"Mute Reason: %s\n"+
					"Severity:    %s\n"+
					"Opened:      %s\n"+
					"Details:     %s\n\n"+
					"%s",
				ctx.SourceName, ctx.ObjectName, ctx.MuteReason, severityDisplay(ctx.Severity), formatTime(ctx.StartedAt), ctx.ObjectURL, ctx.IncidentMessage)

		default:
			body = fmt.Sprintf(
				"A new incident has been opened by %s for %s.\n\n"+
					"Severity: %s\n"+
					"Opened:   %s\n"+
					"Details:  %s\n\n"+
					"%s",
				ctx.SourceName, ctx.ObjectName, severityDisplay(ctx.Severity), formatTime(ctx.StartedAt), ctx.ObjectURL, ctx.IncidentMessage)
		}

	case ReasonEscalated:
		summary = fmt.Sprintf("[%s] Incident escalated: %s", ctx.ObjectName, ctx.IncidentSummary)
		responsible := managerOrUnassigned(ctx.ManagerName)

		switch {
		case r.Closing:
			body = fmt.Sprintf(
				"An incident opened by %s for %s was escalated to you.\n\n"+
					"This will remain the only notification as the incident has already been closed.\n\n"+
					"Severity:    %s (was %s)\n"+
					"Opened:      %s\n"+
					"Responsible: %s\n"+
					"Details:     %s\n\n"+
					"%s",
				ctx.SourceName, ctx.ObjectName, severityDisplay(ctx.Severity), severityDisplay(r.OldSeverity),
				formatTime(ctx.StartedAt), responsible, ctx.ObjectURL, ctx.IncidentMessage)

		case r.CurrentlyMuted && (r.SeverityImproved || r.SeverityWorsened):
			body = fmt.Sprintf(
				"An incident opened by %s for %s was escalated to you.\n\n"+
					"The incident is muted. You will not receive further notifications until it is unmuted.\n\n"+
					"Mute Reason: %s\n"+
					"Severity:    %s (was %s)\n"+
					"Opened:      %s\n"+
					"Responsible: %s\n"+
					"Details:     %s\n\n"+
					"%s",
				ctx.SourceName, ctx.ObjectName, ctx.MuteReason, severityDisplay(ctx.Severity), severityDisplay(r.OldSeverity),
				formatTime(ctx.StartedAt), responsible, ctx.ObjectURL, ctx.IncidentMessage)

		case r.CurrentlyMuted:
			body = fmt.Sprintf(
				"An incident opened by %s for %s was escalated to you.\n\n"+
					"The incident is muted. You will not receive further notifications until it is unmuted.\n\n"+
					"Mute Reason: %s\n"+
					"Severity:    %s\n"+
					"Opened:      %s\n"+
					"Responsible: %s\n"+
					"Details:     %s\n\n"+
					"The incident has been open for %s. The last message received:\n\n"+
					"%s",
				ctx.SourceName, ctx.ObjectName, ctx.MuteReason, severityDisplay(ctx.Severity),
				formatTime(ctx.StartedAt), responsible, ctx.ObjectURL, formatDuration(ctx.Age), ctx.IncidentMessage)

		case r.JustUnmuted:
			body = fmt.Sprintf(
				"An incident opened by %s for %s was escalated to you.\n\n"+
					"The incident was muted until now. You will receive further notifications from now on.\n\n"+
					"Unmute Reason: %s\n"+
					"Severity:      %s (was %s)\n"+
					"Opened:        %s\n"+
					"Responsible:   %s\n"+
					"Details:       %s\n\n"+
					"%s",
				ctx.SourceName, ctx.ObjectName, ctx.MuteReason, severityDisplay(ctx.Severity), severityDisplay(r.OldSeverity),
				formatTime(ctx.StartedAt), responsible, ctx.ObjectURL, ctx.IncidentMessage)

		case r.SeverityImproved, r.SeverityWorsened:
			body = fmt.Sprintf(
				"An incident opened by %s for %s was escalated to you.\n\n"+
					"Severity:    %s (was %s)\n"+
					"Opened:      %s\n"+
					"Responsible: %s\n"+
					"Details:     %s\n\n"+
					"%s",
				ctx.SourceName, ctx.ObjectName, severityDisplay(ctx.Severity), severityDisplay(r.OldSeverity),
				formatTime(ctx.StartedAt), responsible, ctx.ObjectURL, ctx.IncidentMessage)

		default:
			body = fmt.Sprintf(
				"An incident opened by %s for %s was escalated to you.\n\n"+
					"Severity:    %s\n"+
					"Opened:      %s\n"+
					"Responsible: %s\n"+
					"Details:     %s\n\n"+
					"The incident has been open for %s. The last message received:\n\n"+
					"%s",
				ctx.SourceName, ctx.ObjectName, severityDisplay(ctx.Severity),
				formatTime(ctx.StartedAt), responsible, ctx.ObjectURL, formatDuration(ctx.Age), ctx.IncidentMessage)
		}

	case ReasonUpdated:
		responsible := managerOrUnassigned(ctx.ManagerName)

		switch {
		case r.SeverityImproved:
			summary = fmt.Sprintf("[%s] Incident improved: %s", ctx.ObjectName, ctx.IncidentSummary)
			body = fmt.Sprintf(
				"An incident opened by %s for %s has improved.\n\n"+
					"Severity:    %s (was %s)\n"+
					"Opened:      %s\n"+
					"Responsible: %s\n"+
					"Details:     %s\n\n"+
					"%s",
				ctx.SourceName, ctx.ObjectName, severityDisplay(ctx.Severity), severityDisplay(r.OldSeverity),
				formatTime(ctx.StartedAt), responsible, ctx.ObjectURL, ctx.IncidentMessage)

		case r.SeverityWorsened:
			summary = fmt.Sprintf("[%s] Incident worsened: %s", ctx.ObjectName, ctx.IncidentSummary)
			body = fmt.Sprintf(
				"An incident opened by %s for %s has worsened.\n\n"+
					"Severity:    %s (was %s)\n"+
					"Opened:      %s\n"+
					"Responsible: %s\n"+
					"Details:     %s\n\n"+
					"%s",
				ctx.SourceName, ctx.ObjectName, severityDisplay(ctx.Severity), severityDisplay(r.OldSeverity),
				formatTime(ctx.StartedAt), responsible, ctx.ObjectURL, ctx.IncidentMessage)

		default:
			summary = fmt.Sprintf("[%s] Incident updated: %s", ctx.ObjectName, ctx.IncidentSummary)
			body = fmt.Sprintf(
				"An incident opened by %s for %s has been updated.\n\n"+
					"Severity:    %s\n"+
					"Opened:      %s\n"+
					"Responsible: %s\n"+
					"Details:     %s\n\n"+
					"%s",
				ctx.SourceName, ctx.ObjectName, severityDisplay(ctx.Severity),
				formatTime(ctx.StartedAt), responsible, ctx.ObjectURL, ctx.IncidentMessage)
		}

	case ReasonResolved:
		summary = fmt.Sprintf("[%s][%s] Incident resolved: %s", ctx.SourceName, ctx.ObjectName, ctx.IncidentSummary)
		body = fmt.Sprintf(
			"An incident opened by %s for %s has been resolved.\n\n"+
				"Severity:    %s\n"+
				"Duration:    %s (%s - %s)\n"+
				"Responsible: %s\n"+
				"Details:     %s\n\n"+
				"%s",
			ctx.SourceName, ctx.ObjectName, severityDisplay(ctx.Severity),
			formatDuration(ctx.RecoveredAt.Sub(ctx.StartedAt)), formatTime(ctx.StartedAt), formatTime(ctx.RecoveredAt),
			managerOrUnassigned(ctx.ManagerName), ctx.ObjectURL, ctx.IncidentMessage)

	case ReasonMuted:
		summary = fmt.Sprintf("[%s] Incident muted: %s", ctx.ObjectName, ctx.IncidentSummary)
		body = fmt.Sprintf(
			"An incident opened by %s for %s has been muted.\n\n"+
				"Mute Reason: %s\n"+
				"Severity:    %s\n"+
				"Opened:      %s\n"+
				"Details:     %s\n\n"+
				"%s",
			ctx.SourceName, ctx.ObjectName, ctx.MuteReason, severityDisplay(ctx.Severity),
			formatTime(ctx.StartedAt), ctx.ObjectURL, ctx.IncidentMessage)

	case ReasonUnmuted:
		summary = fmt.Sprintf("[%s] Incident unmuted: %s", ctx.ObjectName, ctx.IncidentSummary)
		body = fmt.Sprintf(
			"An incident opened by %s for %s has been unmuted.\n\n"+
				"Unmute Reason: %s\n"+
				"Severity:      %s\n"+
				"Opened:        %s\n"+
				"Details:       %s\n\n"+
				"%s",
			ctx.SourceName, ctx.ObjectName, ctx.MuteReason, severityDisplay(ctx.Severity),
			formatTime(ctx.StartedAt), ctx.ObjectURL, ctx.IncidentMessage)

	case ReasonManaged:
		summary = fmt.Sprintf("[%s][%s] Incident assigned: %s", ctx.SourceName, ctx.ObjectName, ctx.IncidentSummary)
		body = fmt.Sprintf(
			"An incident opened by %s for %s has been assigned to %s.\n\n"+
				"Severity:    %s\n"+
				"Opened:      %s\n"+
				"Responsible: %s\n"+
				"Details:     %s\n\n"+
				"%s",
			ctx.SourceName, ctx.ObjectName, r.ManagerName, severityDisplay(ctx.Severity),
			formatTime(ctx.StartedAt), r.ManagerName, ctx.ObjectURL, ctx.IncidentMessage)

	case ReasonUnmanaged:
		summary = fmt.Sprintf("[%s] Incident unassigned: %s", ctx.ObjectName, ctx.IncidentSummary)
		body = fmt.Sprintf(
			"An incident opened by %s for %s has been unassigned from %s.\n\n"+
				"Severity:    %s\n"+
				"Opened:      %s\n"+
				"Responsible: Unassigned\n"+
				"Details:     %s\n\n"+
				"%s",
			ctx.SourceName, ctx.ObjectName, r.ManagerName, severityDisplay(ctx.Severity),
			formatTime(ctx.StartedAt), ctx.ObjectURL, ctx.IncidentMessage)
	}

	return summary, body
}
