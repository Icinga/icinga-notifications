package config

import (
	"testing"
	"time"

	"github.com/icinga/icinga-go-library/logging"
	"github.com/icinga/icinga-go-library/types"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap/zaptest"
)

func TestGetSourceFromUsername(t *testing.T) {
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
			name:     "known username returns correct source",
			username: "icingadb",
			want:     srcA,
		},
		{
			name:     "second known username returns correct source",
			username: "icinga2",
			want:     srcB,
		},
		{
			name:     "unknown username returns nil",
			username: "unknown",
			want:     nil,
		},
		{
			name:     "source without username configured is not matched by empty string",
			username: "",
			want:     nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			logger := logging.NewLogger(zaptest.NewLogger(t).Sugar(), time.Hour)
			got := rc.GetSourceFromUsername(tc.username, logger)
			assert.Same(t, tc.want, got)
		})
	}
}
