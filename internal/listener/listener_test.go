package listener

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
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
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"
	"golang.org/x/crypto/bcrypt"
)

func makeTestListener(t *testing.T, useSocket bool, withCNSrc bool) *Listener {
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

	logs := logging.NewLoggingWithFactory("testing", zapcore.DebugLevel, time.Second, func(level zap.AtomicLevel) zapcore.Core {
		return zaptest.NewLogger(t, zaptest.Level(level.Level())).Core()
	})

	rc := config.NewRuntimeConfig(logs, nil)
	rc.Sources = map[int64]*config.Source{1: src}

	if withCNSrc {
		rc.Sources[2] = &config.Source{ClientCertificateSubject: types.MakeString("CN=icinga-source")}
	}

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

// makeRequestWithClientCert returns a POST request whose TLS state contains a verified client
// certificate with the given common name.
func makeRequestWithClientCert(cn string) *http.Request {
	cert := &x509.Certificate{Subject: pkix.Name{CommonName: cn}}
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.TLS = &tls.ConnectionState{
		VerifiedChains: [][]*x509.Certificate{{cert}},
	}
	return req
}

func TestSourceFromAuthOrAbort(t *testing.T) {
	t.Parallel()

	t.Run("Socket", func(t *testing.T) {
		t.Parallel()

		t.Run("ValidCredsMatchingSource", func(t *testing.T) {
			t.Parallel()

			l := makeTestListener(t, true, false)
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

			l := makeTestListener(t, true, false)
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

			l := makeTestListener(t, true, false)
			req := withPeerUsername(httptest.NewRequest(http.MethodPost, "/", nil), "unknown User")
			rw := httptest.NewRecorder()

			gotSrc := l.sourceFromAuthOrAbort(rw, req)

			assert.Nil(t, gotSrc)
			assert.Equal(t, http.StatusUnauthorized, rw.Code)
			assert.Empty(t, rw.Header().Get("WWW-Authenticate"))
		})

		t.Run("NoCredsInContext", func(t *testing.T) {
			t.Parallel()

			l := makeTestListener(t, true, false)
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

			l := makeTestListener(t, false, false)
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

			l := makeTestListener(t, false, false)
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

			l := makeTestListener(t, false, false)
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

			l := makeTestListener(t, false, false)
			req := httptest.NewRequest(http.MethodPost, "/", nil)
			rw := httptest.NewRecorder()

			gotSrc := l.sourceFromAuthOrAbort(rw, req)
			assert.Nil(t, gotSrc)
			assert.Equal(t, http.StatusUnauthorized, rw.Code)
			assert.NotEmpty(t, rw.Header().Get("WWW-Authenticate"))
		})
	})

	t.Run("Tls", func(t *testing.T) {
		t.Parallel()
		l := makeTestListener(t, false, true)

		cnSrc := l.runtimeConfig.Sources[2]
		basicSrc := l.runtimeConfig.Sources[1]

		t.Run("CertMatchingSource", func(t *testing.T) {
			t.Parallel()

			req := makeRequestWithClientCert("icinga-source")
			rw := httptest.NewRecorder()

			assert.Same(t, cnSrc, l.sourceFromAuthOrAbort(rw, req))
		})

		t.Run("CertCNNotMatchingAnySource", func(t *testing.T) {
			t.Parallel()

			req := makeRequestWithClientCert("unknown-cn")
			rw := httptest.NewRecorder()

			assert.Nil(t, l.sourceFromAuthOrAbort(rw, req))
			assert.Equal(t, http.StatusUnauthorized, rw.Code)
			assert.NotEmpty(t, rw.Header().Get("WWW-Authenticate"))
			assert.Contains(t, rw.Body.String(), "no matching source found")
		})

		t.Run("NoCertFallsBackToBasicAuth", func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodPost, "/", nil)
			req.SetBasicAuth("icingadb", "secret")
			rw := httptest.NewRecorder()

			req.TLS = &tls.ConnectionState{} // no verified chains
			assert.Same(t, basicSrc, l.sourceFromAuthOrAbort(rw, req))

			req.TLS = &tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{}} // empty verified chains
			assert.Same(t, basicSrc, l.sourceFromAuthOrAbort(rw, req))
		})

		t.Run("ClientCertAndBasicAuth", func(t *testing.T) {
			t.Parallel()

			req := makeRequestWithClientCert("unknown-cn")
			req.SetBasicAuth("icingadb", "secret")
			rw := httptest.NewRecorder()

			assert.Nil(t, l.sourceFromAuthOrAbort(rw, req))
			assert.Equal(t, http.StatusBadRequest, rw.Code)
			assert.Contains(t, rw.Body.String(), "client certificate and basic auth provided")
		})

		t.Run("EmptyVerifiedChain", func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodPost, "/", nil)
			rw := httptest.NewRecorder()

			req.TLS = &tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{}}}

			assert.Nil(t, l.sourceFromAuthOrAbort(rw, req))
			assert.Equal(t, http.StatusUnauthorized, rw.Code)
			assert.NotEmpty(t, rw.Header().Get("WWW-Authenticate"))
			assert.Contains(t, rw.Body.String(), "no verified chain found")
		})

		t.Run("EmptyClientCertificate", func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodPost, "/", nil)
			rw := httptest.NewRecorder()

			req.TLS = &tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{nil}}}

			assert.Nil(t, l.sourceFromAuthOrAbort(rw, req))
			assert.Equal(t, http.StatusUnauthorized, rw.Code)
			assert.NotEmpty(t, rw.Header().Get("WWW-Authenticate"))
			assert.Contains(t, rw.Body.String(), "no client certificate found")
		})
	})
}
