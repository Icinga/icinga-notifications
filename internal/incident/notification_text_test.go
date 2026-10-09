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
		Severity:        baseEv.SeverityCrit,
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
				Kind:           ReasonOpened,
				IsNewIncident:  true,
				Closing:        false,
				CurrentlyMuted: false,
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
				Closing:        false,
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
				Kind:           ReasonOpened,
				IsNewIncident:  true,
				Closing:        true,
				CurrentlyMuted: false,
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
				IsNewIncident:    false,
				SeverityImproved: false,
				SeverityWorsened: true,
				OldSeverity:      baseEv.SeverityWarning,
				CurrentlyMuted:   false,
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
				Kind:             ReasonEscalated,
				IsNewIncident:    false,
				SeverityImproved: false,
				SeverityWorsened: false,
				CurrentlyMuted:   false,
				OldSeverity:      baseEv.SeverityCrit,
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
				IsNewIncident:    false,
				SeverityImproved: false,
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
				Kind:             ReasonEscalated,
				IsNewIncident:    false,
				SeverityImproved: false,
				SeverityWorsened: false,
				CurrentlyMuted:   true,
				OldSeverity:      baseEv.SeverityCrit,
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
				IsNewIncident:    false,
				SeverityImproved: false,
				SeverityWorsened: true,
				OldSeverity:      baseEv.SeverityWarning,
				CurrentlyMuted:   false,
				JustUnmuted:      true,
			},
			ctx:           baseCtx,
			expectSummary: "[server01: disk /] Incident escalated: Disk CRITICAL - no space left on device",
			bodyContains: []string{
				"was escalated to you",
				"The incident was muted until now",
				"Unmute Reason:",
				"Severity:      Critical (was Warning)",
			},
		},
		{
			name: "escalated: closed",
			reason: notifyReason{
				Kind:             ReasonEscalated,
				IsNewIncident:    false,
				SeverityImproved: false,
				SeverityWorsened: false,
				Closing:          true,
				CurrentlyMuted:   false,
				OldSeverity:      baseEv.SeverityCrit,
			},
			ctx:           baseCtx,
			expectSummary: "[server01: disk /] Incident escalated: Disk CRITICAL - no space left on device",
			bodyContains: []string{
				"was escalated to you",
				"This will remain the only notification as the incident has already been closed",
			},
		},
		{
			name: "updated: improved",
			reason: notifyReason{
				Kind:             ReasonUpdated,
				SeverityImproved: true,
				OldSeverity:      baseEv.SeverityErr,
				CurrentlyMuted:   false,
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
				CurrentlyMuted:   false,
			},
			ctx:           baseCtx,
			expectSummary: "[server01: disk /] Incident worsened: Disk CRITICAL - no space left on device",
			bodyContains: []string{
				"has worsened",
				"Severity:    Critical (was Warning)",
			},
		},
		{
			name: "updated: unchanged",
			reason: notifyReason{
				Kind:             ReasonUpdated,
				SeverityImproved: false,
				SeverityWorsened: false,
				CurrentlyMuted:   false,
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
			expectSummary: "[icinga2-master][server01: disk /] Incident resolved: Disk CRITICAL - no space left on device",
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
				Kind: ReasonUnmuted,
			},
			ctx:           baseCtx,
			expectSummary: "[server01: disk /] Incident unmuted: Disk CRITICAL - no space left on device",
			bodyContains: []string{
				"has been unmuted",
				"Unmute Reason: Maintenance window",
			},
		},
		{
			name: "managed",
			reason: notifyReason{
				Kind:        ReasonManaged,
				ManagerName: "Alice",
			},
			ctx:           baseCtx,
			expectSummary: "[icinga2-master][server01: disk /] Incident assigned: Disk CRITICAL - no space left on device",
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			summary, body := renderNotification(tt.reason, tt.ctx)
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

func TestReasonFor(t *testing.T) {
	baseReason := notifyReason{
		Kind:             ReasonUpdated,
		IsNewIncident:    false,
		SeverityImproved: false,
		SeverityWorsened: false,
		CurrentlyMuted:   false,
	}

	tests := []struct {
		name         string
		baseReason   notifyReason
		recipientNew bool
		expectKind   ReasonKind
	}{
		{
			name:         "non-new recipient keeps base kind",
			baseReason:   baseReason,
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
				Kind:          ReasonUpdated,
				IsNewIncident: false,
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			effective := tt.baseReason.reasonFor(tt.recipientNew)
			assert.Equal(t, tt.expectKind, effective.Kind)
		})
	}
}
