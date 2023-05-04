package listener

import (
	"crypto/subtle"
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
	configFile    *config.ConfigFile
	db            *icingadb.DB
	runtimeConfig *config.RuntimeConfig
	mux           http.ServeMux
}

func NewListener(db *icingadb.DB, configFile *config.ConfigFile, runtimeConfig *config.RuntimeConfig) *Listener {
	l := &Listener{
		configFile:    configFile,
		db:            db,
		runtimeConfig: runtimeConfig,
	}
	l.mux.HandleFunc("/process-event", l.ProcessEvent)
	l.mux.HandleFunc("/dump-config", l.DumpConfig)
	return l
}

func (l *Listener) Run() error {
	log.Printf("Starting listener on http://%s", l.configFile.Listen)
	return http.ListenAndServe(l.configFile.Listen, &l.mux)
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

	if ev.Severity != event.SeverityNone {
		const stateType = "state"
		if ev.Type == "" {
			ev.Type = stateType
		} else if ev.Type != stateType {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprintf(w, "ignoring invalid event: if 'severity' is set, 'type' must not be set or set to %q\n", stateType)
			return
		}
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

	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintln(w, "received event")
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, obj.String())
	_, _ = fmt.Fprintln(w, ev.String())

	if ev.Severity == event.SeverityNone {
		// not a state event
		_, _ = fmt.Fprintf(w, "not a state event, ignoring for now\n")
		return
	}

	currentIncident, created, err := incident.GetCurrent(l.db, obj, ev.Severity != event.SeverityOK)
	if err != nil {
		_, _ = fmt.Fprintln(w, err)

		log.Println(err)
		return
	}

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
		err = currentIncident.Sync()
		if err != nil {
			_, _ = fmt.Fprintln(w, err)

			log.Println(err)
			return
		}

		log.Printf("[%s %s] opened incident", obj.DisplayName(), currentIncident.String())

		historyRow := &incident.HistoryRow{Type: incident.Opened}
		_, err = currentIncident.AddHistory(&incident.HistoryEntry{Time: ev.Time, EventRowID: ev.ID}, historyRow, false)
	}

	err = currentIncident.AddEvent(l.db, &ev)
	if err != nil {
		log.Println(err)
		return
	}

	log.Println("processing event")

	oldIncidentSeverity := currentIncident.Severity()

	if currentIncident.SeverityBySource == nil {
		currentIncident.SeverityBySource = make(map[int64]event.Severity)
	}

	oldSourceSeverity := currentIncident.SeverityBySource[ev.SourceId]
	if oldSourceSeverity == event.SeverityNone {
		oldSourceSeverity = event.SeverityOK
	}

	var causedByIncidentHistoryId types.Int
	if oldSourceSeverity != ev.Severity {
		log.Printf(
			"[%s %s] source %d severity changed from %s to %s",
			obj.DisplayName(), currentIncident.String(), ev.SourceId,
			oldSourceSeverity.String(), ev.Severity.String(),
		)

		hr := &incident.HistoryRow{
			EventID:     types.Int{NullInt64: sql.NullInt64{Int64: ev.ID, Valid: true}},
			Type:        incident.SourceSeverityChanged,
			NewSeverity: ev.Severity,
			OldSeverity: oldSourceSeverity,
		}
		causedByIncidentHistoryId, err = currentIncident.AddHistory(
			&incident.HistoryEntry{Time: ev.Time, EventRowID: ev.ID, Message: ev.Message}, hr, true,
		)
		if err != nil {
			_, _ = fmt.Fprintln(w, err)

			log.Println(err)
			return
		}

		err := currentIncident.AddSourceSeverity(ev.Severity, ev.SourceId)
		if err != nil {
			_, _ = fmt.Fprintln(w, err)

			log.Println(err)
			return
		}

		if ev.Severity == event.SeverityOK {
			delete(currentIncident.SeverityBySource, ev.SourceId)
		}
	}

	newIncidentSeverity := currentIncident.Severity()

	if newIncidentSeverity != oldIncidentSeverity {
		log.Printf(
			"[%s %s] incident severity changed from %s to %s",
			obj.DisplayName(), currentIncident.String(),
			oldIncidentSeverity.String(), newIncidentSeverity.String(),
		)

		err = currentIncident.Sync()
		if err != nil {
			_, _ = fmt.Fprintln(w, err)

			log.Println(err)
			return
		}

		hr := &incident.HistoryRow{
			Type:                      incident.SeverityChanged,
			NewSeverity:               newIncidentSeverity,
			OldSeverity:               oldIncidentSeverity,
			CausedByIncidentHistoryID: causedByIncidentHistoryId,
		}

		causedByIncidentHistoryId, err = currentIncident.AddHistory(&incident.HistoryEntry{Time: ev.Time, EventRowID: ev.ID}, hr, true)
		if err != nil {
			_, _ = fmt.Fprintln(w, err)

			log.Println(err)
			return
		}
	}

	if newIncidentSeverity == event.SeverityOK {
		currentIncident.RecoveredAt = ev.Time
		log.Printf("[%s %s] all sources recovered, closing incident", obj.DisplayName(), currentIncident.String())

		err = incident.RemoveCurrent(obj, &incident.HistoryEntry{Time: ev.Time, EventRowID: ev.ID})
		if err != nil {
			_, _ = fmt.Fprintln(w, err)

			log.Println(err)
			return
		}
	}

	if currentIncident.EscalationState == nil {
		currentIncident.EscalationState = make(map[int64]*incident.EscalationState)
	}

	if currentIncident.Rules == nil {
		currentIncident.Rules = make(map[int64]struct{})
	}

	l.runtimeConfig.RLock()
	defer l.runtimeConfig.RUnlock()

	// Check if any (additional) rules match this object. Filters of rules that already have a state don't have
	// to be checked again, these rules already matched and stay effective for the ongoing incident.
	for _, r := range l.runtimeConfig.Rules {
		if !r.IsActive.Valid || !r.IsActive.Bool {
			continue
		}

		if _, ok := currentIncident.Rules[r.ID]; !ok {
			if r.ObjectFilter != nil {
				matched, err := r.ObjectFilter.Eval(obj)
				if err != nil {
					log.Printf("[%s %s] rule %q failed to evaulte object filter: %s", obj.DisplayName(), currentIncident.String(), r.Name, err)
				}

				if err != nil || !matched {
					continue
				}
			}

			currentIncident.Rules[r.ID] = struct{}{}
			log.Printf("[%s %s] rule %q matches", obj.DisplayName(), currentIncident.String(), r.Name)

			history := &incident.HistoryEntry{
				Time:                      ev.Time,
				CausedByIncidentHistoryId: causedByIncidentHistoryId,
				EventRowID:                ev.ID,
			}

			insertedId, err := currentIncident.AddRuleMatchedHistory(r, history)
			if err != nil {
				_, _ = fmt.Fprintln(w, err)

				log.Println(err)
				return
			}

			if insertedId.Valid && !causedByIncidentHistoryId.Valid {
				causedByIncidentHistoryId = insertedId
			}
		}
	}

	for rID := range currentIncident.Rules {
		r := l.runtimeConfig.Rules[rID]

		if r == nil || !r.IsActive.Valid || !r.IsActive.Bool {
			continue
		}

		// Check if new escalation stages are reached
		for _, escalation := range r.Escalations {
			if _, ok := currentIncident.EscalationState[escalation.ID]; !ok {
				matched := false

				if escalation.Condition == nil {
					matched = true
				} else {
					cond := &rule.EscalationFilter{
						IncidentAge:      ev.Time.Sub(currentIncident.StartedAt),
						IncidentSeverity: currentIncident.Severity(),
					}

					matched, err = escalation.Condition.Eval(cond)
					if err != nil {
						log.Printf(
							"[%s %s] rule %q failed to evaulte escalation %q condition: %s",
							obj.DisplayName(), currentIncident.String(), r.Name, escalation.DisplayName(), err,
						)

						matched = false
					}
				}

				if matched {
					currentIncident.EscalationState[escalation.ID] = new(incident.EscalationState)
				}
			}
		}
	}

	if currentIncident.Recipients == nil {
		currentIncident.Recipients = make(map[incident.RecipientKey]*incident.RecipientState)
	}

	for escalationID, state := range currentIncident.EscalationState {
		escalation := l.runtimeConfig.GetRuleEscalation(escalationID)
		for _, escalationRecipient := range escalation.Recipients {
			rk := incident.RecipientKey{ContactID: escalationRecipient.ContactID, GroupID: escalationRecipient.GroupID}
			for recipientKey, state := range currentIncident.Recipients {
				if recipientKey == rk {
					state.Channels[escalationRecipient.ChannelType] = struct{}{}
				}
			}
		}

		if state.TriggeredAt.Time().IsZero() {
			if escalation == nil {
				continue
			}

			state.RuleEscalationID = escalationID
			state.TriggeredAt = types.UnixMilli(ev.Time)

			r := l.runtimeConfig.Rules[escalation.RuleID]
			log.Printf("[%s %s] rule %q reached escalation %q", obj.DisplayName(), currentIncident.String(), r.Name, escalation.DisplayName())

			history := &incident.HistoryEntry{
				Time:                      ev.Time,
				EventRowID:                ev.ID,
				CausedByIncidentHistoryId: causedByIncidentHistoryId,
			}

			causedByIncidentHistoryId, err = currentIncident.AddEscalationTriggered(state, history)
			if err != nil {
				_, _ = fmt.Fprintln(w, err)

				log.Println(err)
				return
			}

			err = currentIncident.AddRecipient(escalation, ev.Time, ev.ID)
			if err != nil {
				_, _ = fmt.Fprintln(w, err)

				log.Println(err)
				return
			}
		}
	}

	managed := currentIncident.HasManager()

	contactChannels := make(map[*recipient.Contact]map[string]struct{})

	for recipientKey, state := range currentIncident.Recipients {
		if !managed || state.Role > incident.RoleRecipient {
			r := l.runtimeConfig.GetRecipient(recipientKey)
			if r == nil {
				continue
			}

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
				ContactID:                 types.Int{NullInt64: sql.NullInt64{Int64: contact.ID, Valid: true}},
				Type:                      incident.Notified,
				ChannelType:               utils.ToDBString(chType),
				CausedByIncidentHistoryID: causedByIncidentHistoryId,
			}

			log.Printf("[%s %s] notify %q via %q", obj.DisplayName(), currentIncident.String(), contact.FullName, chType)

			_, err = currentIncident.AddHistory(&incident.HistoryEntry{Time: ev.Time, EventRowID: ev.ID}, hr, false)
			if err != nil {
				log.Println(err)
			}

			chConf := l.runtimeConfig.Channels[chType]
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

	_, _ = fmt.Fprintln(w)
}

func (l *Listener) DumpConfig(w http.ResponseWriter, r *http.Request) {
	expectedPassword := l.configFile.DebugPassword
	if expectedPassword == "" {
		w.WriteHeader(http.StatusForbidden)
		_, _ = fmt.Fprintln(w, "config dump disables, no debug-password set in config")
		return
	}

	_, providedPassword, _ := r.BasicAuth()
	if subtle.ConstantTimeCompare([]byte(expectedPassword), []byte(providedPassword)) != 1 {
		w.Header().Set("WWW-Authenticate", `Basic realm="debug"`)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprintln(w, "please provide the debug-password as basic auth credentials (user is ignored)")
		return
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(&l.runtimeConfig.ConfigSet)
}
