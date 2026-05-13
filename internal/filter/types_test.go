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
			json:      `{"op":"=","attributes":["foo"],"value":"bar"}`,
			expectErr: false,
			verify: func(t *testing.T, f Filter) {
				c, ok := f.(*Condition)
				require.Truef(t, ok, "expected Condition, got %T", f)
				assert.Equal(t, Equal, c.op)
				assert.Len(t, c.Attributes(), 1)
				assert.Contains(t, c.Attributes(), "foo")
				assert.Equal(t, "bar", c.Value())
			},
		},
		{
			name: "Simple Filter Chain",
			json: `{
  "op": "&",
  "rules": [
    {
      "op": "=",
      "attributes": ["hostgroups[*].name"],
      "regex": "^.*lin.*$"
    },
    {
      "op": "=",
      "attributes": ["host.user.name"],
      "value": "icingaadmin"
    },
    {
      "op": "!=",
      "attributes": ["host.vars['http_vhosts']['http']['http_uri']"],
      "value": "\/"
    }
  ]
}`,
			expectErr: false,
			verify: func(t *testing.T, f Filter) {
				ch, ok := f.(*Chain)
				require.Truef(t, ok, "expected Chain, got %T", f)
				assert.Equal(t, All, ch.op)
				assert.Len(t, ch.rules, 3)

				for _, condition := range f.ExtractConditions() {
					switch condition.op {
					case Like:
						assert.Len(t, condition.Attributes(), 1)
						assert.Contains(t, condition.Attributes(), "hostgroups[*].name")
						assert.Equal(t, "^.*lin.*$", condition.Value())
					case Equal:
						assert.Equal(t, Equal, condition.op)
						assert.Len(t, condition.Attributes(), 1)
						assert.Contains(t, condition.Attributes(), "host.user.name")
						assert.Equal(t, "icingaadmin", condition.Value())
					default:
						assert.Equal(t, UnEqual, condition.op)
						assert.Len(t, condition.Attributes(), 1)
						assert.Contains(t, condition.Attributes(), "host.vars['http_vhosts']['http']['http_uri']")
						assert.Equal(t, "/", condition.Value())
					}
				}
			},
		},
		{
			name:      "Nested Filter Chain",
			json:      `{"op":"&","rules":[{"op":"=","attributes":["x"],"regex":"^v.*$"},{"op":"|","rules":[{"op":"!=","attributes":["y"],"regex":"^some.*$"},{"op":"!=","attributes":["z"],"value":2}]}]}`,
			expectErr: false,
			verify: func(t *testing.T, f Filter) {
				ch, ok := f.(*Chain)
				require.Truef(t, ok, "expected Chain, got %T", f)
				assert.Equal(t, All, ch.op)
				assert.Len(t, ch.rules, 2)

				condition, ok := ch.rules[0].(*Condition)
				require.Truef(t, ok, "expected Condition, got %T", ch.rules[0])
				assert.Equal(t, Like, condition.op)
				assert.Len(t, condition.Attributes(), 1)
				assert.Contains(t, condition.Attributes(), "x")
				assert.Equal(t, "^v.*$", condition.value)

				ch2, ok := ch.rules[1].(*Chain)
				require.Truef(t, ok, "expected Condition, got %T", ch.rules[1])
				assert.Equal(t, Any, ch2.op)
				assert.Len(t, ch2.rules, 2)

				for _, cond := range ch2.ExtractConditions() {
					if cond.op == UnLike {
						assert.Len(t, cond.Attributes(), 1)
						assert.Contains(t, cond.Attributes(), "y")
						assert.Equal(t, "^some.*$", cond.Value())
					} else {
						assert.Equal(t, UnEqual, cond.op)
						require.Len(t, cond.Attributes(), 1)
						assert.Contains(t, cond.Attributes(), "z")
						assert.Equal(t, 2.0, cond.Value())
					}
				}
			},
		},
		{
			name: "Invalid Attributes",
			json: `{"op":"=","attributes":["invalid[", "lol]", "do.something"],"value":1}`,
			verify: func(t *testing.T, f Filter) {
				ch, ok := f.(*Condition)
				require.Truef(t, ok, "expected Condition, got %T", f)
				assert.Equal(t, Equal, ch.op)
				assert.Len(t, ch.Attributes(), 1)
				assert.Contains(t, ch.Attributes(), "do.something")
				assert.Equal(t, 1.0, ch.Value())
			},
		},
		{
			name:      "Regex With Invalid Operator",
			json:      `{"op":"<","attributes":["x"],"regex":"^v.*$"}`,
			expectErr: true,
			errString: "regex field is only supported for equality operators (= and !=), but got operator",
		},
		{
			name:      "Missing Value and Regex",
			json:      `{"op":"=","attributes":["x"]}`,
			expectErr: true,
			errString: "missing required filter condition field: value or regex",
		},
		{
			name:      "Missing Operator",
			json:      `{"attributes":["a"],"value":"1"}`,
			expectErr: true,
			errString: "missing required field: op",
		},
		{
			name:      "Unknown Operator",
			json:      `{"op":"?","attributes":["a"],"value":"1"}`,
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
			name:      "Missing Filter Paths",
			json:      `{"op":"=","value":"1"}`,
			expectErr: true,
			errString: "missing required filter condition field: attributes",
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
			name:      "Invalid JSONPath",
			json:      `{"op":"=","attributes":["invalid[", "lol]"],"value":"1"}`,
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
