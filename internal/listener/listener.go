package listener

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"github.com/icinga/icingadb/pkg/icingadb"
	"github.com/icinga/icingadb/pkg/logging"
	"github.com/icinga/icingadb/pkg/types"
	"github.com/icinga/noma/internal/config"
	"github.com/icinga/noma/internal/event"
	"github.com/icinga/noma/internal/incident"
	"github.com/icinga/noma/internal/object"
	"github.com/icinga/noma/internal/recipient"
	"github.com/icinga/noma/internal/rule"
	"github.com/icinga/noma/internal/utils"
	"net/http"
	"time"
)

type Listener struct {
	configFile    *config.ConfigFile
	db            *icingadb.DB
	logger        *logging.Logger
	runtimeConfig *config.RuntimeConfig
	mux           http.ServeMux
}

func NewListener(db *icingadb.DB, configFile *config.ConfigFile, runtimeConfig *config.RuntimeConfig, logger *logging.Logger) *Listener {
	l := &Listener{
		configFile:    configFile,
		db:            db,
		logger:        logger,
		runtimeConfig: runtimeConfig,
	}
	l.mux.HandleFunc("/process-event", l.ProcessEvent)
	l.mux.HandleFunc("/dump-config", l.DumpConfig)
	l.mux.HandleFunc("/dump-incidents", l.DumpIncidents)
	return l
}

