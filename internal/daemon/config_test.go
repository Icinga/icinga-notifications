package daemon

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListenerValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		listener        Listener
		wantAddr        string
		wantSocket      string
		wantSocketMode  Mode
		wantSocketGroup string
		wantErr         bool
	}{
		{
			name:            "NoConnectionConfigured",
			listener:        Listener{},
			wantAddr:        "localhost:5680",
			wantSocket:      "",
			wantSocketMode:  Mode{},
			wantSocketGroup: "",
		},
		{
			name:            "SocketOnly",
			listener:        Listener{Socket: "/tmp/test.sock", SocketMode: Mode{0660, true}},
			wantAddr:        "",
			wantSocket:      "/tmp/test.sock",
			wantSocketMode:  Mode{0660, true},
			wantSocketGroup: "",
		},
		{
			name:            "AddrOnly",
			listener:        Listener{Addr: "localhost:5681"},
			wantAddr:        "localhost:5681",
			wantSocket:      "",
			wantSocketMode:  Mode{},
			wantSocketGroup: "",
		},
		{
			name:            "AddrAndSocket",
			listener:        Listener{Addr: "localhost:5681", Socket: "/tmp/test.sock", SocketMode: Mode{0660, true}},
			wantAddr:        "localhost:5681",
			wantSocket:      "/tmp/test.sock",
			wantSocketMode:  Mode{0660, true},
			wantSocketGroup: "",
		},
		{
			name:            "PermissiveSocketMode",
			listener:        Listener{Socket: "/tmp/test.sock", SocketMode: Mode{0777, true}},
			wantAddr:        "",
			wantSocket:      "/tmp/test.sock",
			wantSocketMode:  Mode{0777, true},
			wantSocketGroup: "",
		},
		{
			name:     "SocketModeExceedsValidBits",
			listener: Listener{Socket: "/tmp/test.sock", SocketMode: Mode{06660, true}},
			wantErr:  true,
		},
		{
			name:     "SocketModeNoReadOrWritePermission",
			listener: Listener{Socket: "/tmp/test.sock", SocketMode: Mode{0100, true}},
			wantErr:  true,
		},
		{
			name:     "SocketGroupNotExists",
			listener: Listener{Socket: "/tmp/test.sock", SocketMode: Mode{0600, true}, SocketGroup: "unknownGroup"},
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
			assert.Equal(t, tc.wantSocket, tc.listener.Socket)
			assert.Equal(t, tc.wantSocketMode, tc.listener.SocketMode)
			assert.Equal(t, tc.wantSocketGroup, tc.listener.SocketGroup)
		})
	}
}
