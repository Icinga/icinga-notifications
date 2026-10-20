package incident

import (
	"testing"
	"time"

	baseEv "github.com/icinga/icinga-go-library/notifications/event"
	"github.com/stretchr/testify/assert"
)

func TestRenderNotification(t *testing.T) {
	baseCtx := renderCtx{
		ObjectName:      "server01: disk /",
		ObjectURL:       "https://example.com/object",
		Severity:        baseEv.SeverityCritical,
		IncidentMessage: "Disk CRITICAL - no space left on device",
		IncidentSummary: "Disk CRITICAL - no space left on device",
		SourceName:      "icinga2-master",
		StartedAt:       time.Date(2024, 6, 1, 12, 34, 56, 0, time.UTC),
		MuteReason:      "Maintenance window",
	}

	tests := []struct {
		name            string
		reason          notifyReason
		ctx             renderCtx
		expectSummary   string
		bodyContains    []string
		bodyNotContains []string
	}{
		{
			name: "opened",
			reason: notifyReason{
				Kind:          ReasonOpened,
				IsNewIncident: true,
			},
			ctx:           baseCtx,
			expectSummary: "[server01: disk /] Incident opened: Disk CRITICAL - no space left on device",
			bodyContains: []string{
				"A new incident has been opened",
				"icinga2-master",
				"server01: disk /",
				"Severity: Critical",
				"Disk CRITICAL - no space left on device",
			},
		},
		{
			name: "opened: muted",
			reason: notifyReason{
				Kind:           ReasonOpened,
				IsNewIncident:  true,
				CurrentlyMuted: true,
			},
			ctx:           baseCtx,
			expectSummary: "[server01: disk /] Incident opened: Disk CRITICAL - no space left on device",
			bodyContains: []string{
				"A new incident has been opened",
				"The incident is muted",
				"Mute Reason: Maintenance window",
				"Severity:    Critical",
			},
		},
		{
			name: "opened: closed",
			reason: notifyReason{
				Kind:          ReasonOpened,
				IsNewIncident: true,
				Closing:       true,
			},
			ctx:           baseCtx,
			expectSummary: "[server01: disk /] Incident opened: Disk CRITICAL - no space left on device",
			bodyContains: []string{
				"A new incident has been opened",
				"This will remain the only notification as the incident has already been closed",
				"Severity:    Critical",
			},
		},
		{
			name: "escalated: severity change",
			reason: notifyReason{
				Kind:             ReasonEscalated,
				SeverityWorsened: true,
				OldSeverity:      baseEv.SeverityWarning,
			},
			ctx:           baseCtx,
			expectSummary: "[server01: disk /] Incident escalated: Disk CRITICAL - no space left on device",
			bodyContains: []string{
				"was escalated to you",
				"Severity:    Critical (was Warning)",
				"Responsible: Unassigned",
			},
		},
		{
			name: "escalated: age only",
			reason: notifyReason{
				Kind:        ReasonEscalated,
				OldSeverity: baseEv.SeverityCritical,
			},
			ctx: func() renderCtx {
				c := baseCtx
				c.Age = 2 * time.Hour
				return c
			}(),
			expectSummary: "[server01: disk /] Incident escalated: Disk CRITICAL - no space left on device",
			bodyContains: []string{
				"was escalated to you",
				"The incident has been open for",
				"The last message received:",
			},
		},
		{
			name: "escalated: muted and severity",
			reason: notifyReason{
				Kind:             ReasonEscalated,
				SeverityWorsened: true,
				OldSeverity:      baseEv.SeverityWarning,
				CurrentlyMuted:   true,
			},
			ctx:           baseCtx,
			expectSummary: "[server01: disk /] Incident escalated: Disk CRITICAL - no space left on device",
			bodyContains: []string{
				"was escalated to you",
				"The incident is muted",
				"Mute Reason:",
				"Severity:    Critical (was Warning)",
			},
		},
		{
			name: "escalated: muted and age",
			reason: notifyReason{
				Kind:           ReasonEscalated,
				CurrentlyMuted: true,
				OldSeverity:    baseEv.SeverityCritical,
			},
			ctx: func() renderCtx {
				c := baseCtx
				c.Age = 1 * time.Hour
				return c
			}(),
			expectSummary: "[server01: disk /] Incident escalated: Disk CRITICAL - no space left on device",
			bodyContains: []string{
				"was escalated to you",
				"The incident is muted",
				"The incident has been open for",
				"The last message received:",
			},
		},
		{
			name: "escalated: unmuted",
			reason: notifyReason{
				Kind:             ReasonEscalated,
				SeverityWorsened: true,
				OldSeverity:      baseEv.SeverityWarning,
				JustUnmuted:      true,
				UnmuteReason:     "Maintenance over",
			},
			ctx:           baseCtx,
			expectSummary: "[server01: disk /] Incident escalated: Disk CRITICAL - no space left on device",
			bodyContains: []string{
				"was escalated to you",
				"Severity:      Critical (was Warning)",
			},
			bodyNotContains: []string{
				"The incident was muted until now",
				"Unmute Reason: Maintenance over",
			},
		},
		{
			name: "escalated: closed",
			reason: notifyReason{
				Kind:        ReasonEscalated,
				Closing:     true,
				OldSeverity: baseEv.SeverityCritical,
			},
			ctx:           baseCtx,
			expectSummary: "[server01: disk /] Incident escalated: Disk CRITICAL - no space left on device",
			bodyContains: []string{
				"was escalated to you",
				"This will remain the only notification as the incident has already been closed",
				"Severity:    Critical\n",
			},
			bodyNotContains: []string{"(was "},
		},
		{
			name: "escalated: unmuted without severity change",
			reason: notifyReason{
				Kind:         ReasonEscalated,
				OldSeverity:  baseEv.SeverityCritical,
				JustUnmuted:  true,
				UnmuteReason: "Maintenance over",
			},
			ctx: func() renderCtx {
				c := baseCtx
				c.MuteReason = ""
				return c
			}(),
			expectSummary: "[server01: disk /] Incident escalated: Disk CRITICAL - no space left on device",
			bodyContains: []string{
				"Severity:      Critical\n",
			},
			bodyNotContains: []string{
				"(was ",
				"Unmute Reason: Maintenance over",
			},
		},
		{
			name: "updated: improved",
			reason: notifyReason{
				Kind:             ReasonUpdated,
				SeverityImproved: true,
				OldSeverity:      baseEv.SeverityError,
			},
			ctx:           baseCtx,
			expectSummary: "[server01: disk /] Incident improved: Disk CRITICAL - no space left on device",
			bodyContains: []string{
				"has improved",
				"Severity:    Critical (was Error)",
			},
		},
		{
			name: "updated: worsened",
			reason: notifyReason{
				Kind:             ReasonUpdated,
				SeverityWorsened: true,
				OldSeverity:      baseEv.SeverityWarning,
			},
			ctx:           baseCtx,
			expectSummary: "[server01: disk /] Incident worsened: Disk CRITICAL - no space left on device",
			bodyContains: []string{
				"has worsened",
				"Severity:    Critical (was Warning)",
			},
			bodyNotContains: []string{"The incident was muted until now"},
		},
		{
			name: "updated: improved and unmuted",
			reason: notifyReason{
				Kind:             ReasonUpdated,
				SeverityImproved: true,
				OldSeverity:      baseEv.SeverityOK,
				JustUnmuted:      true,
				UnmuteReason:     "Maintenance over",
			},
			ctx:           baseCtx,
			expectSummary: "[server01: disk /] Incident improved: Disk CRITICAL - no space left on device",
			bodyContains: []string{
				"has improved",
				"The incident was muted until now. You will receive further notifications from now on.",
				"Unmute Reason: Maintenance over",
				"Severity:    Critical (was OK)",
			},
		},
		{
			name: "updated: worsened and unmuted",
			reason: notifyReason{
				Kind:             ReasonUpdated,
				SeverityWorsened: true,
				OldSeverity:      baseEv.SeverityWarning,
				JustUnmuted:      true,
				UnmuteReason:     "Maintenance over",
			},
			ctx:           baseCtx,
			expectSummary: "[server01: disk /] Incident worsened: Disk CRITICAL - no space left on device",
			bodyContains: []string{
				"has worsened",
				"The incident was muted until now. You will receive further notifications from now on.",
				"Unmute Reason: Maintenance over",
				"Severity:    Critical (was Warning)",
			},
		},
		{
			name: "updated: unchanged",
			reason: notifyReason{
				Kind: ReasonUpdated,
			},
			ctx:           baseCtx,
			expectSummary: "[server01: disk /] Incident updated: Disk CRITICAL - no space left on device",
			bodyContains: []string{
				"has been updated",
				"Severity:    Critical",
			},
		},
		{
			name: "resolved",
			reason: notifyReason{
				Kind: ReasonResolved,
			},
			ctx: func() renderCtx {
				c := baseCtx
				c.RecoveredAt = time.Date(2024, 6, 1, 14, 34, 56, 0, time.UTC)
				return c
			}(),
			expectSummary: "[server01: disk /] Incident resolved: Disk CRITICAL - no space left on device",
			bodyContains: []string{
				"has been resolved",
				"Duration:",
				"2h0m0s",
			},
		},
		{
			name: "muted",
			reason: notifyReason{
				Kind: ReasonMuted,
			},
			ctx:           baseCtx,
			expectSummary: "[server01: disk /] Incident muted: Disk CRITICAL - no space left on device",
			bodyContains: []string{
				"has been muted",
				"Mute Reason: Maintenance window",
			},
		},
		{
			name: "unmuted",
			reason: notifyReason{
				Kind:         ReasonUnmuted,
				JustUnmuted:  true,
				UnmuteReason: "Maintenance over",
			},
			ctx: func() renderCtx {
				c := baseCtx
				c.MuteReason = "" // cleared by handleUnmute before rendering
				return c
			}(),
			expectSummary: "[server01: disk /] Incident unmuted: Disk CRITICAL - no space left on device",
			bodyContains: []string{
				"has been unmuted",
				"Unmute Reason: Maintenance over",
			},
		},
		{
			name: "managed",
			reason: notifyReason{
				Kind:        ReasonManaged,
				ManagerName: "Alice",
			},
			ctx:           baseCtx,
			expectSummary: "[server01: disk /] Incident assigned: Disk CRITICAL - no space left on device",
			bodyContains: []string{
				"has been assigned to Alice",
				"Responsible: Alice",
			},
		},
		{
			name: "unmanaged",
			reason: notifyReason{
				Kind:        ReasonUnmanaged,
				ManagerName: "Bob",
			},
			ctx:           baseCtx,
			expectSummary: "[server01: disk /] Incident unassigned: Disk CRITICAL - no space left on device",
			bodyContains: []string{
				"has been unassigned from Bob",
				"Responsible: Unassigned",
			},
		},
		{
			name:   "event",
			reason: notifyReason{Kind: ReasonEvent},
			ctx: renderCtx{
				ObjectName:   "server01: disk /",
				ObjectURL:    "https://example.com/object",
				EventSummary: "Acknowledged",
				EventMessage: "Looking into it\nSecond line",
			},
			expectSummary: "[server01: disk /] Acknowledged",
		},
		{
			name:   "event: summary fallback to first message line",
			reason: notifyReason{Kind: ReasonEvent},
			ctx: renderCtx{
				ObjectName:   "server01: disk /",
				EventMessage: "\n Looking into it\nSecond line",
			},
			expectSummary: "[server01: disk /] Looking into it",
			bodyContains:  []string{"Second line"},
		},
		{
			name:   "incident event",
			reason: notifyReason{Kind: ReasonIncidentEvent},
			ctx: func() renderCtx {
				c := baseCtx
				c.EventSummary = "Acknowledged"
				c.EventMessage = "Looking into it"
				c.ManagerName = "Alice"
				return c
			}(),
			expectSummary: "[server01: disk /] Incident updated: Acknowledged",
			bodyContains: []string{
				"An incident opened by icinga2-master for server01: disk / has been updated.",
				"Severity:    Critical",
				"Responsible: Alice",
				"Looking into it",
			},
			bodyNotContains: []string{"Disk CRITICAL"},
		},
		{
			name:   "incident event: summary fallback to first message line",
			reason: notifyReason{Kind: ReasonIncidentEvent},
			ctx: func() renderCtx {
				c := baseCtx
				c.EventMessage = "Looking into it\nSecond line"
				return c
			}(),
			expectSummary: "[server01: disk /] Incident updated: Looking into it",
			bodyContains:  []string{"Responsible: Unassigned", "Looking into it\nSecond line"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			summary, body := renderNotification(&tt.reason, &tt.ctx)
			assert.Equal(t, tt.expectSummary, summary)
			for _, phrase := range tt.bodyContains {
				assert.Contains(t, body, phrase)
			}
			for _, phrase := range tt.bodyNotContains {
				assert.NotContains(t, body, phrase)
			}
		})
	}
}

