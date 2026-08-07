package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/icinga/icinga-go-library/notifications"
	"github.com/icinga/icinga-go-library/notifications/jsonrpc"
	"github.com/icinga/icinga-go-library/notifications/plugin"
	"github.com/icinga/icinga-notifications/internal"
)

func main() {
	plugin.Run(new(Sleep))
}

// Sleep is a plugin that sleeps for a specified duration before returning a success.
//
// It is used for testing purposes.
type (
	Sleep struct {
		DurationString string `json:"duration"`
		PersistState   bool   `json:"persist_state"`
		SpamStderr	  bool   `json:"spam_stderr"`

		duration time.Duration
		state    map[string]state
		mu       sync.Mutex

		rpcCtx context.Context
		rpcEp  *jsonrpc.Endpoint
	}

	// state represents a dummy state for the Sleep plugin, which can be persisted across restarts if PersistState is enabled.
	state struct {
		Type  string `json:"type"`
		Value string `json:"value"`
	}
)

func (s *Sleep) ReceiveEndpoint(ctx context.Context, ep *jsonrpc.Endpoint) {
	s.rpcCtx = ctx
	s.rpcEp = ep
}

func (s *Sleep) GetInfo() *plugin.Info {
	return &plugin.Info{
		Name:    "Sleep",
		Version: internal.Version.Version,
		Author:  "Icinga GmbH",
		ConfigAttributes: plugin.ConfigOptions{
			{
				Name: "duration",
				Type: "string",
				Label: map[string]string{
					"en_US": "Sleep Duration",
					"de_DE": "Dauer der Pause",
				},
				Required: true,
				Help: map[string]string{
					"en_US": `It specifies the duration for which the plugin will sleep before returning a success.
								The duration should be specified in a format that can be parsed by time.ParseDuration,
								e.g., '5s' for 5 seconds, '1m' for 1 minute.`,
					"de_DE": `Es gibt die Dauer an, für die das Plugin schlafen wird, bevor es einen Erfolg zurückgibt.
								Die Dauer sollte in einem Format angegeben werden, das von time.ParseDuration geparst
								werden kann, z.B. '5s' für 5 Sekunden, '1m' für 1 Minute.`,
				},
			},
			{
				Name: "persist_state",
				Type: "bool",
				Label: map[string]string{
					"en_US": "Persist State",
					"de_DE": "Status speichern",
				},
				Help: map[string]string{
					"en_US": "If set to true, the plugin will persist its state across invocations. This is useful for testing stateful behavior.",
					"de_DE": "Wenn auf true gesetzt, speichert das Plugin seinen Status über Aufrufe hinweg. Dies ist nützlich, um zustandsbehaftetes Verhalten zu testen.",
				},
			},
		},
	}
}

func (s *Sleep) SetConfig(jsonStr json.RawMessage) error {
	var tmp Sleep
	if err := json.Unmarshal(jsonStr, &tmp); err != nil {
		return err
	}

	duration, err := time.ParseDuration(tmp.DurationString)
	if err != nil {
		return err
	}

	s.mu.Lock()
	s.duration = duration
	s.PersistState = tmp.PersistState
	s.SpamStderr = tmp.SpamStderr
	s.mu.Unlock()

	return nil
}

func (s *Sleep) SendNotification(nr *plugin.NotificationRequest) error {
	slog.Info("Sleep plugin received notification request", "contact", nr.Contact.FullName, "incident_id", nr.Incident.Id, "recovered", nr.Incident.IsRecovered)

	if err := s.mergeState(nr.State); err != nil {
		return err
	}

	s.mu.Lock()
	duration := s.duration
	persistState := s.PersistState
	spamStderr := s.SpamStderr
	s.mu.Unlock()

	if persistState {
		compositeKey := composeKey(nr)
		if nr.Incident.IsRecovered {
			s.mu.Lock()
			delete(s.state, compositeKey)
			s.mu.Unlock()

			if err := s.rpcEp.Call(s.rpcCtx, notifications.MethodDeleteState, compositeKey, nil); err != nil {
				return err
			}
		} else {
			st := state{Type: "sleep", Value: fmt.Sprintf("Slept for %s", duration)}
			s.mu.Lock()
			s.state[compositeKey] = st
			s.mu.Unlock()

			err := s.rpcEp.Call(s.rpcCtx, notifications.MethodUpsertState, map[string]string{compositeKey: jsonMust(st)}, nil)
			if err != nil {
				return err
			}
		}
	}

	if spamStderr {
		// This stress tests the stderr handler on the Icinga Notifications side.
		slog.Info("Sending huge message to stderr", "message", strings.Repeat("X", 10*1024*1024)) // 10 MB message
		slog.Info("Sending huge message to stderr", "message", strings.Repeat("A", 10*1024*1024)) // 10 MB message
	}

	select {
	case <-s.rpcCtx.Done():
		return fmt.Errorf("plugin context canceled: %w", s.rpcCtx.Err())
	case <-time.After(duration):
		return fmt.Errorf("plugin slept for %s", duration)
	}
}

// mergeState merges the provided state map into the plugin's internal state, if PersistState is enabled.
func (s *Sleep) mergeState(stateM map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.PersistState {
		return nil
	}

	if s.state == nil {
		s.state = make(map[string]state)
	}

	for k, v := range stateM {
		var tmp state
		if err := json.Unmarshal([]byte(v), &tmp); err != nil {
			return fmt.Errorf("failed to unmarshal state for key %s: %w", k, err)
		}
		s.state[k] = tmp
	}
	return nil
}

// composeKey generates a unique key for the plugin state based on the contact's Sleep address and the incident ID.
func composeKey(nr *plugin.NotificationRequest) string {
	return fmt.Sprintf("%s.#%d", getSleepyAddr(nr.Contact), nr.Incident.Id)
}

// getSleepyAddr returns the address of the contact for the Sleep plugin, or a default value if not found.
func getSleepyAddr(c *plugin.Contact) string {
	for _, addr := range c.Addresses {
		if addr.Type == "sleep" {
			return addr.Address
		}
	}
	return "unknown.sleepy.addr"
}

// jsonMust is a helper function that marshals the given value to JSON and panics if it fails.
func jsonMust(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}
