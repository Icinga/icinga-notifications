package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

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
	s.UseInvalidStateV = tmp.UseInvalidStateV
	s.Success = tmp.Success
	s.mu.Unlock()

	return nil
}

func (s *Sleep) SendNotification(nr *plugin.NotificationRequest) (*plugin.DeliveryResult, error) {
	s.mu.Lock()
	duration := s.duration
	persistState := s.PersistState
	spamStderr := s.SpamStderr
	useInvalidStateV := s.UseInvalidStateV
	success := s.Success
	s.mu.Unlock()

	if spamStderr {
		// This stress tests the stderr handler on the Icinga Notifications side.
		slog.Info("Sending huge message to stderr", "message", strings.Repeat("X", 10*1024*1024)) // 10 MB message
		slog.Info("Sending huge message to stderr", "message", strings.Repeat("A", 10*1024*1024)) // 10 MB message
	}

	select {
	case <-s.rpcCtx.Done():
		return nil, fmt.Errorf("plugin context canceled: %w", s.rpcCtx.Err())
	case <-time.After(duration):
		result := &plugin.DeliveryResult{
			Details: map[string]string{
				"message": fmt.Sprintf("Slept for %s", duration),
			},
		}

		if persistState && !nr.State.IsZero() && !nr.Incident.IsRecovered {
			result.State.Key = nr.State.Key
			if useInvalidStateV {
				// This exceeds the Unicode character limit of 4096 by two characters, which should trigger an error on
				// the Icinga Notifications side. Using builtin len() function would have reported 4098*4 bytes instead.
				result.State.Value = strings.Repeat("💤", 4098)
			} else {
				result.State.Value = strings.Repeat("💤", 4096) // max len of 4096 Unicode characters
			}
		}

		if success {
			return result, nil
		}

		return nil, fmt.Errorf("plugin slept for %s", duration)
	}
}
