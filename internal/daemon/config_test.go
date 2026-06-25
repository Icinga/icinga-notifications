package daemon

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListenerValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		listener Listener
		wantAddr string
		wantErr  bool
	}{
		{
			name:     "nothing set defaults TCP address",
			listener: Listener{},
			wantAddr: "localhost:5680",
		},
		{
			name:     "only socket set leaves addr empty",
			listener: Listener{Socket: "/tmp/test.sock", SocketMode: "0660"},
			wantAddr: "",
		},
		{
			name:     "explicit addr preserved",
			listener: Listener{Addr: ":5681"},
			wantAddr: ":5681",
		},
		{
			name:     "both addr and socket set preserves both",
			listener: Listener{Addr: ":5681", Socket: "/tmp/test.sock", SocketMode: "0660"},
			wantAddr: ":5681",
		},
		{
			name:     "socket with permissive mode is valid",
			listener: Listener{Socket: "/tmp/test.sock", SocketMode: "0777"},
			wantAddr: "",
		},
		{
			name:     "socket with non-octal digit in mode",
			listener: Listener{Socket: "/tmp/test.sock", SocketMode: "0890"},
			wantErr:  true,
		},
		{
			name:     "socket with non-numeric mode string",
			listener: Listener{Socket: "/tmp/test.sock", SocketMode: "invalid"},
			wantErr:  true,
		},
		{
			name:     "socket with empty mode string",
			listener: Listener{Socket: "/tmp/test.sock", SocketMode: ""},
			wantErr:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := tc.listener.Validate()

			if tc.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.wantAddr, tc.listener.Addr)
		})
	}
}
