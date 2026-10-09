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

	// ManagerName is only meaningful when Kind is ReasonManaged or ReasonUnmanaged: the name of the recipient being
	// assigned/unassigned as manager.
	ManagerName string
}

// reasonFor resolves the final per-recipient reason, applying the Opened/Escalated override for recipients that are new
// to the incident. Managed/Unmanaged reasons are never overridden this way.
func (r notifyReason) reasonFor(recipientIsNew bool) notifyReason {
	effective := r
	if recipientIsNew && r.Kind != ReasonManaged && r.Kind != ReasonUnmanaged {
		if r.IsNewIncident {
			effective.Kind = ReasonOpened
		} else {
			effective.Kind = ReasonEscalated
		}
	}
	return effective
}
