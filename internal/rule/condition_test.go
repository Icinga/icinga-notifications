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
