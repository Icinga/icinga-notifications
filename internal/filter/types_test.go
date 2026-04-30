package filter

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUnmarshalJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		json      string
		expectErr bool
		errString string
		verify    func(t *testing.T, f Filter)
	}{
		{
			name:      "Null JSON",
			json:      "null",
			expectErr: false,
			verify:    func(t *testing.T, f Filter) { assert.Nil(t, f) },
		},
		{
			name:      "Filter Condition",
			json:      `{"op":"=","column":"foo","value":"bar"}`,
			expectErr: false,
			verify: func(t *testing.T, f Filter) {
				c, ok := f.(*Condition)
				require.Truef(t, ok, "expected Condition, got %T", f)
				assert.Equal(t, Equal, c.op)
				assert.Equal(t, "foo", c.column)
				assert.Equal(t, "bar", c.value)
			},
		},
		{
			name:      "Simple Filter Chain",
			json:      `{"op":"&","rules":[{"op":"=","column":"a","value":"1"},{"op":"!=","column":"b","value":2}]}`,
			expectErr: false,
			verify: func(t *testing.T, f Filter) {
				ch, ok := f.(*Chain)
				require.Truef(t, ok, "expected Chain, got %T", f)
				assert.Equal(t, All, ch.op)
				assert.Len(t, ch.rules, 2)

				c1, ok := ch.rules[0].(*Condition)
				require.Truef(t, ok, "expected Condition, got %T", ch.rules[0])
				assert.Equal(t, Equal, c1.op)
				assert.Equal(t, "a", c1.column)
				assert.Equal(t, "1", c1.value)

				c2, ok := ch.rules[1].(*Condition)
				require.Truef(t, ok, "expected Condition, got %T", ch.rules[1])
				assert.Equal(t, UnEqual, c2.op)
				assert.Equal(t, "b", c2.column)
				assert.Equal(t, "2", c2.value)
			},
		},
		{
			name:      "Nested Filter Chain",
			json:      `{"op":"&","rules":[{"op":"~","column":"x","value":"v*"},{"op":"|","rules":[{"op":"!~","column":"y","value":"some*"},{"op":"!=","column":"z","value":"2"}]}]}`,
			expectErr: false,
			verify: func(t *testing.T, f Filter) {
				ch, ok := f.(*Chain)
				require.Truef(t, ok, "expected Chain, got %T", f)
				assert.Equal(t, All, ch.op)
				assert.Len(t, ch.rules, 2)

				condition, ok := ch.rules[0].(*Condition)
				require.Truef(t, ok, "expected Condition, got %T", ch.rules[0])
				assert.Equal(t, Like, condition.op)
				assert.Equal(t, "x", condition.column)
				assert.Equal(t, "v*", condition.value)

				ch2, ok := ch.rules[1].(*Chain)
				require.Truef(t, ok, "expected Condition, got %T", ch.rules[1])
				assert.Equal(t, Any, ch2.op)
				assert.Len(t, ch2.rules, 2)

				c1, ok := ch2.rules[0].(*Condition)
				require.Truef(t, ok, "expected Condition, got %T", ch2.rules[0])
				assert.Equal(t, UnLike, c1.op)
				assert.Equal(t, "y", c1.column)
				assert.Equal(t, "some*", c1.value)

				c2, ok := ch2.rules[1].(*Condition)
				require.Truef(t, ok, "expected Condition, got %T", ch2.rules[1])
				assert.Equal(t, UnEqual, c2.op)
				assert.Equal(t, "z", c2.column)
				assert.Equal(t, "2", c2.value)
			},
		},
		{
			name:      "Missing Operator",
			json:      `{"column":"a","value":"1"}`,
			expectErr: true,
			errString: "missing required field: op",
		},
		{
			name:      "Unknown Operator",
			json:      `{"op":"?","column":"a","value":"1"}`,
			expectErr: true,
			errString: "unknown filter operator",
		},
		{
			name:      "Missing Chain Rules",
			json:      `{"op":"&"}`,
			expectErr: true,
			errString: "missing required field: rules",
		},
		{
			name:      "Missing Filter Column",
			json:      `{"op":"=","value":"1"}`,
			expectErr: true,
			errString: "missing required filter condition field: column",
		},
		{
			name:      "Rules Not an Array",
			json:      `{"op":"!","rules":"notarray"}`,
			expectErr: true,
			// error message from json.Unmarshal when trying to unmarshal a string into a []json.RawMessage
			errString: "cannot unmarshal string into Go value of type",
		},
		{
			name:      "Invalid JSON",
			json:      `not a json`,
			expectErr: true,
			errString: "invalid character",
		},
		{
			name:      "Invalid JSONPath Column",
			json:      `{"op":"=","column":"invalid[","value":"1"}`,
			expectErr: true,
			errString: "unexpected eof",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f, err := UnmarshalJSON([]byte(tc.json))
			if tc.expectErr {
				assert.Errorf(t, err, "expected error but got nil; filter=%#v", f)
				assert.Nil(t, f)
				if tc.errString != "" {
					assert.ErrorContainsf(t, err, tc.errString, "error mismatch: want contains %q, got %q", tc.errString, err.Error())
				}
				return
			}
			require.NoErrorf(t, err, "unexpected error: %v", err)
			if tc.verify != nil {
				tc.verify(t, f)
			}
		})
	}
}
