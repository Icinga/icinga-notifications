package incident

import (
	"fmt"
	"slices"
	"strings"
	"time"

	baseEv "github.com/icinga/icinga-go-library/notifications/event"
	"github.com/icinga/icinga-go-library/notifications/plugin"
	"github.com/icinga/icinga-go-library/utils"
)

// renderCtx carries the incident/object details needed to render a default notification summary and body.
type renderCtx struct {
	ObjectName string
	ObjectURL  string

	Severity        baseEv.Severity
	IncidentMessage string
	IncidentSummary string

	SourceName string

	// EventSummary and EventMessage describe the current event; only used for ReasonEvent and ReasonIncidentEvent.
	EventSummary string
	EventMessage string

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

// detailsLine renders the "Details:" line with its label padded to width, or "" if url is empty.
func detailsLine(url string, width int) string {
	if url == "" {
		return ""
	}
	return fmt.Sprintf("%-*s%s\n", width, "Details:", url)
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
	case baseEv.SeverityWarning:
		return "Warning"
	case baseEv.SeverityError:
		return "Error"
	case baseEv.SeverityCritical:
		return "Critical"
	default:
		return s.String()
	}
}

// severityChangeDisplay renders the current severity, followed by the old one if the severity changed in this batch.
func severityChangeDisplay(s baseEv.Severity, r *notifyReason) string {
	if !r.SeverityImproved && !r.SeverityWorsened {
		return severityDisplay(s)
	}
	return fmt.Sprintf("%s (was %s)", severityDisplay(s), severityDisplay(r.OldSeverity))
}

