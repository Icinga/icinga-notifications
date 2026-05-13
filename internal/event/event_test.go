package event

import (
	"encoding/json"
	"testing"

	baseEv "github.com/icinga/icinga-go-library/notifications/event"
	"github.com/icinga/icinga-notifications/internal/filter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEvent(t *testing.T) {
	t.Parallel()

	t.Run("ExtractMissingRelations", func(t *testing.T) {
		t.Parallel()

		ev := &Event{
			Event: baseEv.Event{
				CompleteRelations: []string{"host.vars", "host", "services"},
				Relations: map[string]any{
					"host.vars": map[string]any{
						"os": "Linux",
					},
					"services": []any{
						map[string]any{
							"name": "service",
							"vars": map[string]any{
								"department": "IT",
							},
						},
					},
				},
			},
		}

		filterColumns := [][]string{
			{"host.vars.os", "host.vars.arch", "hostgroups[*].name_ci", "services[*].name_ci"},
			{"host.vars.department", "hostgroups[*].name_ci", "servicegroups[*].name"},
			{"services[*].name", "services[*].name_ci", "services[*].vars.department", "hostgroups[*].name"},
			{"services[*].vars.arch", "services[*].vars.department", "hostgroups[*].name", "hostgroups[*].name_ci"},
		}
		missingRelations := ev.ExtractMissingRelations(filterColumns...)
		require.Lenf(t, missingRelations, 2, "%v", missingRelations)
		assert.Equal(t, "hostgroups[*].name_ci", missingRelations[0])
		assert.Equal(t, "servicegroups[*].name", missingRelations[1])
	})

	t.Run("Filter", func(t *testing.T) {
		t.Parallel()

		ev := &Event{
			Event: baseEv.Event{
				Relations: map[string]any{
					"host": map[string]any{
						"name": "test-host",
						"vars": map[string]any{
							"dict": map[string]any{
								"key":       "value",
								"key_int":   42,
								"key_float": 3.1415,
								"domain":    "example.com",
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
			{Expr: makeJsonFilterExpr(t, []string{"host.name", "host.something"}, "=", "test-host", false), Expected: true},
			{Expr: makeJsonFilterExpr(t, []string{"foo.bar", "host.name"}, "=", "^test.*$", true), Expected: true},
			{Expr: makeJsonFilterExpr(t, []string{"bar.foo", "host.name"}, "=", "^.*host$", true), Expected: true},
			{Expr: makeJsonFilterExpr(t, []string{"something.wrong", "host.name"}, "=", "^test.*host$", true), Expected: true},
			{Expr: makeJsonFilterExpr(t, []string{"host.vars.dict.key", "invalid["}, "=", "^value$", true), Expected: true},
			{Expr: makeJsonFilterExpr(t, []string{"host.vars.dict.key"}, "!=", "^something.*$", true), Expected: true},
			{Expr: makeJsonFilterExpr(t, []string{"host.vars.dict.key_int"}, ">=", 42, false), Expected: true},
			{Expr: makeJsonFilterExpr(t, []string{"host.vars.dict.key_int"}, "<=", 42, false), Expected: true},
			{Expr: makeJsonFilterExpr(t, []string{"host.vars.dict.key_int"}, ">=", 5, false), Expected: true},
			{Expr: makeJsonFilterExpr(t, []string{"host.vars.dict.key_int"}, "=", 42.0, false), Expected: true},
			{Expr: makeJsonFilterExpr(t, []string{"host.vars.dict.key_float"}, "<", 3.17, false), Expected: true},
			{Expr: makeJsonFilterExpr(t, []string{"host.vars.dict.domain"}, ">", "foo.com", false), Expected: true},
			{Expr: makeJsonFilterExpr(t, []string{"host.vars.dict.key_array[0]"}, "!=", "value2", false), Expected: true},
			{Expr: makeJsonFilterExpr(t, []string{"host.vars.dict.key_array[1]"}, "!=", "value1", false), Expected: true},
			{Expr: makeJsonFilterExpr(t, []string{"host.vars.dict.key_array[2].dict_in_array"}, "=", "dict_in_array1", false), Expected: true},
			{Expr: makeJsonFilterExpr(t, []string{"host.vars.array[0]"}, "=", "^value1-from.*$", true), Expected: true},
			{Expr: makeJsonFilterExpr(t, []string{"host.vars.array[*].dict_in_array"}, "=", "^dict_in_array.*$", true), Expected: true},
			{Expr: makeJsonFilterExpr(t, []string{"host.vars.dict.key_array[2].dict_in_array"}, "=", "dict_in_array1", false), Expected: true},

			// ... expected negative matches
			{Expr: makeJsonFilterExpr(t, []string{"host.name"}, "=", "wrong-host", false), Expected: false},
			{Expr: makeJsonFilterExpr(t, []string{"host.name"}, "!=", "test-host", false), Expected: false},
			{Expr: makeJsonFilterExpr(t, []string{"host.vars.dict.key"}, "=", "wrong-value", false), Expected: false},
			{Expr: makeJsonFilterExpr(t, []string{"host.missing"}, "=", "foo", false), Expected: false},
			{Expr: makeJsonFilterExpr(t, []string{"host.vars.dict.missing"}, "=", "foo", false), Expected: false},
			{Expr: makeJsonFilterExpr(t, []string{"host.vars.dict.key_array[3]"}, "=", "value", false), Expected: false},
			{Expr: makeJsonFilterExpr(t, []string{"host.vars.dict.key_array[2].missing"}, "=", "foo", false), Expected: false},
			{Expr: makeJsonFilterExpr(t, []string{"host.vars.array[1].dict_in_array"}, "=", "wrong-value", false), Expected: false},
			{Expr: makeJsonFilterExpr(t, []string{"host.vars.dict.key_int"}, "=", 043, false), Expected: false},
			{Expr: makeJsonFilterExpr(t, []string{"host.vars.dict.key_int"}, "<=", 30, false), Expected: false},
			{Expr: makeJsonFilterExpr(t, []string{"host.vars.dict.key_int"}, "<", 5, false), Expected: false},
			{Expr: makeJsonFilterExpr(t, []string{"host.vars.dict.key_float"}, ">", 90, false), Expected: false},
			{Expr: makeJsonFilterExpr(t, []string{"host.vars.dict.domain"}, ">", "notifications.devlab.com", false), Expected: false},
			{Expr: makeJsonFilterExpr(t, []string{"service.name"}, "=", "^whatever.*$", true), Expected: false},
			{Expr: makeJsonFilterExpr(t, []string{"host.name"}, "!=", "^.*host$", true), Expected: false},
			{Expr: makeJsonFilterExpr(t, []string{"host.name"}, "!=", "^test.*host$", true), Expected: false},
			{Expr: makeJsonFilterExpr(t, []string{"host.vars.dict.key_array[2]"}, "=", "dict_in_array1", false), Expected: false},
			{Expr: makeJsonFilterExpr(t, []string{"host.vars.array"}, "=", "^value1-from.*$", true), Expected: false},
			{Expr: makeJsonFilterExpr(t, []string{"host.vars.dict"}, "=", "^foo.*bar$", true), Expected: false},
		}

		for _, data := range filterData {
			f, err := filter.UnmarshalJSON(data.Expr)
			if assert.NoErrorf(t, err, "parsing %q should not fail", data.Expr) {
				matched, err := f.Eval(ev)
				assert.NoErrorf(t, err, "evaluating %q should not fail", data.Expr)
				assert.Equal(t, data.Expected, matched, "unexpected result for %q", data.Expr)
			}
		}
	})
}

// makeJsonFilterExpr is a helper function to create a JSON filter expression for testing purposes.
func makeJsonFilterExpr(t *testing.T, jsonPath, operator, value any, isRegex bool) []byte {
	data := map[string]any{"attributes": jsonPath, "op": operator}
	if isRegex {
		data["regex"] = value
	} else {
		data["value"] = value
	}
	dataBytes, err := json.Marshal(data)
	require.NoError(t, err)
	return dataBytes
}
