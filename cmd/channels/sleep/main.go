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
		DurationString   string `json:"duration"`
		PersistState     bool   `json:"persist_state"`
		SpamStderr       bool   `json:"spam_stderr"`
		UseInvalidStateK bool   `json:"use_invalid_state_key"`
		UseInvalidStateV bool   `json:"use_invalid_state_value"`
		Success          bool   `json:"success"`

		duration time.Duration
		mu       sync.Mutex

		rpcCtx context.Context
		rpcEp  *jsonrpc.Endpoint
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
			},
			{
				Name: "persist_state",
				Type: "bool",
				Label: map[string]string{
					"en_US": "Persist State",
					"de_DE": "Status speichern",
				},
			},
			{
				Name: "spam_stderr",
				Type: "bool",
				Label: map[string]string{
					"en_US": "Spam Stderr",
					"de_DE": "Spam Stderr",
				},
			},
			{
				Name: "use_invalid_state_key",
				Type: "bool",
				Label: map[string]string{
					"en_US": "Send Invalid State Key",
					"de_DE": "Ungültigen Statusschlüssel senden",
				},
			},
			{
				Name: "use_invalid_state_value",
				Type: "bool",
				Label: map[string]string{
					"en_US": "Send Invalid State Value",
					"de_DE": "Ungültigen Statuswert senden",
				},
			},
			{
				Name: "success",
				Type: "bool",
				Label: map[string]string{
					"en_US": "Return Always Success",
					"de_DE": "Immer Erfolg zurückgeben",
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
	s.UseInvalidStateK = tmp.UseInvalidStateK
	s.UseInvalidStateV = tmp.UseInvalidStateV
	s.mu.Unlock()

	return nil
}

func (s *Sleep) SendNotification(nr *plugin.NotificationRequest) error {
	s.mu.Lock()
	duration := s.duration
	persistState := s.PersistState
	spamStderr := s.SpamStderr
	useInvalidStateK := s.UseInvalidStateK
	useInvalidStateV := s.UseInvalidStateV
	success := s.Success
	s.mu.Unlock()

	if success {
		return nil
	}

	if persistState && nr.Incident != nil {
		var key string
		if useInvalidStateK {
			key = strings.Repeat("X", 256) // exceeds the max len of 255
		} else {
			key = composeKey(nr)
		}
		if nr.Incident.IsRecovered {
			if err := s.rpcEp.Call(s.rpcCtx, notifications.MethodDeleteState, []plugin.State{{Key: key}}, nil); err != nil {
				return err
			}
		} else {
			state := plugin.State{Key: key}
			if useInvalidStateV {
				// This exceeds the Unicode character limit of 4096 by two characters, which should trigger an error on
				// the Icinga Notifications side. Using builtin len() function would have reported 4098*4 bytes instead.
				state.Value = strings.Repeat("💤", 4098)
			} else {
				state.Value = strings.Repeat("💤", 4096) // max len of 4096 Unicode characters
			}
			if err := s.rpcEp.Call(s.rpcCtx, notifications.MethodUpsertState, []plugin.State{state}, nil); err != nil {
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
