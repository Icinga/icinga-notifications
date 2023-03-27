package listener

import (
	"encoding/json"
	"fmt"
	"github.com/icinga/noma/internal/contact"
	"github.com/icinga/noma/internal/event"
	"github.com/icinga/noma/internal/incident"
	"github.com/icinga/noma/internal/object"
	"github.com/icinga/noma/internal/rule"
	"log"
	"net/http"
	"time"
)

type Listener struct {
	address string
	mux     http.ServeMux
}

func NewListener(address string) *Listener {
	l := &Listener{address: address}
	l.mux.HandleFunc("/process-event", l.ProcessEvent)
	return l
}

func (l *Listener) Run() error {
	log.Printf("Starting listener on http://%s", l.address)
	return http.ListenAndServe(l.address, &l.mux)
}

func (l *Listener) ProcessEvent(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		_, _ = fmt.Fprintln(w, "POST required")
		return
	}

	var ev event.Event
	err := json.NewDecoder(req.Body).Decode(&ev)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprintf(w, "cannot parse JSON body: %v\n", err)
		return
	}
	ev.Time = time.Now()

	obj := object.FromTags(ev.Tags)
	obj.UpdateMetadata(ev.Source, ev.Name, ev.URL, ev.ExtraTags)

	w.WriteHeader(http.StatusTeapot)
	_, _ = fmt.Fprintln(w, "received event")
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, obj.String())
	_, _ = fmt.Fprintln(w, ev.String())

	if ev.Severity == event.Severity(0) {
		// not a state event
		_, _ = fmt.Fprintf(w, "not a state event, ignoring for now\n")
		return
	}

	currentIncident, created := incident.GetCurrent(obj, ev.Severity != event.SeverityOK)

	if currentIncident == nil {
		if ev.Severity != event.SeverityOK {
			panic("non-OK state but no incident was created")
		}

		log.Printf("%s: ignoring superfluous OK state event from source %d", obj.DisplayName(), ev.Source)
		return
	}

	// TODO: better move all this logic somewhere into incident.go
	currentIncident.Lock()
	defer currentIncident.Unlock()

	if created {
		currentIncident.StartedAt = ev.Time
		currentIncident.AddHistory(ev.Time, "opened incident")
	}

	currentIncident.AddHistory(ev.Time, "processing event")

	oldIncidentSeverity := currentIncident.Severity()

	if currentIncident.SeverityBySource == nil {
		currentIncident.SeverityBySource = make(map[int64]event.Severity)
	}

	oldSourceSeverity := currentIncident.SeverityBySource[ev.Source]
	if oldSourceSeverity == event.Severity(0) {
		oldSourceSeverity = event.SeverityOK
	}
	if oldSourceSeverity != ev.Severity {
		currentIncident.AddHistory(ev.Time, "source %d severity changed from %s to %s", ev.Source, oldSourceSeverity.String(), ev.Severity.String())

		if ev.Severity != event.SeverityOK {
			currentIncident.SeverityBySource[ev.Source] = ev.Severity
		} else {
			delete(currentIncident.SeverityBySource, ev.Source)
		}
	}

	newIncidentSeverity := currentIncident.Severity()

	if newIncidentSeverity != oldIncidentSeverity {
		currentIncident.AddHistory(ev.Time, "incident severity changed from %s to %s", oldIncidentSeverity.String(), newIncidentSeverity.String())
	}

	if newIncidentSeverity == event.SeverityOK {
		currentIncident.AddHistory(ev.Time, "all sources recovered, closing incident")
		currentIncident.RecoveredAt = ev.Time
		incident.RemoveCurrent(obj)
	}

	if currentIncident.State == nil {
		currentIncident.State = make(map[*rule.Rule]map[*rule.Escalation]*incident.EscalationState)
	}

	// Check if any (additional) rules match this object. Filters of rules that already have a state don't have
	// to be checked again, these rules already matched and stay effective for the ongoing incident.
	for _, r := range rule.Rules {
		if _, ok := currentIncident.State[r]; !ok && r.ObjectFilter.Matches(obj) {
			currentIncident.AddHistory(ev.Time, "rule %q matches", r.Name)
			currentIncident.State[r] = make(map[*rule.Escalation]*incident.EscalationState)
		}
	}

	for r, states := range currentIncident.State {
		// Check if new escalation stages are reached
		for _, escalation := range r.Escalations {
			if _, ok := states[escalation]; !ok {
				cond := escalation.Condition
				match := false

				if cond == nil {
					match = true
				} else if cond.MinDuration > 0 && ev.Time.Sub(currentIncident.StartedAt) > cond.MinDuration {
					match = true
				} else if cond.MinSeverity > 0 && currentIncident.Severity() >= cond.MinSeverity {
					match = true
				}

				if match {
					states[escalation] = new(incident.EscalationState)
				}
			}
		}

		if currentIncident.Recipients == nil {
			currentIncident.Recipients = make(map[contact.Recipient]*incident.RecipientState)
		}

		newRole := incident.RoleRecipient
		if currentIncident.HasManager() {
			newRole = incident.RoleSubscriber
		}

		for escalation, state := range states {
			if state.TriggeredAt.IsZero() {
				state.TriggeredAt = ev.Time
				currentIncident.AddHistory(ev.Time, "rule %q reached escalation %q", r.Name, escalation.DisplayName())

				addRecipient := func(r contact.Recipient) {
					state, ok := currentIncident.Recipients[r]
					if !ok {
						currentIncident.Recipients[r] = &incident.RecipientState{
							Role:     newRole,
							Channels: map[string]struct{}{escalation.ChannelType: {}},
						}
					} else {
						if state.Role < newRole {
							state.Role = newRole
						}
						state.Channels[escalation.ChannelType] = struct{}{}
					}
				}

				for _, c := range escalation.Contacts {
					addRecipient(c)
				}

				for _, g := range escalation.ContactGroups {
					addRecipient(g)
				}

				for _, s := range escalation.Schedules {
					addRecipient(s)
				}
			}
		}

		managed := currentIncident.HasManager()

		contactChannels := make(map[*contact.Contact]map[string]struct{})

		for r, state := range currentIncident.Recipients {
			if !managed || state.Role > incident.RoleRecipient {
				for _, c := range r.GetContactsAt(ev.Time) {
					channels := contactChannels[c]
					if channels == nil {
						channels = make(map[string]struct{})
						contactChannels[c] = channels
					}
					for channel := range state.Channels {
						channels[channel] = struct{}{}
					}
				}
			}
		}

		for contact, channels := range contactChannels {
			for channel := range channels {
				currentIncident.AddHistory(ev.Time, "notify %q via %q", contact.FullName, channel)
			}
		}
	}

	_, _ = fmt.Fprintln(w)
}
