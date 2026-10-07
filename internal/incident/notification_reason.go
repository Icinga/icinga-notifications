package incident

import (
	baseEv "github.com/icinga/icinga-go-library/notifications/event"
)

// ReasonKind classifies why a notification is being sent, driving which default summary/body template is used.
type ReasonKind int

const (
	ReasonUpdated ReasonKind = iota
	ReasonOpened
	ReasonEscalated
	ReasonResolved
	ReasonMuted
	ReasonUnmuted
	ReasonManaged
	ReasonUnmanaged
	ReasonEvent
	ReasonIncidentEvent
)

// notifyReason carries everything needed to pick and render the correct default notification template for one batch of
// notifications (one call to generateNotifications). Per-recipient Opened/Escalated overrides are applied separately
// via reasonFor based on RecipientState.IsNew.
type notifyReason struct {
	// Kind is the base reason applied to recipients who are NOT new to the incident (RecipientState.IsNew == false).
	Kind ReasonKind

	// IsNewIncident is true if this incident was just opened by the current event. It affects the Opened-vs-Escalated
	// choice made in reasonFor for recipients with RecipientState.IsNew == true.
	IsNewIncident bool

	// SeverityImproved and SeverityWorsened are true only when the severity actually changed as part of this batch.
	// Both false means the severity did not change.
	SeverityImproved bool
	SeverityWorsened bool
	OldSeverity      baseEv.Severity

	// Closing is true if the incident is being closed/resolved as part of this same event.
	Closing bool

	// CurrentlyMuted reflects the incident's mute state for this batch. JustMuted/JustUnmuted are true only when this
	// exact event is the one transitioning the mute state.
	CurrentlyMuted bool
	JustMuted      bool
	JustUnmuted    bool

	// UnmuteReason is only meaningful when JustUnmuted is true: the reason supplied by the unmuting event. It must be
	// carried here, as the incident's mute reason is already cleared when the notification is rendered.
	UnmuteReason string

	// ManagerName is only meaningful when Kind is ReasonManaged or ReasonUnmanaged: the name of the recipient being
	// assigned/unassigned as manager.
	ManagerName string
}

// reasonFor resolves the final per-recipient reason.
func (r notifyReason) reasonFor(notificationOnlyAndNotNew, recipientIsNew bool) notifyReason {
	effective := r

	if notificationOnlyAndNotNew {
		effective.Kind = ReasonIncidentEvent
	}

	if !recipientIsNew {
		return effective
	}

	switch effective.Kind {
	case ReasonManaged, ReasonUnmanaged, ReasonEvent, ReasonIncidentEvent:
		// Keep the reason.
	default:
		if effective.IsNewIncident {
			effective.Kind = ReasonOpened
		} else {
			effective.Kind = ReasonEscalated
		}
	}
	return effective
}