// renderNotification builds the default one-line summary and multi-line body for a notification.
//
// The text is based on <https://github.com/Icinga/icinga-notifications/issues/536>.
func renderNotification(r *notifyReason, ctx *renderCtx) (summary, body string, details []plugin.DeliveryDetail) {
	if ctx.IncidentSummary == "" {
		summary, _, _ := strings.Cut(strings.TrimLeft(ctx.IncidentMessage, "\n\t "), "\n")
		ctx.IncidentSummary = utils.EllipsizeRunes(summary, baseEv.MaxSummaryLen)
	}
	if ctx.EventSummary == "" {
		summary, _, _ := strings.Cut(strings.TrimLeft(ctx.EventMessage, "\n\t "), "\n")
		ctx.EventSummary = utils.EllipsizeRunes(summary, baseEv.MaxSummaryLen)
	}

	switch r.Kind {
	case ReasonOpened:
		summary = fmt.Sprintf("[%s] Incident opened: %s", ctx.ObjectName, ctx.IncidentSummary)
		if !r.Closing {
			addSeverityDetails(ctx, &details)
		}

		switch {
		case r.Closing:
			body = fmt.Sprintf(
				"A new incident has been opened by %s for %s.\n\n"+
					"This will remain the only notification as the incident has already been closed.\n\n"+
					"Severity:    %s\n"+
					"Opened:      %s\n"+
					"%s\n"+
					"%s",
				ctx.SourceName, ctx.ObjectName,
				severityDisplay(ctx.Severity),
				formatTime(ctx.StartedAt),
				detailsLine(ctx.ObjectURL, 13),
				ctx.IncidentMessage)

		case r.CurrentlyMuted:
			addMutingDetails(r, ctx, true, &details)
			body = fmt.Sprintf(
				"A new incident has been opened by %s for %s.\n\n"+
					"The incident is muted. You will not receive further notifications until it is unmuted.\n\n"+
					"Mute Reason: %s\n"+
					"Severity:    %s\n"+
					"Opened:      %s\n"+
					"%s\n"+
					"%s",
				ctx.SourceName, ctx.ObjectName,
				ctx.MuteReason,
				severityDisplay(ctx.Severity),
				formatTime(ctx.StartedAt),
				detailsLine(ctx.ObjectURL, 13),
				ctx.IncidentMessage)

		default:
			body = fmt.Sprintf(
				"A new incident has been opened by %s for %s.\n\n"+
					"Severity: %s\n"+
					"Opened:   %s\n"+
					"%s\n"+
					"%s",
				ctx.SourceName, ctx.ObjectName,
				severityDisplay(ctx.Severity),
				formatTime(ctx.StartedAt),
				detailsLine(ctx.ObjectURL, 10),
				ctx.IncidentMessage)
		}

	case ReasonEscalated:
		summary = fmt.Sprintf("[%s] Incident escalated: %s", ctx.ObjectName, ctx.IncidentSummary)
		responsible := managerOrUnassigned(ctx.ManagerName)

		switch {
		case r.Closing:
			addResponsibleDetails(r, ctx, &details)
			body = fmt.Sprintf(
				"An incident opened by %s for %s was escalated to you.\n\n"+
					"This will remain the only notification as the incident has already been closed.\n\n"+
					"Severity:    %s\n"+
					"Opened:      %s\n"+
					"Responsible: %s\n"+
					"%s\n"+
					"%s",
				ctx.SourceName, ctx.ObjectName,
				severityChangeDisplay(ctx.Severity, r),
				formatTime(ctx.StartedAt),
				responsible,
				detailsLine(ctx.ObjectURL, 13),
				ctx.IncidentMessage)

		case r.CurrentlyMuted && (r.SeverityImproved || r.SeverityWorsened):
			addMutingDetails(r, ctx, true, &details)
			addSeverityChangeDetails(r, ctx, &details)
			addResponsibleDetails(r, ctx, &details)

			body = fmt.Sprintf(
				"An incident opened by %s for %s was escalated to you.\n\n"+
					"The incident is muted. You will not receive further notifications until it is unmuted.\n\n"+
					"Mute Reason: %s\n"+
					"Severity:    %s (was %s)\n"+
					"Opened:      %s\n"+
					"Responsible: %s\n"+
					"%s\n"+
					"%s",
				ctx.SourceName, ctx.ObjectName,
				ctx.MuteReason,
				severityDisplay(ctx.Severity), severityDisplay(r.OldSeverity),
				formatTime(ctx.StartedAt),
				responsible,
				detailsLine(ctx.ObjectURL, 13),
				ctx.IncidentMessage)

		case r.CurrentlyMuted:
			addMutingDetails(r, ctx, true, &details)
			addSeverityDetails(ctx, &details)
			addResponsibleDetails(r, ctx, &details)

			body = fmt.Sprintf(
				"An incident opened by %s for %s was escalated to you.\n\n"+
					"The incident is muted. You will not receive further notifications until it is unmuted.\n\n"+
					"Mute Reason: %s\n"+
					"Severity:    %s\n"+
					"Opened:      %s\n"+
					"Responsible: %s\n"+
					"%s\n"+
					"The incident has been open for %s. The last message received:\n\n"+
					"%s",
				ctx.SourceName, ctx.ObjectName,
				ctx.MuteReason,
				severityDisplay(ctx.Severity),
				formatTime(ctx.StartedAt),
				responsible,
				detailsLine(ctx.ObjectURL, 13),
				formatDuration(ctx.Age),
				ctx.IncidentMessage)

		case r.JustUnmuted:
			addMutingDetails(r, ctx, false, &details)
			addSeverityDetails(ctx, &details)
			addResponsibleDetails(r, ctx, &details)

			body = fmt.Sprintf(
				"An incident opened by %s for %s was escalated to you.\n\n"+
					"Severity:      %s\n"+
					"Opened:        %s\n"+
					"Responsible:   %s\n"+
					"%s\n"+
					"%s",
				ctx.SourceName, ctx.ObjectName,
				severityChangeDisplay(ctx.Severity, r),
				formatTime(ctx.StartedAt),
				responsible,
				detailsLine(ctx.ObjectURL, 15),
				ctx.IncidentMessage)

		case r.SeverityImproved, r.SeverityWorsened:
			addSeverityChangeDetails(r, ctx, &details)
			addResponsibleDetails(r, ctx, &details)

			body = fmt.Sprintf(
				"An incident opened by %s for %s was escalated to you.\n\n"+
					"Severity:    %s (was %s)\n"+
					"Opened:      %s\n"+
					"Responsible: %s\n"+
					"%s\n"+
					"%s",
				ctx.SourceName, ctx.ObjectName,
				severityDisplay(ctx.Severity), severityDisplay(r.OldSeverity),
				formatTime(ctx.StartedAt),
				responsible,
				detailsLine(ctx.ObjectURL, 13),
				ctx.IncidentMessage)

		default:
			addSeverityDetails(ctx, &details)
			addResponsibleDetails(r, ctx, &details)
			details = append(details, plugin.DeliveryDetail{
				Type:  "text",
				Label: "Incident Age",
				Value: formatDuration(ctx.Age),
			})

			body = fmt.Sprintf(
				"An incident opened by %s for %s was escalated to you.\n\n"+
					"Severity:    %s\n"+
					"Opened:      %s\n"+
					"Responsible: %s\n"+
					"%s\n"+
					"The incident has been open for %s. The last message received:\n\n"+
					"%s",
				ctx.SourceName, ctx.ObjectName,
				severityDisplay(ctx.Severity),
				formatTime(ctx.StartedAt),
				responsible,
				detailsLine(ctx.ObjectURL, 13),
				formatDuration(ctx.Age),
				ctx.IncidentMessage)
		}

	case ReasonUpdated:
		responsible := managerOrUnassigned(ctx.ManagerName)

		var unmuted string
		if r.JustUnmuted {
			addMutingDetails(r, ctx, false, &details)
			unmuted = fmt.Sprintf(
				"The incident was muted until now. You will receive further notifications from now on.\n\n"+
					"Unmute Reason: %s\n",
				r.UnmuteReason)
		}

		switch {
		case r.SeverityImproved:
			addSeverityChangeDetails(r, ctx, &details)
			addResponsibleDetails(r, ctx, &details)

			summary = fmt.Sprintf("[%s] Incident improved: %s", ctx.ObjectName, ctx.IncidentSummary)
			body = fmt.Sprintf(
				"An incident opened by %s for %s has improved.\n\n"+
					"%s"+
					"Severity:    %s (was %s)\n"+
					"Opened:      %s\n"+
					"Responsible: %s\n"+
					"%s\n"+
					"%s",
				ctx.SourceName, ctx.ObjectName,
				unmuted,
				severityDisplay(ctx.Severity), severityDisplay(r.OldSeverity),
				formatTime(ctx.StartedAt),
				responsible,
				detailsLine(ctx.ObjectURL, 13),
				ctx.IncidentMessage)

		case r.SeverityWorsened:
			addSeverityChangeDetails(r, ctx, &details)
			addResponsibleDetails(r, ctx, &details)

			summary = fmt.Sprintf("[%s] Incident worsened: %s", ctx.ObjectName, ctx.IncidentSummary)
			body = fmt.Sprintf(
				"An incident opened by %s for %s has worsened.\n\n"+
					"%s"+
					"Severity:    %s (was %s)\n"+
					"Opened:      %s\n"+
					"Responsible: %s\n"+
					"%s\n"+
					"%s",
				ctx.SourceName, ctx.ObjectName,
				unmuted,
				severityDisplay(ctx.Severity), severityDisplay(r.OldSeverity),
				formatTime(ctx.StartedAt),
				responsible,
				detailsLine(ctx.ObjectURL, 13),
				ctx.IncidentMessage)

		default:
			addSeverityDetails(ctx, &details)
			addResponsibleDetails(r, ctx, &details)

			summary = fmt.Sprintf("[%s] Incident updated: %s", ctx.ObjectName, ctx.IncidentSummary)
			body = fmt.Sprintf(
				"An incident opened by %s for %s has been updated.\n\n"+
					"Severity:    %s\n"+
					"Opened:      %s\n"+
					"Responsible: %s\n"+
					"%s\n"+
					"%s",
				ctx.SourceName, ctx.ObjectName,
				severityDisplay(ctx.Severity),
				formatTime(ctx.StartedAt),
				responsible,
				detailsLine(ctx.ObjectURL, 13),
				ctx.IncidentMessage)
		}

	case ReasonResolved:
		addResponsibleDetails(r, ctx, &details)

		summary = fmt.Sprintf("[%s] Incident resolved: %s", ctx.ObjectName, ctx.IncidentSummary)
		body = fmt.Sprintf(
			"An incident opened by %s for %s has been resolved.\n\n"+
				"Severity:    %s\n"+
				"Duration:    %s (%s - %s)\n"+
				"Responsible: %s\n"+
				"%s\n"+
				"%s",
			ctx.SourceName, ctx.ObjectName,
			severityDisplay(ctx.Severity),
			formatDuration(ctx.RecoveredAt.Sub(ctx.StartedAt)), formatTime(ctx.StartedAt), formatTime(ctx.RecoveredAt),
			managerOrUnassigned(ctx.ManagerName),
			detailsLine(ctx.ObjectURL, 13),
			ctx.IncidentMessage)

	case ReasonMuted:
		addMutingDetails(r, ctx, true, &details)
		addSeverityDetails(ctx, &details)

		summary = fmt.Sprintf("[%s] Incident muted: %s", ctx.ObjectName, ctx.IncidentSummary)
		body = fmt.Sprintf(
			"An incident opened by %s for %s has been muted.\n\n"+
				"Mute Reason: %s\n"+
				"Severity:    %s\n"+
				"Opened:      %s\n"+
				"%s\n"+
				"%s",
			ctx.SourceName, ctx.ObjectName,
			ctx.MuteReason,
			severityDisplay(ctx.Severity),
			formatTime(ctx.StartedAt),
			detailsLine(ctx.ObjectURL, 13),
			ctx.IncidentMessage)

	case ReasonUnmuted:
		addMutingDetails(r, ctx, false, &details)
		addSeverityDetails(ctx, &details)

		summary = fmt.Sprintf("[%s] Incident unmuted: %s", ctx.ObjectName, ctx.IncidentSummary)
		body = fmt.Sprintf(
			"An incident opened by %s for %s has been unmuted.\n\n"+
				"Unmute Reason: %s\n"+
				"Severity:      %s\n"+
				"Opened:        %s\n"+
				"%s\n"+
				"%s",
			ctx.SourceName, ctx.ObjectName,
			r.UnmuteReason,
			severityDisplay(ctx.Severity),
			formatTime(ctx.StartedAt),
			detailsLine(ctx.ObjectURL, 15),
			ctx.IncidentMessage)

	case ReasonManaged:
		addSeverityDetails(ctx, &details)
		addResponsibleDetails(r, ctx, &details)

		summary = fmt.Sprintf("[%s] Incident assigned: %s", ctx.ObjectName, ctx.IncidentSummary)
		body = fmt.Sprintf(
			"An incident opened by %s for %s has been assigned to %s.\n\n"+
				"You will not receive further notifications as long as %s is responsible.\n\n"+
				"Severity:    %s\n"+
				"Opened:      %s\n"+
				"Responsible: %s\n"+
				"%s\n"+
				"%s",
			ctx.SourceName, ctx.ObjectName, r.ManagerName,
			r.ManagerName,
			severityDisplay(ctx.Severity),
			formatTime(ctx.StartedAt),
			r.ManagerName,
			detailsLine(ctx.ObjectURL, 13),
			ctx.IncidentMessage)

	case ReasonUnmanaged:
		addSeverityDetails(ctx, &details)
		addResponsibleDetails(r, ctx, &details)

		summary = fmt.Sprintf("[%s] Incident unassigned: %s", ctx.ObjectName, ctx.IncidentSummary)
		body = fmt.Sprintf(
			"An incident opened by %s for %s has been unassigned from %s.\n\n"+
				"You will receive further notifications from now on.\n\n"+
				"Severity:    %s\n"+
				"Opened:      %s\n"+
				"Responsible: Unassigned\n"+
				"%s\n"+
				"%s",
			ctx.SourceName, ctx.ObjectName, r.ManagerName,
			severityDisplay(ctx.Severity),
			formatTime(ctx.StartedAt),
			detailsLine(ctx.ObjectURL, 13),
			ctx.IncidentMessage)

	case ReasonEvent:
		summary = fmt.Sprintf("[%s] %s", ctx.ObjectName, ctx.EventSummary)

		var more string
		if ctx.ObjectURL != "" {
			more = "For more information: " + ctx.ObjectURL
		}

		body = strings.Join(
			slices.DeleteFunc(
				[]string{ctx.EventMessage, more},
				func(s string) bool { return s == "" }),
			"\n\n")

	case ReasonIncidentEvent:
		addSeverityDetails(ctx, &details)
		addResponsibleDetails(r, ctx, &details)

		summary = fmt.Sprintf("[%s] Incident updated: %s", ctx.ObjectName, ctx.EventSummary)
		body = fmt.Sprintf(
			"An incident opened by %s for %s has been updated.\n\n"+
				"Severity:    %s\n"+
				"Opened:      %s\n"+
				"Responsible: %s\n"+
				"%s\n"+
				"%s",
			ctx.SourceName, ctx.ObjectName,
			severityDisplay(ctx.Severity),
			formatTime(ctx.StartedAt),
			managerOrUnassigned(ctx.ManagerName),
			detailsLine(ctx.ObjectURL, 13),
			ctx.EventMessage)
	}

	return summary, body, details
}

