package listener

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"os/user"
	"strconv"
	"testing"
	"time"

	"github.com/icinga/icinga-go-library/logging"
	"github.com/icinga/icinga-go-library/types"
	"github.com/icinga/icinga-notifications/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/sys/unix"
)

func makeTestListener(t *testing.T, useSocket bool) (*Listener, *config.Source) {
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

// makeOSUserTestListener creates a socket listener whose single source has ListenerUsername set to the current OS user,
// and returns the listener, source, and the current user's UID.
func makeOSUserTestListener(t *testing.T) (*Listener, *config.Source, uint32) {
	u, err := user.Current()
	require.NoError(t, err)

	uid, err := strconv.ParseUint(u.Uid, 10, 32)
	require.NoError(t, err)

	src := &config.Source{
		ListenerUsername: types.MakeString(u.Username),
	}
	rc := &config.RuntimeConfig{}
	rc.Sources = map[int64]*config.Source{1: src}

	l := &Listener{
		runtimeConfig: rc,
		useSocket:     true,
		logger:        logging.NewLogger(zaptest.NewLogger(t).Sugar(), time.Hour),
	}

	return l, src, uint32(uid)
}

// withPeerCreds returns a copy of req with a *unix.Ucred carrying uid injected into its context.
func withPeerCreds(req *http.Request, uid uint32) *http.Request {
	creds := &unix.Ucred{Uid: uid}
	return req.WithContext(context.WithValue(req.Context(), listenerUnixDomainSocketCreds{}, creds))
}

func TestSourceFromAuthOrAbort(t *testing.T) {
	t.Parallel()

	t.Run("Socket", func(t *testing.T) {
		t.Parallel()

		t.Run("ValidCredsMatchingSource", func(t *testing.T) {
			t.Parallel()

			l, src, uid := makeOSUserTestListener(t)
			req := withPeerCreds(httptest.NewRequest(http.MethodPost, "/", nil), uid)
			rw := httptest.NewRecorder()

			gotSrc, ok := l.sourceFromAuthOrAbort(rw, req)
			assert.True(t, ok)
			assert.Same(t, src, gotSrc)
		})

		t.Run("ValidCredsNoMatchingSource", func(t *testing.T) {
			t.Parallel()

			l, _, uid := makeOSUserTestListener(t)
			// Replace the source with one that has a different username.
			l.runtimeConfig.Sources[1] = &config.Source{
				ListenerUsername: types.MakeString("some-other-source"),
			}
			req := withPeerCreds(httptest.NewRequest(http.MethodPost, "/", nil), uid)
			rw := httptest.NewRecorder()

			gotSrc, ok := l.sourceFromAuthOrAbort(rw, req)
			assert.False(t, ok)
			assert.Nil(t, gotSrc)
			assert.Equal(t, http.StatusUnauthorized, rw.Code)
			assert.Empty(t, rw.Header().Get("WWW-Authenticate"))
		})

		t.Run("CredsWithUnknownUID", func(t *testing.T) {
			t.Parallel()

			l, _, _ := makeOSUserTestListener(t)
			req := withPeerCreds(httptest.NewRequest(http.MethodPost, "/", nil), math.MaxUint32)
			rw := httptest.NewRecorder()

			gotSrc, ok := l.sourceFromAuthOrAbort(rw, req)
			assert.False(t, ok)
			assert.Nil(t, gotSrc)
			assert.Equal(t, http.StatusUnauthorized, rw.Code)
			assert.Empty(t, rw.Header().Get("WWW-Authenticate"))
		})

		t.Run("NoCredsInContext", func(t *testing.T) {
			t.Parallel()

			l, _, _ := makeOSUserTestListener(t)
			req := httptest.NewRequest(http.MethodPost, "/", nil)
			rw := httptest.NewRecorder()

			gotSrc, ok := l.sourceFromAuthOrAbort(rw, req)
			assert.False(t, ok)
			assert.Nil(t, gotSrc)
			assert.Equal(t, http.StatusUnauthorized, rw.Code)
			assert.Empty(t, rw.Header().Get("WWW-Authenticate"))
		})
	})

	t.Run("Tcp", func(t *testing.T) {
		t.Parallel()

		t.Run("CorrectCredentials", func(t *testing.T) {
			t.Parallel()

			l, src := makeTestListener(t, false)
			req := httptest.NewRequest(http.MethodPost, "/", nil)
			req.SetBasicAuth("icingadb", "secret")
			rw := httptest.NewRecorder()

			gotSrc, ok := l.sourceFromAuthOrAbort(rw, req)
			assert.True(t, ok)
			assert.Same(t, src, gotSrc)
		})

		t.Run("WrongPassword", func(t *testing.T) {
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

		t.Run("UnknownUsername", func(t *testing.T) {
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

		t.Run("MissingAuthHeader", func(t *testing.T) {
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
