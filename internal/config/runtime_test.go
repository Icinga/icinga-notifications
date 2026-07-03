package config

import (
	"testing"

	"github.com/icinga/icinga-go-library/types"
	"github.com/stretchr/testify/assert"
)

func TestGetSourceByUsername(t *testing.T) {
	t.Parallel()

	srcA := &Source{ListenerUsername: types.MakeString("icingadb")}
	srcB := &Source{ListenerUsername: types.MakeString("icinga2")}
	srcNoUser := &Source{} // ListenerUsername.Valid == false

	rc := &RuntimeConfig{}
	rc.Sources = map[int64]*Source{
		1: srcA,
		2: srcB,
		3: srcNoUser,
	}

	tests := []struct {
		name     string
		username string
		want     *Source
	}{
		{
			name:     "KnownUsername1",
			username: "icingadb",
			want:     srcA,
		},
		{
			name:     "KnownUsername2",
			username: "icinga2",
			want:     srcB,
		},
		{
			name:     "UnknownUsername",
			username: "unknown",
			want:     nil,
		},
		{
			name:     "EmptyUsername",
			username: "",
			want:     nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Same(t, tc.want, rc.GetSourceByUsername(tc.username))
		})
	}
}
