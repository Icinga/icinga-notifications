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
			listener: Listener{Socket: "/tmp/test.sock", SocketMode: Mode{0660, true}},
			wantAddr: "",
		},
		{
			name:     "explicit addr preserved",
			listener: Listener{Addr: ":5681"},
			wantAddr: ":5681",
		},
		{
			name:     "both addr and socket set preserves both",
			listener: Listener{Addr: ":5681", Socket: "/tmp/test.sock", SocketMode: Mode{0660, true}},
			wantAddr: ":5681",
		},
		{
			name:     "socket with permissive mode is valid",
			listener: Listener{Socket: "/tmp/test.sock", SocketMode: Mode{0777, true}},
			wantAddr: "",
		},
		{
			name:     "socket mode exceeds valid unix permission bits",
			listener: Listener{Socket: "/tmp/test.sock", SocketMode: Mode{06660, true}},
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
