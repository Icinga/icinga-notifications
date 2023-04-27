package utils

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestRemoveNils(t *testing.T) {
	var a, b, c, d int

	tests := []struct {
		name string
		in   []*int
		want []*int
	}{
		{"Empty", []*int{}, []*int{}},
		{"SingleKeep", []*int{&a}, []*int{&a}},
		{"SingleRemove", []*int{nil}, []*int{}},
		{"KeepOrder", []*int{&a, &b, &c, &d}, []*int{&a, &b, &c, &d}},
		{"Duplicates", []*int{&a, &b, &b}, []*int{&a, &b, &b}},
		{"Mixed", []*int{&a, nil, &b, nil, nil, &b, nil, &d}, []*int{&a, &b, &b, &d}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, RemoveNils(tt.in))
		})
	}
}
