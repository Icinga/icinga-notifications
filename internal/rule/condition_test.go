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
			{expr: "is_managed=y", managed: true, unmanaged: false},
			{expr: "is_managed=n", managed: false, unmanaged: true},
			{expr: "is_managed!=y", managed: false, unmanaged: true},
			{expr: "is_managed!=n", managed: true, unmanaged: false},
			{expr: "incident_age>=1h&is_managed=y", managed: true, unmanaged: false},
			{expr: "incident_age>=3h&is_managed=y", managed: false, unmanaged: false},
			{expr: "incident_age>=3h|is_managed=n", managed: false, unmanaged: true},
			{expr: "!(is_managed=y)", managed: false, unmanaged: true},
			// Everything but "y" and "n" is not a valid boolean value.
			{expr: "is_managed=yes", expectedErr: true},
			{expr: "is_managed=1", expectedErr: true},
			// Ordering booleans doesn't make any sense.
			{expr: "is_managed>y", expectedErr: true},
			{expr: "is_managed<=n", expectedErr: true},
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
					f, err := filter.Parse(tt.expr)
					require.NoError(t, err, "escalation condition should be parsable")

					matched, err := f.Eval(tc.ctx)
					if tt.expectedErr {
						assert.Error(t, err, "%s incident: evaluation should fail", tc.name)
					} else {
						assert.NoError(t, err, "%s incident: evaluation should succeed", tc.name)
						if tc.name == "Managed Ctx" {
							assert.Equal(t, tt.managed, matched, "%s incident: unexpected evaluation result", tc.name)
						} else {
							assert.Equal(t, tt.unmanaged, matched, "%s incident: unexpected evaluation result", tc.name)
						}
					}
				}
			})
		}

		// A bare "is_managed" only tests whether the escalation filter knows that attribute at all.
		assert.True(t, unmanaged.EvalExists("is_managed"))
		assert.False(t, unmanaged.EvalExists("is_muted"))
	})
}

func TestNotificationFilter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		expr        string
		expected    map[string]bool
		expectedErr string
	}{
		{
			name:     "equal",
			expr:     "event_type=valid_type",
			expected: map[string]bool{"valid_type": true, "invalid_type": false},
		},
		{
			name:     "equal OR combined",
			expr:     "event_type=valid_type||event_type=valid_type_2",
			expected: map[string]bool{"valid_type": true, "valid_type_2": true, "invalid_type": false},
		},
		{
			name:     "unequal AND combined",
			expr:     "event_type!=invalid_type&event_type!=invalid_type_2",
			expected: map[string]bool{"valid_type": true, "invalid_type": false, "invalid_type_2": false},
		},
		{
			name:     "empty condition",
			expr:     "",
			expected: map[string]bool{"valid_type": true, "valid_type_2": true},
		},
		{
			name:     "impossible",
			expr:     "event_type=invalid_type&event_type!=invalid_type",
			expected: map[string]bool{"invalid_type": false, "invalid_type_2": false},
		},
		{
			name:        "less matches",
			expr:        "event_type<invalid_type",
			expected:    map[string]bool{"invalid_type": false},
			expectedErr: "notification filter does not support 'less' matches",
		},
		{
			name:        "lessOrEqual matches",
			expr:        "event_type<=invalid_type",
			expected:    map[string]bool{"invalid_type": false},
			expectedErr: "notification filter does not support 'less or equal' matches",
		},
		{
			name:        "greater or equal matches",
			expr:        "event_type>=invalid_type",
			expected:    map[string]bool{"invalid_type": false},
			expectedErr: "notification filter does not support 'less' matches",
		},
		{
			name:        "greater matches",
			expr:        "event_type>invalid_type",
			expected:    map[string]bool{"invalid_type": false},
			expectedErr: "notification filter does not support 'less or equal' matches",
		},
		{
			name:        "like matches",
			expr:        "event_type=valid_type*",
			expected:    map[string]bool{"invalid_type": false},
			expectedErr: "notification filter does not support wildcard matches",
		},
		{
			name:        "unlike matches",
			expr:        "event_type!=valid_type*",
			expected:    map[string]bool{"invalid_type": false},
			expectedErr: "notification filter does not support wildcard matches",
		},
	}

	for _, tt := range tests {
		f, err := filter.Parse(tt.expr)
		require.NoError(t, err, "escalation condition should be parsable")
		for eventType, expected := range tt.expected {
			nf := &NotificationFilter{EventType: eventType}
			matched, err := f.Eval(nf)
			if err != nil {
				assert.ErrorContains(t, err, tt.expectedErr)
			}
			assert.Equal(t, expected, matched)
		}
	}
}
