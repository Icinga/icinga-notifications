package icinga2

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestRawurlencode(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"printable", "abcABC0123", "abcABC0123"},
		{"space", "foo bar", "foo%20bar"},
		{"plus", "foo+bar", "foo%2Bbar"},
		{"slash", "foo/bar", "foo%2Fbar"},
		{"percent", "foo%bar", "foo%25bar"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, rawurlencode(tt.in))
		})
	}
}
