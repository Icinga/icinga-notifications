package listener

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os/user"
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

func makeTestListener(t *testing.T, useSocket bool) *Listener {
	var src *config.Source
	if useSocket {
		u, err := user.Current()
		require.NoError(t, err)

		src = &config.Source{
			ListenerUsername: types.MakeString(u.Username),
		}
	} else {
		hash, err := bcrypt.GenerateFromPassword([]byte("secret"), bcrypt.MinCost)
		require.NoError(t, err)

		src = &config.Source{
			ListenerUsername:     types.MakeString("icingadb"),
			ListenerPasswordHash: types.MakeString(string(hash)),
		}
	}

	rc := &config.RuntimeConfig{}
	rc.Sources = map[int64]*config.Source{1: src}

	return &Listener{
		runtimeConfig: rc,
		useSocket:     useSocket,
		logger:        logging.NewLogger(zaptest.NewLogger(t).Sugar(), time.Hour),
	}
}

// withPeerUsername returns a copy of req with the given username injected into its context.
func withPeerUsername(req *http.Request, username string) *http.Request {
	setUser := func() (string, error) {
		return username, nil
	}
	return req.WithContext(context.WithValue(req.Context(), peerUserLookupKey{}, peerUserLookupFunc(setUser)))
}

func TestSourceFromAuthOrAbort(t *testing.T) {
	t.Parallel()

	t.Run("Socket", func(t *testing.T) {
		t.Parallel()

		t.Run("ValidCredsMatchingSource", func(t *testing.T) {
			t.Parallel()

			l := makeTestListener(t, true)
			src := l.runtimeConfig.Sources[1]
			username := src.ListenerUsername.String
			req := withPeerUsername(httptest.NewRequest(http.MethodPost, "/", nil), username)
			rw := httptest.NewRecorder()

			gotSrc := l.sourceFromAuthOrAbort(rw, req)
			assert.NotNil(t, gotSrc)
			assert.Same(t, src, gotSrc)
		})

		t.Run("ValidCredsNoMatchingSource", func(t *testing.T) {
			t.Parallel()

			l := makeTestListener(t, true)
			username := l.runtimeConfig.Sources[1].ListenerUsername.String
			// Replace the source with one that has a different username.
			l.runtimeConfig.Sources[1] = &config.Source{
				ListenerUsername: types.MakeString("some-other-source"),
			}
			req := withPeerUsername(httptest.NewRequest(http.MethodPost, "/", nil), username)
			rw := httptest.NewRecorder()

			gotSrc := l.sourceFromAuthOrAbort(rw, req)
			assert.Nil(t, gotSrc)
			assert.Equal(t, http.StatusUnauthorized, rw.Code)
			assert.Empty(t, rw.Header().Get("WWW-Authenticate"))
		})

		t.Run("CredsWithUnknownUsername", func(t *testing.T) {
			t.Parallel()

			l := makeTestListener(t, true)
			req := withPeerUsername(httptest.NewRequest(http.MethodPost, "/", nil), "unknown User")
			rw := httptest.NewRecorder()

			gotSrc := l.sourceFromAuthOrAbort(rw, req)

			assert.Nil(t, gotSrc)
			assert.Equal(t, http.StatusUnauthorized, rw.Code)
			assert.Empty(t, rw.Header().Get("WWW-Authenticate"))
		})

		t.Run("NoCredsInContext", func(t *testing.T) {
			t.Parallel()

			l := makeTestListener(t, true)
			req := httptest.NewRequest(http.MethodPost, "/", nil)
			rw := httptest.NewRecorder()

			gotSrc := l.sourceFromAuthOrAbort(rw, req)
			assert.Nil(t, gotSrc)
			assert.Equal(t, http.StatusUnauthorized, rw.Code)
			assert.Empty(t, rw.Header().Get("WWW-Authenticate"))
		})
	})

	t.Run("Tcp", func(t *testing.T) {
		t.Parallel()

		t.Run("CorrectCredentials", func(t *testing.T) {
			t.Parallel()

			l := makeTestListener(t, false)
			src := l.runtimeConfig.Sources[1]
			req := httptest.NewRequest(http.MethodPost, "/", nil)
			req.SetBasicAuth("icingadb", "secret")
			rw := httptest.NewRecorder()

			gotSrc := l.sourceFromAuthOrAbort(rw, req)
			assert.NotNil(t, gotSrc)
			assert.Same(t, src, gotSrc)
		})

		t.Run("WrongPassword", func(t *testing.T) {
			t.Parallel()

			l := makeTestListener(t, false)
			req := httptest.NewRequest(http.MethodPost, "/", nil)
			req.SetBasicAuth("icingadb", "wrongpassword")
			rw := httptest.NewRecorder()

			gotSrc := l.sourceFromAuthOrAbort(rw, req)
			assert.Nil(t, gotSrc)
			assert.Equal(t, http.StatusUnauthorized, rw.Code)
			assert.NotEmpty(t, rw.Header().Get("WWW-Authenticate"))
		})

		t.Run("UnknownUsername", func(t *testing.T) {
			t.Parallel()

			l := makeTestListener(t, false)
			req := httptest.NewRequest(http.MethodPost, "/", nil)
			req.SetBasicAuth("unknown", "secret")
			rw := httptest.NewRecorder()

			gotSrc := l.sourceFromAuthOrAbort(rw, req)
			assert.Nil(t, gotSrc)
			assert.Equal(t, http.StatusUnauthorized, rw.Code)
			assert.NotEmpty(t, rw.Header().Get("WWW-Authenticate"))
		})

		t.Run("MissingAuthHeader", func(t *testing.T) {
			t.Parallel()

			l := makeTestListener(t, false)
			req := httptest.NewRequest(http.MethodPost, "/", nil)
			rw := httptest.NewRecorder()

			gotSrc := l.sourceFromAuthOrAbort(rw, req)
			assert.Nil(t, gotSrc)
			assert.Equal(t, http.StatusUnauthorized, rw.Code)
			assert.NotEmpty(t, rw.Header().Get("WWW-Authenticate"))
		})
	})
}