// addSeverityChangeDetails appends the new and old severity to the details slice.
func addSeverityChangeDetails(r *notifyReason, ctx *renderCtx, details *[]plugin.DeliveryDetail) {
	*details = append(
		*details,
		plugin.DeliveryDetail{
			Type:  "severity",
			Label: "New Severity",
			Value: ctx.Severity,
		},
		plugin.DeliveryDetail{
			Type:  "severity",
			Label: "Old Severity",
			Value: r.OldSeverity,
		},
	)
}

// addSeverityDetails appends the current severity to the details slice.
func addSeverityDetails(ctx *renderCtx, details *[]plugin.DeliveryDetail) {
	*details = append(*details, plugin.DeliveryDetail{
		Type:  "severity",
		Label: "Severity",
		Value: ctx.Severity,
	})
}

// addMutingDetails appends the mute status and reason to the details slice.
func addMutingDetails(r *notifyReason, ctx *renderCtx, v bool, details *[]plugin.DeliveryDetail) {
	*details = append(*details, plugin.DeliveryDetail{
		Type:  "boolean",
		Label: "Is Muted",
		Value: v,
	})

	if v {
		*details = append(*details, plugin.DeliveryDetail{
			Type:  "text",
			Label: "Mute Reason",
			Value: ctx.MuteReason,
		})
	} else {
		*details = append(*details, plugin.DeliveryDetail{
			Type:  "text",
			Label: "Unmute Reason",
			Value: r.UnmuteReason,
		})
	}
}

// addResponsibleDetails appends the responsible manager to the details slice, or the previous manager
// if the incident is currently unmanaged.
func addResponsibleDetails(r *notifyReason, ctx *renderCtx, details *[]plugin.DeliveryDetail) {
	if ctx.ManagerName != "" || r.Kind == ReasonManaged {
		var responsible string
		if r.Kind == ReasonManaged {
			responsible = r.ManagerName
		} else {
			responsible = ctx.ManagerName
		}
		*details = append(*details, plugin.DeliveryDetail{
			Type:  "text",
			Label: "Responsible",
			Value: responsible,
		})
		return
	}

	if r.Kind == ReasonUnmanaged {
		*details = append(*details, plugin.DeliveryDetail{
			Type:  "text",
			Label: "Was Responsible",
			Value: r.ManagerName,
		})
	}
}
