package rule

import (
	"testing"
	"time"

	"github.com/icinga/icinga-go-library/notifications/event"
	"github.com/icinga/icinga-notifications/internal/filter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEscalationFilter(t *testing.T) {
	t.Parallel()

	t.Run("ReevaluateAfter", func(t *testing.T) {
		t.Parallel()

		incident := &EscalationFilter{IncidentAge: 2 * time.Hour, IncidentSeverity: event.SeverityCrit}

		tests := []struct {
			expr     string
			expected time.Duration
		}{
			{expr: `{"op":">","attributes":["incident_age"],"value":"1h"}`, expected: RetryNever},
			{expr: `{"op":">","attributes":["incident_age"],"value":"2h"}`, expected: RetryNever},
			{expr: `{"op":">","attributes":["incident_age"],"value":"3h"}`, expected: 1 * time.Hour},
			{expr: `{"op":"&","rules":[{"op":">","attributes":["incident_age"],"value":"3h"},{"op":"=","attributes":["incident_severity"],"value":"crit"}]}`, expected: 1 * time.Hour},
			{expr: `{"op":"&","rules":[{"op":">","attributes":["incident_age"],"value":"3h"},{"op":"=","attributes":["incident_severity"],"value":"warn"}]}`, expected: 1 * time.Hour},
			{expr: `{"op":"|","rules":[{"op":">","attributes":["incident_age"],"value":"3h"},{"op":"=","attributes":["incident_severity"],"value":"crit"}]}`, expected: 1 * time.Hour},
			{expr: `{"op":"|","rules":[{"op":">","attributes":["incident_age"],"value":"3h"},{"op":"=","attributes":["incident_severity"],"value":"warn"}]}`, expected: 1 * time.Hour},
		}

		for _, tt := range tests {
			f, err := filter.UnmarshalJSON([]byte(tt.expr))
			require.NoError(t, err, "failed to unmarshal filter expression: %v", err)
			assert.Equal(t, tt.expected, incident.ReevaluateAfter(f), "unexpected reevaluation duration for expression: %s", tt.expr)
		}
	})

	t.Run("IsManaged", func(t *testing.T) {
		t.Parallel()

		managed := &EscalationFilter{IncidentAge: 2 * time.Hour, IncidentSeverity: event.SeverityCrit, IsManaged: true}
		unmanaged := &EscalationFilter{IncidentAge: 2 * time.Hour, IncidentSeverity: event.SeverityCrit}

		tests := []struct {
			expr        string
			managed     bool // expected result for the managed incident
			unmanaged   bool // expected result for the unmanaged incident
			expectedErr bool
		}{
			{expr: `{"op":"=","attributes":["is_managed"],"value":"y"}`, managed: true, unmanaged: false},
			{expr: `{"op":"=","attributes":["is_managed"],"value":"n"}`, managed: false, unmanaged: true},
			{expr: `{"op":"!=","attributes":["is_managed"],"value":"y"}`, managed: false, unmanaged: true},
			{expr: `{"op":"!=","attributes":["is_managed"],"value":"n"}`, managed: true, unmanaged: false},
			{expr: `{"op":"&","rules":[{"op":">=","attributes":["incident_age"],"value":"1h"},{"op":"=","attributes":["is_managed"],"value":"y"}]}`, managed: true, unmanaged: false},
			{expr: `{"op":"&","rules":[{"op":">=","attributes":["incident_age"],"value":"3h"},{"op":"=","attributes":["is_managed"],"value":"y"}]}`, managed: false, unmanaged: false},
			{expr: `{"op":"|","rules":[{"op":">=","attributes":["incident_age"],"value":"3h"},{"op":"=","attributes":["is_managed"],"value":"n"}]}`, managed: false, unmanaged: true},
			{expr: `{"op":"!","rules":[{"op":"=","attributes":["is_managed"],"value":"y"}]}`, managed: false, unmanaged: true},
			// Everything but "y" and "n" is not a valid boolean value.
			{expr: `{"op":"=","attributes":["is_managed"],"value":"yes"}`, expectedErr: true},
			{expr: `{"op":"=","attributes":["is_managed"],"value":1}`, expectedErr: true},
			// Ordering booleans doesn't make any sense.
			{expr: `{"op":">","attributes":["is_managed"],"value":"y"}`, expectedErr: true},
			{expr: `{"op":"<=","attributes":["is_managed"],"value":"n"}`, expectedErr: true},
		}

		testCases := []struct {
			name string
			ctx  *EscalationFilter
		}{
			{"Managed Ctx", managed},
			{"Unmanaged Ctx", unmanaged},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				for _, tt := range tests {
					f, err := filter.UnmarshalJSON([]byte(tt.expr))
					require.NoError(t, err, "failed to unmarshal filter expression: %v", err)

					matched, err := f.Eval(tc.ctx)
					if tt.expectedErr {
						assert.Error(t, err, "filter evaluation should have failed for expression: %s", tt.expr)
					} else {
						assert.NoError(t, err, "filter evaluation failed for expression: %s", tt.expr)
						if tc.name == "Managed Ctx" {
							assert.Equal(t, tt.managed, matched, "filter evaluation result mismatch for managed incident: %s", tt.expr)
						} else {
							assert.Equal(t, tt.unmanaged, matched, "filter evaluation result mismatch for unmanaged incident: %s", tt.expr)
						}
					}
				}
			})
		}

		// A bare "is_managed" only tests whether the escalation filter knows that attribute at all.
		assert.True(t, unmanaged.EvalExists([]string{"is_managed"}))
		assert.False(t, unmanaged.EvalExists([]string{"is_muted"}))
	})
}
