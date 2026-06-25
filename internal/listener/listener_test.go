package listener

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/icinga/icinga-go-library/logging"
	"github.com/icinga/icinga-go-library/types"
	"github.com/icinga/icinga-notifications/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
	"golang.org/x/crypto/bcrypt"
)

func makeTestListener(t *testing.T, useSocket bool) (*Listener, *config.Source) {
	t.Helper()

	hash, err := bcrypt.GenerateFromPassword([]byte("secret"), bcrypt.MinCost)
	require.NoError(t, err)

	src := &config.Source{
		ListenerUsername:     types.MakeString("icingadb"),
		ListenerPasswordHash: types.MakeString(string(hash)),
	}

	rc := &config.RuntimeConfig{}
	rc.Sources = map[int64]*config.Source{1: src}

	l := &Listener{
		runtimeConfig: rc,
		useSocket:     useSocket,
		logger:        logging.NewLogger(zaptest.NewLogger(t).Sugar(), time.Hour),
	}

	return l, src
}

func makePasswordlessTestListener(t *testing.T, useSocket bool) (*Listener, *config.Source) {
	t.Helper()

	src := &config.Source{
		ListenerUsername: types.MakeString("icingadb"),
	}

	rc := &config.RuntimeConfig{}
	rc.Sources = map[int64]*config.Source{1: src}

	l := &Listener{
		runtimeConfig: rc,
		useSocket:     useSocket,
		logger:        logging.NewLogger(zaptest.NewLogger(t).Sugar(), time.Hour),
	}

	return l, src
}

func TestSourceFromAuthOrAbort(t *testing.T) {
	t.Parallel()

	t.Run("socket", func(t *testing.T) {
		t.Parallel()

		t.Run("passwordless source accepts any credential", func(t *testing.T) {
			t.Parallel()

			l, src := makePasswordlessTestListener(t, true)

			for _, password := range []string{"", "anypassword", "secret"} {
				req := httptest.NewRequest(http.MethodPost, "/", nil)
				req.SetBasicAuth("icingadb", password)
				rw := httptest.NewRecorder()

				gotSrc, ok := l.sourceFromAuthOrAbort(rw, req)
				assert.True(t, ok, "password %q should be accepted for passwordless source", password)
				assert.Same(t, src, gotSrc)
			}
		})

		t.Run("password-protected source requires correct password", func(t *testing.T) {
			t.Parallel()

			l, src := makeTestListener(t, true)
			req := httptest.NewRequest(http.MethodPost, "/", nil)
			req.SetBasicAuth("icingadb", "secret")
			rw := httptest.NewRecorder()

			gotSrc, ok := l.sourceFromAuthOrAbort(rw, req)
			assert.True(t, ok)
			assert.Same(t, src, gotSrc)
		})

		t.Run("password-protected source rejects wrong password", func(t *testing.T) {
			t.Parallel()

			l, _ := makeTestListener(t, true)
			req := httptest.NewRequest(http.MethodPost, "/", nil)
			req.SetBasicAuth("icingadb", "wrongpassword")
			rw := httptest.NewRecorder()

			gotSrc, ok := l.sourceFromAuthOrAbort(rw, req)
			assert.False(t, ok)
			assert.Nil(t, gotSrc)
			assert.Equal(t, http.StatusUnauthorized, rw.Code)
			assert.Empty(t, rw.Header().Get("WWW-Authenticate"))
		})

		t.Run("password-protected source rejects empty password", func(t *testing.T) {
			t.Parallel()

			l, _ := makeTestListener(t, true)
			req := httptest.NewRequest(http.MethodPost, "/", nil)
			req.SetBasicAuth("icingadb", "")
			rw := httptest.NewRecorder()

			gotSrc, ok := l.sourceFromAuthOrAbort(rw, req)
			assert.False(t, ok)
			assert.Nil(t, gotSrc)
			assert.Equal(t, http.StatusUnauthorized, rw.Code)
			assert.Empty(t, rw.Header().Get("WWW-Authenticate"))
		})

		t.Run("unknown username returns 401 without WWW-Authenticate", func(t *testing.T) {
			t.Parallel()

			l, _ := makeTestListener(t, true)
			req := httptest.NewRequest(http.MethodPost, "/", nil)
			req.SetBasicAuth("unknown", "")
			rw := httptest.NewRecorder()

			gotSrc, ok := l.sourceFromAuthOrAbort(rw, req)
			assert.False(t, ok)
			assert.Nil(t, gotSrc)
			assert.Equal(t, http.StatusUnauthorized, rw.Code)
			assert.Empty(t, rw.Header().Get("WWW-Authenticate"))
		})

		t.Run("missing auth header returns 401 without WWW-Authenticate", func(t *testing.T) {
			t.Parallel()

			l, _ := makeTestListener(t, true)
			req := httptest.NewRequest(http.MethodPost, "/", nil)
			rw := httptest.NewRecorder()

			gotSrc, ok := l.sourceFromAuthOrAbort(rw, req)
			assert.False(t, ok)
			assert.Nil(t, gotSrc)
			assert.Equal(t, http.StatusUnauthorized, rw.Code)
			assert.Empty(t, rw.Header().Get("WWW-Authenticate"))
		})
	})

	t.Run("tcp", func(t *testing.T) {
		t.Parallel()

		t.Run("correct credentials succeed", func(t *testing.T) {
			t.Parallel()

			l, src := makeTestListener(t, false)
			req := httptest.NewRequest(http.MethodPost, "/", nil)
			req.SetBasicAuth("icingadb", "secret")
			rw := httptest.NewRecorder()

			gotSrc, ok := l.sourceFromAuthOrAbort(rw, req)
			assert.True(t, ok)
			assert.Same(t, src, gotSrc)
		})

		t.Run("wrong password returns 401 with WWW-Authenticate", func(t *testing.T) {
			t.Parallel()

			l, _ := makeTestListener(t, false)
			req := httptest.NewRequest(http.MethodPost, "/", nil)
			req.SetBasicAuth("icingadb", "wrongpassword")
			rw := httptest.NewRecorder()

			gotSrc, ok := l.sourceFromAuthOrAbort(rw, req)
			assert.False(t, ok)
			assert.Nil(t, gotSrc)
			assert.Equal(t, http.StatusUnauthorized, rw.Code)
			assert.NotEmpty(t, rw.Header().Get("WWW-Authenticate"))
		})

		t.Run("unknown username returns 401 with WWW-Authenticate", func(t *testing.T) {
			t.Parallel()

			l, _ := makeTestListener(t, false)
			req := httptest.NewRequest(http.MethodPost, "/", nil)
			req.SetBasicAuth("unknown", "secret")
			rw := httptest.NewRecorder()

			gotSrc, ok := l.sourceFromAuthOrAbort(rw, req)
			assert.False(t, ok)
			assert.Nil(t, gotSrc)
			assert.Equal(t, http.StatusUnauthorized, rw.Code)
			assert.NotEmpty(t, rw.Header().Get("WWW-Authenticate"))
		})

		t.Run("missing auth header returns 401 with WWW-Authenticate", func(t *testing.T) {
			t.Parallel()

			l, _ := makeTestListener(t, false)
			req := httptest.NewRequest(http.MethodPost, "/", nil)
			rw := httptest.NewRecorder()

			gotSrc, ok := l.sourceFromAuthOrAbort(rw, req)
			assert.False(t, ok)
			assert.Nil(t, gotSrc)
			assert.Equal(t, http.StatusUnauthorized, rw.Code)
			assert.NotEmpty(t, rw.Header().Get("WWW-Authenticate"))
		})
	})
}