func (l *Listener) Run() error {
	l.logger.Infof("Starting listener on http://%s", l.configFile.Listen)
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
		if ev.Type == "" {
			ev.Type = event.TypeState
		} else if ev.Type != event.TypeState {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprintf(w, "ignoring invalid event: if 'severity' is set, 'type' must not be set or set to %q\n", event.TypeState)
			return
		}
	}

	if ev.Severity == event.SeverityNone {
		if ev.Type != event.TypeAcknowledgement {
			// It's neither a state nor an acknowledgement event.
			w.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprintf(w, "received not a state/acknowledgement event, ignoring\n")
			return
		}
	}

	obj, err := object.FromTags(l.db, ev.Tags)
	if err != nil {
		l.logger.Errorln(err)

		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprintln(w, err.Error())
		return
	}

	err = obj.UpdateMetadata(ev.SourceId, ev.Name, utils.ToDBString(ev.URL), ev.ExtraTags)
	if err != nil {
		l.logger.Errorln(err)

		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprintln(w, err.Error())
		return
	}

	if err = ev.Sync(l.db, obj.ID); err != nil {
		l.logger.Errorln(err)

		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprintln(w, err.Error())
		return
	}

	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintln(w, "received event")
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, obj.String())
	_, _ = fmt.Fprintln(w, ev.String())

	createIncident := ev.Severity != event.SeverityNone && ev.Severity != event.SeverityOK
	currentIncident, created, err := incident.GetCurrent(l.db, obj, createIncident)
	if err != nil {
		_, _ = fmt.Fprintln(w, err)

		l.logger.Errorln(err)
		return
	}

	if currentIncident == nil {
		if ev.Type == event.TypeAcknowledgement {
			msg := fmt.Sprintf("%q doesn't have active incident. Ignoring acknowledgement event from source %d", obj.DisplayName(), ev.SourceId)
			_, _ = fmt.Fprintln(w, msg)

			l.logger.Warnln(msg)
			return
		}

		if ev.Severity != event.SeverityOK {
			panic("non-OK state but no incident was created")
		}

		l.logger.Warnf("%s: ignoring superfluous OK state event from source %d", obj.DisplayName(), ev.SourceId)
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

			l.logger.Errorln(err)
			return
		}

		l.logger.Infof("[%s %s] opened incident", obj.DisplayName(), currentIncident.String())

		historyRow := &incident.HistoryRow{
			Type:    incident.Opened,
			Time:    types.UnixMilli(ev.Time),
			EventID: utils.ToDBInt(ev.ID),
		}
		_, err = currentIncident.AddHistory(historyRow, false)
	}

	err = currentIncident.AddEvent(l.db, &ev)
	if err != nil {
		l.logger.Errorln(err)
		return
	}

	l.logger.Infof("processing event")

	if ev.Type == event.TypeAcknowledgement {
		err := l.ProcessAcknowledgementEvent(currentIncident, ev)
		if err != nil {
			_, _ = fmt.Fprintln(w, err)

			l.logger.Errorln(err)
		}

		return
	}

	oldIncidentSeverity := currentIncident.Severity()

	if currentIncident.SeverityBySource == nil {
		currentIncident.SeverityBySource = make(map[int64]event.Severity)
	}

	oldSourceSeverity := currentIncident.SeverityBySource[ev.SourceId]
	if oldSourceSeverity == event.SeverityNone {
		oldSourceSeverity = event.SeverityOK
	}

	if oldSourceSeverity == ev.Severity {
		msg := fmt.Sprintf("%s: ignoring superfluous %q state event from source %d", obj.DisplayName(), ev.Severity.String(), ev.SourceId)
		_, _ = fmt.Fprintln(w, msg)

		l.logger.Warnln(msg)
		return
	}

	var causedByIncidentHistoryId types.Int
	if oldSourceSeverity != ev.Severity {
		l.logger.Infof(
			"[%s %s] source %d severity changed from %s to %s",
			obj.DisplayName(), currentIncident.String(), ev.SourceId,
			oldSourceSeverity.String(), ev.Severity.String(),
		)

		hr := &incident.HistoryRow{
			EventID:     utils.ToDBInt(ev.ID),
			Type:        incident.SourceSeverityChanged,
			Time:        types.UnixMilli(time.Now()),
			NewSeverity: ev.Severity,
			OldSeverity: oldSourceSeverity,
			Message:     utils.ToDBString(ev.Message),
		}

		causedByIncidentHistoryId, err = currentIncident.AddHistory(hr, true)
		if err != nil {
			_, _ = fmt.Fprintln(w, err)

			l.logger.Errorln(err)
			return
		}

		err := currentIncident.AddSourceSeverity(ev.Severity, ev.SourceId)
		if err != nil {
			_, _ = fmt.Fprintln(w, err)

			l.logger.Errorln(err)
			return
		}

		if ev.Severity == event.SeverityOK {
			delete(currentIncident.SeverityBySource, ev.SourceId)
		}
	}

	newIncidentSeverity := currentIncident.Severity()

	if newIncidentSeverity != oldIncidentSeverity {
		l.logger.Infof(
			"[%s %s] incident severity changed from %s to %s",
			obj.DisplayName(), currentIncident.String(),
			oldIncidentSeverity.String(), newIncidentSeverity.String(),
		)

		err = currentIncident.Sync()
		if err != nil {
			_, _ = fmt.Fprintln(w, err)

			l.logger.Errorln(err)
			return
		}

		hr := &incident.HistoryRow{
			EventID:                   utils.ToDBInt(ev.ID),
			Time:                      types.UnixMilli(time.Now()),
			Type:                      incident.SeverityChanged,
			NewSeverity:               newIncidentSeverity,
			OldSeverity:               oldIncidentSeverity,
			CausedByIncidentHistoryID: causedByIncidentHistoryId,
		}

		causedByIncidentHistoryId, err = currentIncident.AddHistory(hr, true)
		if err != nil {
			_, _ = fmt.Fprintln(w, err)

			l.logger.Errorln(err)
			return
		}
	}

	if newIncidentSeverity == event.SeverityOK {
		currentIncident.RecoveredAt = time.Now()
		l.logger.Infof("[%s %s] all sources recovered, closing incident", obj.DisplayName(), currentIncident.String())

		hr := &incident.HistoryRow{
			EventID: utils.ToDBInt(ev.ID),
			Time:    types.UnixMilli(time.Now()),
			Type:    incident.Closed,
		}
		err = incident.RemoveCurrent(obj, hr)
		if err != nil {
			_, _ = fmt.Fprintln(w, err)

			l.logger.Errorln(err)
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
					l.logger.Warnf("[%s %s] rule %q failed to evaluate object filter: %s", obj.DisplayName(), currentIncident.String(), r.Name, err)
				}

				if err != nil || !matched {
					continue
				}
			}

			currentIncident.Rules[r.ID] = struct{}{}
			l.logger.Infof("[%s %s] rule %q matches", obj.DisplayName(), currentIncident.String(), r.Name)

			history := &incident.HistoryRow{
				Time:                      types.UnixMilli(time.Now()),
				EventID:                   utils.ToDBInt(ev.ID),
				RuleID:                    utils.ToDBInt(r.ID),
				Type:                      incident.RuleMatched,
				CausedByIncidentHistoryID: causedByIncidentHistoryId,
			}

			insertedId, err := currentIncident.AddRuleMatchedHistory(r, history)
			if err != nil {
				_, _ = fmt.Fprintln(w, err)

				l.logger.Errorln(err)
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
						l.logger.Infof(
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
		currentIncident.Recipients = make(map[recipient.Key]*incident.RecipientState)
	}

	managed := currentIncident.HasManager()

	contactChannels := make(map[*recipient.Contact]map[string]struct{})

	escalationRecipients := make(map[recipient.Key]bool)
	for escalationID, state := range currentIncident.EscalationState {
		escalation := l.runtimeConfig.GetRuleEscalation(escalationID)
		if state.TriggeredAt.Time().IsZero() {
			if escalation == nil {
				continue
			}

			state.RuleEscalationID = escalationID
			state.TriggeredAt = types.UnixMilli(time.Now())

			r := l.runtimeConfig.Rules[escalation.RuleID]
			l.logger.Infof("[%s %s] rule %q reached escalation %q", obj.DisplayName(), currentIncident.String(), r.Name, escalation.DisplayName())

			history := &incident.HistoryRow{
				Time:                      state.TriggeredAt,
				EventID:                   utils.ToDBInt(ev.ID),
				RuleEscalationID:          utils.ToDBInt(state.RuleEscalationID),
				RuleID:                    utils.ToDBInt(r.ID),
				Type:                      incident.EscalationTriggered,
				CausedByIncidentHistoryID: causedByIncidentHistoryId,
			}

			causedByIncidentHistoryId, err = currentIncident.AddEscalationTriggered(state, history)
			if err != nil {
				_, _ = fmt.Fprintln(w, err)

				l.logger.Errorln(err)
				return
			}

			err = currentIncident.AddRecipient(escalation, ev.ID)
			if err != nil {
				_, _ = fmt.Fprintln(w, err)

				l.logger.Errorln(err)
				return
			}
		}

		for _, escalationRecipient := range escalation.Recipients {
			state := currentIncident.Recipients[escalationRecipient.Key]
			if state == nil {
				continue
			}

			escalationRecipients[escalationRecipient.Key] = true

			if !managed || state.Role > incident.RoleRecipient {
				for _, c := range escalationRecipient.Recipient.GetContactsAt(ev.Time) {
					if contactChannels[c] == nil {
						contactChannels[c] = make(map[string]struct{})
					}
					if escalationRecipient.ChannelType.Valid {
						contactChannels[c][escalationRecipient.ChannelType.String] = struct{}{}
					} else {
						contactChannels[c][c.DefaultChannel] = struct{}{}
					}
				}
			}
		}
	}

	for recipientKey, state := range currentIncident.Recipients {
		r := l.runtimeConfig.GetRecipient(recipientKey)
		if r == nil {
			continue
		}

		isEscalationRecipient := escalationRecipients[recipientKey]
		if !isEscalationRecipient && (!managed || state.Role > incident.RoleRecipient) {
			for _, contact := range r.GetContactsAt(ev.Time) {
				if contactChannels[contact] == nil {
					contactChannels[contact] = make(map[string]struct{})
				}
				contactChannels[contact][contact.DefaultChannel] = struct{}{}
			}
		}
	}

	for contact, channels := range contactChannels {
		for chType := range channels {
			hr := &incident.HistoryRow{
				Key:                       recipient.ToKey(contact),
				EventID:                   utils.ToDBInt(ev.ID),
				Time:                      types.UnixMilli(time.Now()),
				Type:                      incident.Notified,
				ChannelType:               utils.ToDBString(chType),
				CausedByIncidentHistoryID: causedByIncidentHistoryId,
			}

			l.logger.Infof("[%s %s] notify %q via %q", obj.DisplayName(), currentIncident.String(), contact.FullName, chType)

			_, err = currentIncident.AddHistory(hr, false)
			if err != nil {
				l.logger.Errorln(err)
			}

			chConf := l.runtimeConfig.Channels[chType]
			if chConf == nil {
				l.logger.Errorw("ERROR: could not find config for channel type %q", chType)
				continue
			}

			plugin, err := chConf.GetPlugin()
			if err != nil {
				l.logger.Errorw("ERROR: could initialize channel type %q: %v", chType, err)
				continue
			}

			err = plugin.Send(contact, currentIncident, &ev, l.configFile.Icingaweb2URL)
			if err != nil {
				l.logger.Errorw("ERROR: failed to send via channel type %q: %v", chType, err)
				continue
			}
		}
	}

	_, _ = fmt.Fprintln(w)
}

