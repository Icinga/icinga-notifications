package listener

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStreamJsonResults(t *testing.T) {
	t.Parallel()

	t.Run("No OnError", func(t *testing.T) {
		t.Parallel()

		rw := httptest.NewRecorder()
		outCh, errCh := yieldData(t.Context())
		err := StreamJsonResults(t.Context(), rw, outCh, errCh, WithOnResult(func(i int) (any, error) { return i, nil }))
		require.Error(t, err)
		assert.ErrorContains(t, err, "onError and onResult callbacks must be provided")
	})

	t.Run("No OnResult", func(t *testing.T) {
		t.Parallel()

		rw := httptest.NewRecorder()
		outCh, errCh := yieldData(t.Context())
		err := StreamJsonResults(t.Context(), rw, outCh, errCh,
			WithOnError[int](func(enc *json.Encoder, wroteHeader *bool, err error) { require.NoError(t, err) }))
		require.Error(t, err)
		assert.ErrorContains(t, err, "onError and onResult callbacks must be provided")
	})

	t.Run("Successful", func(t *testing.T) {
		t.Parallel()

		rw := httptest.NewRecorder()
		outCh, errCh := yieldData(t.Context())
		err := StreamJsonResults(t.Context(), rw, outCh, errCh,
			WithOnError[int](func(enc *json.Encoder, wroteHeader *bool, err error) { require.NoError(t, err) }),
			WithOnResult(func(i int) (any, error) { return i, nil }),
		)
		require.NoError(t, <-errCh)
		require.NoError(t, err)

		assert.Equal(t, http.StatusAccepted, rw.Code)
		assert.Equal(t, "application/x-ndjson", rw.Header().Get("Content-Type"))
		assert.Equal(t, "0\n1\n2\n3\n4\n5\n6\n7\n8\n9\n", rw.Body.String())
	})

	t.Run("Successful Early Header", func(t *testing.T) {
		t.Parallel()

		rw := httptest.NewRecorder()
		outCh, errCh := yieldData(t.Context())
		err := StreamJsonResults(t.Context(), rw, outCh, errCh,
			WithSendHeaderEarly[int](),
			WithOnError[int](func(enc *json.Encoder, wroteHeader *bool, err error) { require.NoError(t, err) }),
			WithOnResult(func(i int) (any, error) { return i, nil }),
		)
		require.NoError(t, <-errCh)
		require.NoError(t, err)

		assert.Equal(t, http.StatusAccepted, rw.Code)
		assert.Equal(t, "application/x-ndjson", rw.Header().Get("Content-Type"))
		assert.Equal(t, "0\n1\n2\n3\n4\n5\n6\n7\n8\n9\n", rw.Body.String())
	})

	t.Run("Partial Success", func(t *testing.T) {
		t.Parallel()

		rw := httptest.NewRecorder()
		outCh, errCh := yieldData(t.Context())
		err := StreamJsonResults(t.Context(), rw, outCh, errCh,
			WithOnError[int](func(enc *json.Encoder, wroteHeader *bool, err error) {
				require.True(t, *wroteHeader)
				require.Error(t, err)
				assert.ErrorContains(t, err, "simulated error")
			}),
			WithOnResult(func(i int) (any, error) {
				if i == 4 {
					return nil, errors.New("simulated error")
				}
				return i, nil
			}),
		)
		require.Error(t, err)
		assert.ErrorContains(t, err, "simulated error")

		assert.Equal(t, http.StatusAccepted, rw.Code)
		assert.Equal(t, "application/x-ndjson", rw.Header().Get("Content-Type"))
		assert.Equal(t, "0\n1\n2\n3\n", rw.Body.String())
	})

	t.Run("Stream Error", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(t.Context())
		cancel() // Cancel the context to simulate a stream error.

		rw := httptest.NewRecorder()
		_, errCh := yieldData(ctx)
		err := StreamJsonResults(t.Context(), rw, nil, errCh,
			WithOnError[int](func(enc *json.Encoder, wroteHeader *bool, err error) {
				require.False(t, *wroteHeader)
				require.Error(t, err)
				assert.ErrorContains(t, err, "context canceled")
				http.Error(rw, "context canceled", http.StatusInternalServerError)
			}),
			WithOnResult(func(i int) (any, error) { return i, nil }),
		)
		require.Error(t, err)
		assert.ErrorContains(t, err, "context canceled")

		assert.Equal(t, http.StatusInternalServerError, rw.Code)
		assert.Equal(t, "context canceled\n", rw.Body.String())
	})
}

// yieldData simulates a data source that produces integers and can be canceled via the test context.
func yieldData(ctx context.Context) (<-chan int, <-chan error) {
	out := make(chan int)
	errCh := make(chan error, 1)

	go func() {
		defer close(out)
		defer close(errCh)

		for i := range 10 {
			select {
			case out <- i:
			case <-ctx.Done():
				errCh <- ctx.Err()
				return
			}
		}
	}()
	return out, errCh
}
