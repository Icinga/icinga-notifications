package object

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestID(t *testing.T) {
	t.Parallel()

	t.Run("Stable", func(t *testing.T) {
		t.Parallel()

		tags1 := map[string]string{
			"foo": "bar",
			"baz": "qux",
		}
		tags2 := map[string]string{ // same as tags1 but with different order.
			"baz": "qux",
			"foo": "bar",
		}

		tags3 := map[string]string{
			"some":      "thing",
			"different": "value",
		}

		id1 := ID(tags1).String()
		id2 := ID(tags1).String()
		require.Equal(t, id1, id2)
		require.Len(t, id1, 64)

		// Just some sanity/static checks to ensure it is stable and consistent.
		require.Equal(t, "4bfe8b2596005172c9db4d2b4b400a12b478b87a793ed9577e9d2d165fd07e7a", ID(tags1).String())
		require.Equal(t, "4bfe8b2596005172c9db4d2b4b400a12b478b87a793ed9577e9d2d165fd07e7a", ID(tags2).String())
		require.Equal(t, "d42b5e17d0868536dcf004e2aac9b8478a632e03cb666eb0dc96c3a3f608dcfa", ID(tags3).String())
	})

	t.Run("Nil and Empty", func(t *testing.T) {
		t.Parallel()

		require.Len(t, ID(nil).String(), 64)
		require.Len(t, ID(map[string]string{}).String(), 64)
		require.Equal(t, ID(nil).String(), ID(map[string]string{}).String())
	})

	t.Run("Empty Key and Value", func(t *testing.T) {
		t.Parallel()

		emptyKey := map[string]string{"": "value"}
		emptyValue := map[string]string{"key": ""}

		// These should not equal a plain empty map's ID.
		require.NotEqual(t, ID(map[string]string{}).String(), ID(emptyKey).String())
		require.NotEqual(t, ID(map[string]string{}).String(), ID(emptyValue).String())

		sameKeyNonEmptyValue := map[string]string{"key": "something"}
		require.NotEqual(t, ID(emptyValue).String(), ID(sameKeyNonEmptyValue).String())
	})

	t.Run("Unicode Chars", func(t *testing.T) {
		t.Parallel()

		unicode := map[string]string{"z": "☃"}
		unicode2 := map[string]string{"z": "☃"}

		require.Equal(t, ID(unicode).String(), ID(unicode2).String())
		require.Len(t, ID(unicode).String(), 64)

		long := map[string]string{"k": strings.Repeat("☃", 128)}
		require.Len(t, ID(long).String(), 64)
	})

	t.Run("Large Map", func(t *testing.T) {
		t.Parallel()

		asc := make(map[string]string, 128)  // filled in ascending order.
		desc := make(map[string]string, 128) // filled in descending order.

		for i := range 128 {
			asc[fmt.Sprintf("key-%d", i)] = fmt.Sprintf("val-%d", i)
		}

		for i := 128 - 1; i >= 0; i-- {
			desc[fmt.Sprintf("key-%d", i)] = fmt.Sprintf("val-%d", i)
		}

		require.Equal(t, ID(asc).String(), ID(desc).String())
		require.Len(t, ID(asc).String(), 64)
	})
}