// checkDebugPassword checks if the valid debug password was provided. If there is no password configured or the
// supplied password is incorrect, it sends an error code and returns false. True is returned if access is allowed.
func (l *Listener) checkDebugPassword(w http.ResponseWriter, r *http.Request) bool {
	expectedPassword := l.configFile.DebugPassword
	if expectedPassword == "" {
		w.WriteHeader(http.StatusForbidden)
		_, _ = fmt.Fprintln(w, "config dump disables, no debug-password set in config")

		return false
	}

	_, providedPassword, _ := r.BasicAuth()
	if subtle.ConstantTimeCompare([]byte(expectedPassword), []byte(providedPassword)) != 1 {
		w.Header().Set("WWW-Authenticate", `Basic realm="debug"`)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprintln(w, "please provide the debug-password as basic auth credentials (user is ignored)")
		return false
	}

	return true
}

func (l *Listener) DumpConfig(w http.ResponseWriter, r *http.Request) {
	if !l.checkDebugPassword(w, r) {
		return
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(&l.runtimeConfig.ConfigSet)
}

func (l *Listener) DumpIncidents(w http.ResponseWriter, r *http.Request) {
	if !l.checkDebugPassword(w, r) {
		return
	}

	incidents := incident.GetCurrentIncidents()
	encodedIncidents := make(map[int64]json.RawMessage)

	// Extra function to ensure that unlocking happens in all cases, including panic.
	encode := func(incident *incident.Incident) json.RawMessage {
		incident.Lock()
		defer incident.Unlock()

		encoded, err := json.Marshal(incident)
		if err != nil {
			encoded, err = json.Marshal(err.Error())
			if err != nil {
				// If a string can't be marshalled, something is very wrong.
				panic(err)
			}
		}

		return encoded
	}

	for id, incident := range incidents {
		encodedIncidents[id] = encode(incident)
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(encodedIncidents)
}

func (l *Listener) ProcessAcknowledgementEvent(i *incident.Incident, ev event.Event) error {
	l.runtimeConfig.RLock()
	defer l.runtimeConfig.RUnlock()

	contact := l.runtimeConfig.GetContact(ev.Username)
	if contact == nil {
		return fmt.Errorf("unknown acknowledgment author %q", ev.Username)
	}

	recipientKey := recipient.ToKey(contact)
	state := i.Recipients[recipientKey]
	oldRole := incident.RoleNone
	newRole := incident.RoleManager
	if state != nil {
		oldRole = state.Role

		if oldRole == incident.RoleManager {
			// The user is already a manager
			return nil
		}
	} else {
		i.Recipients[recipientKey] = &incident.RecipientState{Role: newRole}
	}

	l.logger.Infof("[%s %s] contact %q role changed from %s to %s", i.Object.DisplayName(), i.String(), contact.String(), oldRole.String(), newRole.String())

	hr := &incident.HistoryRow{
		Key:              recipientKey,
		EventID:          utils.ToDBInt(ev.ID),
		Type:             incident.RecipientRoleChanged,
		Time:             types.UnixMilli(time.Now()),
		NewRecipientRole: newRole,
		OldRecipientRole: oldRole,
		Message:          utils.ToDBString(ev.Message),
	}

	_, err := i.AddHistory(hr, false)
	if err != nil {
		return err
	}

	cr := &incident.ContactRow{IncidentID: hr.IncidentID, Key: recipientKey, Role: newRole}

	stmt, _ := l.db.BuildUpsertStmt(cr)
	_, err = l.db.NamedExec(stmt, cr)
	if err != nil {
		return fmt.Errorf("failed to upsert incident contact %s: %s", contact.String(), err)
	}

	return nil
}