func TestRenderNotificationDetails(t *testing.T) {
	ctx := renderCtx{
		ObjectName:      "server01: disk /",
		Severity:        baseEv.SeverityCritical,
		IncidentMessage: "Disk CRITICAL - no space left on device",
		IncidentSummary: "Disk CRITICAL - no space left on device",
		SourceName:      "icinga2-master",
		StartedAt:       time.Date(2024, 6, 1, 12, 34, 56, 0, time.UTC),
		EventMessage:    "Looking into it",
	}
	ctxWithURL := ctx
	ctxWithURL.ObjectURL = "https://example.com/object"

	tests := []struct {
		name       string
		reason     notifyReason
		ctx        renderCtx
		expectBody string
	}{
		{
			name:   "opened with url",
			reason: notifyReason{Kind: ReasonOpened, IsNewIncident: true},
			ctx:    ctxWithURL,
			expectBody: "A new incident has been opened by icinga2-master for server01: disk /.\n\n" +
				"Severity: Critical\n" +
				"Opened:   2024-06-01 12:34:56 +0000\n" +
				"Details:  https://example.com/object\n\n" +
				"Disk CRITICAL - no space left on device",
		},
		{
			name:   "opened without url",
			reason: notifyReason{Kind: ReasonOpened, IsNewIncident: true},
			ctx:    ctx,
			expectBody: "A new incident has been opened by icinga2-master for server01: disk /.\n\n" +
				"Severity: Critical\n" +
				"Opened:   2024-06-01 12:34:56 +0000\n\n" +
				"Disk CRITICAL - no space left on device",
		},
		{
			name:       "event with url",
			reason:     notifyReason{Kind: ReasonEvent},
			ctx:        ctxWithURL,
			expectBody: "Looking into it\n\nFor more information: https://example.com/object",
		},
		{
			name:       "event without url",
			reason:     notifyReason{Kind: ReasonEvent},
			ctx:        ctx,
			expectBody: "Looking into it",
		},
		{
			name:   "event with url and without message",
			reason: notifyReason{Kind: ReasonEvent},
			ctx: func() renderCtx {
				c := ctxWithURL
				c.EventMessage = ""
				return c
			}(),
			expectBody: "For more information: https://example.com/object",
		},
		{
			name:   "event without url and message",
			reason: notifyReason{Kind: ReasonEvent},
			ctx: func() renderCtx {
				c := ctx
				c.EventMessage = ""
				return c
			}(),
			expectBody: "",
		},
		{
			name:   "incident event with url and manager",
			reason: notifyReason{Kind: ReasonIncidentEvent},
			ctx: func() renderCtx {
				c := ctxWithURL
				c.ManagerName = "Alice"
				return c
			}(),
			expectBody: "An incident opened by icinga2-master for server01: disk / has been updated.\n\n" +
				"Severity:    Critical\n" +
				"Opened:      2024-06-01 12:34:56 +0000\n" +
				"Responsible: Alice\n" +
				"Details:     https://example.com/object\n\n" +
				"Looking into it",
		},
		{
			name:   "incident event without url and manager",
			reason: notifyReason{Kind: ReasonIncidentEvent},
			ctx:    ctx,
			expectBody: "An incident opened by icinga2-master for server01: disk / has been updated.\n\n" +
				"Severity:    Critical\n" +
				"Opened:      2024-06-01 12:34:56 +0000\n" +
				"Responsible: Unassigned\n\n" +
				"Looking into it",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, body := renderNotification(&tt.reason, &tt.ctx)
			assert.Equal(t, tt.expectBody, body)
		})
	}

	for kind := ReasonUpdated; kind <= ReasonIncidentEvent; kind++ {
		c := ctx
		_, body := renderNotification(&notifyReason{Kind: kind}, &c)
		assert.NotContains(t, body, "Details:", "kind %d", kind)
		assert.NotContains(t, body, "\n\n\n", "kind %d", kind)
	}
}

