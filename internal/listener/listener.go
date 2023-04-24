package listener

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/icinga/icingadb/pkg/icingadb"
	"github.com/icinga/icingadb/pkg/types"
	"github.com/icinga/noma/internal/config"
	"github.com/icinga/noma/internal/event"
	"github.com/icinga/noma/internal/incident"
	"github.com/icinga/noma/internal/object"
	"github.com/icinga/noma/internal/recipient"
	"github.com/icinga/noma/internal/rule"
	"github.com/icinga/noma/internal/utils"
	"log"
	"net/http"
	"time"
)

type Listener struct {
	address       string
	db            *icingadb.DB
	runtimeConfig *config.RuntimeConfig
	mux           http.ServeMux
}

func NewListener(db *icingadb.DB, address string, runtimeConfig *config.RuntimeConfig) *Listener {
	l := &Listener{
		address:       address,
		db:            db,
		runtimeConfig: runtimeConfig,
	}
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

	if ev.Severity == event.SeverityNone && ev.Type == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprintln(w, "ignoring invalid event: must set 'type' or 'severity'")
		return
	}

	obj, err := object.FromTags(l.db, ev.Tags)
	if err != nil {
		log.Println(err)

		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprintln(w, err.Error())
		return
	}

	err = obj.UpdateMetadata(ev.SourceId, ev.Name, utils.ToDBString(ev.URL), ev.ExtraTags)
	if err != nil {
		log.Println(err)

		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprintln(w, err.Error())
		return
	}

	if err = ev.Sync(l.db, obj.ID); err != nil {
		log.Println(err)

		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprintln(w, err.Error())
		return
	}

	w.WriteHeader(http.StatusTeapot)
	_, _ = fmt.Fprintln(w, "received event")
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, obj.String())
	_, _ = fmt.Fprintln(w, ev.String())

	if ev.Severity == event.SeverityNone {
		// not a state event
		_, _ = fmt.Fprintf(w, "not a state event, ignoring for now\n")
		return
	}

	currentIncident, created := incident.GetCurrent(l.db, obj, ev.SourceId, ev.Severity != event.SeverityOK)

	if currentIncident == nil {
		if ev.Severity != event.SeverityOK {
			panic("non-OK state but no incident was created")
		}

		log.Printf("%s: ignoring superfluous OK state event from source %d", obj.DisplayName(), ev.SourceId)
		return
	}

	// TODO: better move all this logic somewhere into incident.go
	currentIncident.Lock()
	defer currentIncident.Unlock()

	if created {
		currentIncident.StartedAt = ev.Time
		err = currentIncident.Sync(incident.NewHistoryEntry(ev.Time, ev.ID, "opened incident"), nil)
		if err != nil {
			log.Println(err)
			return
		}
	}

	err = currentIncident.AddEvent(l.db, &ev)
	if err != nil {
		log.Println(err)
		return
	}

	//currentIncident.AddHistory(incident.NewHistoryEntry(ev.Time, "processing event"))

	oldIncidentSeverity := currentIncident.Severity()

	if currentIncident.SeverityBySource == nil {
		currentIncident.SeverityBySource = make(map[int64]event.Severity)
	}

	oldSourceSeverity := currentIncident.SeverityBySource[ev.SourceId]
	if oldSourceSeverity == event.SeverityNone {
		oldSourceSeverity = event.SeverityOK
	}
	if oldSourceSeverity != ev.Severity {
		hr := &incident.HistoryRow{
			EventID:     types.Int{NullInt64: sql.NullInt64{Int64: ev.ID, Valid: true}},
			Type:        incident.SourceSeverityChanged,
			NewSeverity: ev.Severity,
			OldSeverity: oldSourceSeverity,
		}
		err = currentIncident.AddHistory(
			incident.NewHistoryEntry(ev.Time, ev.ID, "source %d severity changed from %s to %s", ev.SourceId, oldSourceSeverity.String(), ev.Severity.String()),
			hr,
		)
		if err != nil {
			log.Println(err)
		}

		if ev.Severity != event.SeverityOK {
			currentIncident.SeverityBySource[ev.SourceId] = ev.Severity
		} else {
			delete(currentIncident.SeverityBySource, ev.SourceId)
		}
	}

	newIncidentSeverity := currentIncident.Severity()

	if newIncidentSeverity != oldIncidentSeverity {
		hr := &incident.HistoryRow{
			Type:        incident.SeverityChanged,
			NewSeverity: newIncidentSeverity,
			OldSeverity: oldIncidentSeverity,
		}
		err = currentIncident.Sync(
			incident.NewHistoryEntry(ev.Time, ev.ID, "incident severity changed from %s to %s", oldIncidentSeverity.String(), newIncidentSeverity.String()),
			hr,
		)
		if err != nil {
			log.Println(err)
		}
	}

	if newIncidentSeverity == event.SeverityOK {
		currentIncident.RecoveredAt = ev.Time
		err = incident.RemoveCurrent(obj, incident.NewHistoryEntry(ev.Time, ev.ID, "all sources recovered, closing incident"))
		if err != nil {
			log.Println(err)
		}
	}

	if currentIncident.State == nil {
		currentIncident.State = make(map[*rule.Rule]map[*rule.Escalation]*incident.EscalationState)
	}

	// Check if any (additional) rules match this object. Filters of rules that already have a state don't have
	// to be checked again, these rules already matched and stay effective for the ongoing incident.
	for _, r := range l.runtimeConfig.Rules {
		if !r.IsActive.Valid || !r.IsActive.Bool {
			continue
		}

		if _, ok := currentIncident.State[r]; !ok && (r.ObjectFilter == nil || r.ObjectFilter.Matches(obj)) {
			currentIncident.State[r] = make(map[*rule.Escalation]*incident.EscalationState)
			err = currentIncident.AddRuleMatchedHistory(r, incident.NewHistoryEntry(ev.Time, ev.ID, "rule %q matches", r.Name))
			if err != nil {
				log.Println(err)
			}
		}
	}

	for r, states := range currentIncident.State {
		if !r.IsActive.Valid || !r.IsActive.Bool {
			continue
		}

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
			currentIncident.Recipients = make(map[recipient.Recipient]*incident.RecipientState)
		}

		for escalation, state := range states {
			if state.TriggeredAt.Time().IsZero() {
				state.RuleEscalationID = escalation.ID
				state.TriggeredAt = types.UnixMilli(ev.Time)

				err = currentIncident.AddEscalationTriggeredHistory(
					state,
					incident.NewHistoryEntry(ev.Time, ev.ID, "rule %q reached escalation %q", r.Name, escalation.DisplayName()),
				)
				if err != nil {
					log.Println(err)
				}

				err = currentIncident.AddRecipient(escalation, ev.Time, ev.ID)
				if err != nil {
					log.Println(err)
				}
			}
		}

		managed := currentIncident.HasManager()

		contactChannels := make(map[*recipient.Contact]map[string]struct{})

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
			for chType := range channels {
				hr := &incident.HistoryRow{
					ContactID:   types.Int{NullInt64: sql.NullInt64{Int64: contact.ID, Valid: true}},
					Type:        incident.Notified,
					ChannelType: utils.ToDBString(chType),
				}
				err = currentIncident.AddHistory(
					incident.NewHistoryEntry(ev.Time, ev.ID, "notify %q via %q", contact.FullName, chType),
					hr,
				)
				if err != nil {
					log.Println(err)
				}

				chConf := l.runtimeConfig.ChannelByType[chType]
				if chConf == nil {
					log.Printf("ERROR: could not find config for channel type %q", chType)
					continue
				}

				plugin, err := chConf.GetPlugin()
				if err != nil {
					log.Printf("ERROR: could initialize channel type %q: %v", chType, err)
					continue
				}

				err = plugin.Send(contact, currentIncident, &ev)
				if err != nil {
					log.Printf("ERROR: failed to send via channel type %q: %v", chType, err)
					continue
				}
			}
		}
	}

	_, _ = fmt.Fprintln(w)
}
