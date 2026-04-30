package event

import (
	"encoding/json"
	"testing"

	baseEv "github.com/icinga/icinga-go-library/notifications/event"
	"github.com/icinga/icinga-notifications/internal/filter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFilter(t *testing.T) {
	t.Parallel()

	ev := &Event{
		Event: baseEv.Event{
			Relations: map[string]any{
				"host": map[string]any{
					"name": "test-host",
					"vars": map[string]any{
						"dict": map[string]any{
							"key":     "value",
							"key_int": 42,
							"key_array": []any{
								"value1",
								"value2",
								map[string]any{
									"dict_in_array": "dict_in_array1",
								},
							},
						},
						"array": []any{
							"value1-from-array",
							map[string]any{
								"dict_in_array": "dict_in_array2",
							},
							map[string]any{
								"dict_in_array": "dict_in_array3",
							},
						},
					},
				},
			},
		},
	}

	filterData := []struct {
		Expr     []byte
		Expected bool
	}{
		// ... expected positive matches
		{Expr: makeJsonFilterExpr(t, "host.name", "=", "test-host"), Expected: true},
		{Expr: makeJsonFilterExpr(t, "host.name", "~", "test*"), Expected: true},
		{Expr: makeJsonFilterExpr(t, "host.vars.dict.key", "=", "value"), Expected: true},
		{Expr: makeJsonFilterExpr(t, "host.vars.dict.key", "!~", "something*"), Expected: true},
		{Expr: makeJsonFilterExpr(t, "host.vars.dict.key_int", ">=", 42), Expected: true},
		{Expr: makeJsonFilterExpr(t, "host.vars.dict.key_int", "=", 42.0), Expected: true},
		{Expr: makeJsonFilterExpr(t, "host.vars.dict.key_array[0]", "!=", "value2"), Expected: true},
		{Expr: makeJsonFilterExpr(t, "host.vars.dict.key_array[1]", "!=", "value1"), Expected: true},
		{Expr: makeJsonFilterExpr(t, "host.vars.dict.key_array[2].dict_in_array", "=", "dict_in_array1"), Expected: true},
		{Expr: makeJsonFilterExpr(t, "host.vars.array[0]", "~", "value1-from*"), Expected: true},
		{Expr: makeJsonFilterExpr(t, "host.vars.array[*].dict_in_array", "~", "dict_in_array*"), Expected: true},
		{Expr: makeJsonFilterExpr(t, "host.vars.dict.key_array[2].dict_in_array", "=", "dict_in_array1"), Expected: true},

		// ... expected negative matches
		{Expr: makeJsonFilterExpr(t, "host.name", "=", "wrong-host"), Expected: false},
		{Expr: makeJsonFilterExpr(t, "host.name", "!=", "test-host"), Expected: false},
		{Expr: makeJsonFilterExpr(t, "host.vars.dict.key", "=", "wrong-value"), Expected: false},
		{Expr: makeJsonFilterExpr(t, "host.missing", "=", "foo"), Expected: false},
		{Expr: makeJsonFilterExpr(t, "host.vars.dict.missing", "=", "foo"), Expected: false},
		{Expr: makeJsonFilterExpr(t, "host.vars.dict.key_array[3]", "=", "value"), Expected: false},
		{Expr: makeJsonFilterExpr(t, "host.vars.dict.key_array[2].missing", "=", "foo"), Expected: false},
		{Expr: makeJsonFilterExpr(t, "host.vars.array[1].dict_in_array", "=", "wrong-value"), Expected: false},
		{Expr: makeJsonFilterExpr(t, "host.vars.dict.key_int", "=", 043), Expected: false},
		{Expr: makeJsonFilterExpr(t, "host.vars.dict.key_int", "<=", 30), Expected: false},
		{Expr: makeJsonFilterExpr(t, "service.name", "~", "whatever"), Expected: false},
		{Expr: makeJsonFilterExpr(t, "host.vars.dict.key_array[2]", "=", "dict_in_array1"), Expected: false},
		{Expr: makeJsonFilterExpr(t, "host.vars.array", "~", "value1-from*"), Expected: false},
	}

	for _, data := range filterData {
		f, err := filter.UnmarshalJSON(data.Expr)
		if assert.NoErrorf(t, err, "parsing %q should not fail", data.Expr) {
			matched, err := f.Eval(ev)
			assert.NoErrorf(t, err, "evaluating %q should not fail", data.Expr)
			assert.Equal(t, data.Expected, matched, "unexpected result for %q", data.Expr)
		}
	}
}

// makeJsonFilterExpr is a helper function to create a JSON filter expression for testing purposes.
func makeJsonFilterExpr(t *testing.T, jsonPath, operator, value any) []byte {
	data, err := json.Marshal(map[string]any{
		"column": jsonPath,
		"op":     operator,
		"value":  value,
	})
	require.NoError(t, err)
	return data
}