func TestReasonFor(t *testing.T) {
	tests := []struct {
		name                      string
		baseReason                notifyReason
		notificationOnlyAndNotNew bool
		recipientNew              bool
		expectKind                ReasonKind
	}{
		{
			name:         "non-new recipient keeps base kind",
			baseReason:   notifyReason{Kind: ReasonUpdated},
			recipientNew: false,
			expectKind:   ReasonUpdated,
		},
		{
			name: "new recipient on new incident becomes opened",
			baseReason: notifyReason{
				Kind:          ReasonUpdated,
				IsNewIncident: true,
			},
			recipientNew: true,
			expectKind:   ReasonOpened,
		},
		{
			name: "new recipient on existing incident becomes escalated",
			baseReason: notifyReason{
				Kind: ReasonUpdated,
			},
			recipientNew: true,
			expectKind:   ReasonEscalated,
		},
		{
			name: "new recipient does not override managed kind",
			baseReason: notifyReason{
				Kind:          ReasonManaged,
				IsNewIncident: true,
			},
			recipientNew: true,
			expectKind:   ReasonManaged,
		},
		{
			name: "new recipient does not override unmanaged kind",
			baseReason: notifyReason{
				Kind:          ReasonUnmanaged,
				IsNewIncident: true,
			},
			recipientNew: true,
			expectKind:   ReasonUnmanaged,
		},
		{
			name:         "new recipient does not override event kind",
			baseReason:   notifyReason{Kind: ReasonEvent},
			recipientNew: true,
			expectKind:   ReasonEvent,
		},
		{
			name:         "new recipient does not override incident event kind",
			baseReason:   notifyReason{Kind: ReasonIncidentEvent, IsNewIncident: true},
			recipientNew: true,
			expectKind:   ReasonIncidentEvent,
		},
		{
			name:                      "notification-only event on existing incident becomes incident event",
			baseReason:                notifyReason{},
			notificationOnlyAndNotNew: true,
			recipientNew:              false,
			expectKind:                ReasonIncidentEvent,
		},
		{
			name:                      "notification-only event on existing incident is not overridden for new recipient",
			baseReason:                notifyReason{},
			notificationOnlyAndNotNew: true,
			recipientNew:              true,
			expectKind:                ReasonIncidentEvent,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			effective := tt.baseReason.reasonFor(tt.notificationOnlyAndNotNew, tt.recipientNew)
			assert.Equal(t, tt.expectKind, effective.Kind)
		})
	}
}
