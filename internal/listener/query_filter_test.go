package listener

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQueryFilter(t *testing.T) {
	t.Parallel()

	t.Run("Parse", func(t *testing.T) {
		t.Parallel()

		qs := `{"key1":"value1","key2":null}`

		filter, err := ParseQueryFilter(qs)
		require.NoError(t, err)
		assert.Equal(t, map[string]any{"key1": "value1", "key2": nil}, filter)
	})

	t.Run("Evaluate", func(t *testing.T) {
		t.Parallel()

		qs := `{"key1":"value1","key2":null}`
		filter, err := ParseQueryFilter(qs)
		require.NoError(t, err)

		filterable := map[string]string{"key1": "value1"}
		matched, err := EvaluateQueryFilter(filter, filterable)
		require.NoError(t, err)
		assert.True(t, matched)

		filterable = map[string]string{"key1": "value1", "key2": "value2"}
		matched, err = EvaluateQueryFilter(filter, filterable)
		require.NoError(t, err)
		assert.False(t, matched)

		qs = `[{"key1":"value1","key2":"value2"}, {"key3":"value3"}]`
		filter, err = ParseQueryFilter(qs)
		require.NoError(t, err)

		filterable = map[string]string{"key1": "value1", "key2": "value2"}
		matched, err = EvaluateQueryFilter(filter, filterable)
		require.NoError(t, err)
		assert.True(t, matched)

		filterable = map[string]string{"key3": "value3"}
		matched, err = EvaluateQueryFilter(filter, filterable)
		require.NoError(t, err)
		assert.True(t, matched)

		filterable = map[string]string{"key4": "value4"}
		matched, err = EvaluateQueryFilter(filter, filterable)
		require.NoError(t, err)
		assert.False(t, matched)

		qss := []string{
			`"lol"`,
			`{"key1": 400.0}`,
			`{"key1": true}`,
			`{"key1": [{"key2": "value2"}, {"key3": "value3"}]}`,
			`["lol"]`,
			`[["something"]]`,
		}
		for _, qs := range qss {
			filter, err = ParseQueryFilter(qs)
			require.NoError(t, err)

			matched, err = EvaluateQueryFilter(filter, filterable)
			require.ErrorContains(t, err, "invalid JSON filter type")
			assert.False(t, matched)
		}

		qss = []string{
			`{"key1": null}`,
			`{"key1": null, "key2": null, "key3": null}`,
			`[{"key1": null}, {"key2": null}]`,
		}
		for _, qs := range qss {
			filter, err = ParseQueryFilter(qs)
			require.NoError(t, err)

			matched, err = EvaluateQueryFilter(filter, filterable)
			require.ErrorContains(t, err, "'NOT EXISTS' condition must be combined with at least one positive condition")
			assert.False(t, matched)
		}

		qs = `{"key1": null, "key2": null, "key3": null, "key4": "null"}`
		filter, err = ParseQueryFilter(qs)
		require.NoError(t, err)

		matched, err = EvaluateQueryFilter(filter, filterable)
		require.NoError(t, err)
		assert.False(t, matched)
	})
}
