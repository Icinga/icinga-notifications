package utils

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestIterateOrderedMap(t *testing.T) {
	tests := []struct {
		name    string
		in      map[int]string
		outKeys []int
	}{
		{"empty", map[int]string{}, nil},
		{"single", map[int]string{1: "foo"}, []int{1}},
		{"few-numbers", map[int]string{1: "a", 2: "b", 3: "c"}, []int{1, 2, 3}},
		{
			"1k-numbers",
			func() map[int]string {
				m := make(map[int]string)
				for i := 0; i < 1000; i++ {
					m[i] = "foo"
				}
				return m
			}(),
			func() []int {
				keys := make([]int, 1000)
				for i := 0; i < 1000; i++ {
					keys[i] = i
				}
				return keys
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var outKeys []int
			for k, v := range IterateOrderedMap(tt.in) {
				assert.Equal(t, tt.in[k], v)
				outKeys = append(outKeys, k)
			}

			assert.Equal(t, tt.outKeys, outKeys)
		})
	}
}
