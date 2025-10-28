package plugin

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestBool_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		out     Bool
		wantErr bool
	}{
		{"bool-true", `true`, true, false},
		{"bool-false", `false`, false, false},
		{"string-true", `"y"`, true, false},
		{"string-false", `"n"`, false, false},
		{"string-invalid", `"NEIN"`, false, true},
		{"invalid-type", `23`, false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out Bool
			if err := out.UnmarshalJSON([]byte(tt.in)); tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.out, out)
			}
		})
	}
}
