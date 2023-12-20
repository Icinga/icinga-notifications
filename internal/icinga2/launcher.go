package icinga2

// This file contains the Launcher type to, well, launch new Event Stream Clients through a callback function.

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"github.com/icinga/icinga-notifications/internal"
	"github.com/icinga/icinga-notifications/internal/config"
	"github.com/icinga/icinga-notifications/internal/daemon"
	"github.com/icinga/icinga-notifications/internal/event"
	"github.com/icinga/icinga-notifications/internal/eventhandler"
	"github.com/icinga/icinga-notifications/internal/incident"
	"github.com/icinga/icingadb/pkg/icingadb"
	"github.com/icinga/icingadb/pkg/logging"
	"go.uber.org/zap"
	"net/http"
	"sync"
)

// Launcher allows starting a new Icinga 2 Event Stream API Client through a callback from within the config package.
//
// This architecture became kind of necessary to work around circular imports due to the RuntimeConfig's omnipresence.
type Launcher struct {
	Ctx           context.Context
	Logs          *logging.Logging
	Db            *icingadb.DB
	RuntimeConfig *config.RuntimeConfig

	mutex          sync.Mutex
	isReady        bool
	waitingSources []*config.Source
}

// Launch either directly launches an Icinga 2 Event Stream Client for this Source or enqueues it until the Launcher is Ready.
func (launcher *Launcher) Launch(src *config.Source) {
	launcher.mutex.Lock()
	defer launcher.mutex.Unlock()

	if !launcher.isReady {
		launcher.Logs.GetChildLogger("icinga2").
			With(zap.Int64("source-id", src.ID)).
			Debug("Postponing Event Stream Client Launch as Launcher is not ready yet")
		launcher.waitingSources = append(launcher.waitingSources, src)
		return
	}

	launcher.launch(src)
}

// Ready marks the Launcher as ready and launches all enqueued, postponed Sources.
func (launcher *Launcher) Ready() {
	launcher.mutex.Lock()
	defer launcher.mutex.Unlock()

	launcher.isReady = true
	for _, src := range launcher.waitingSources {
		launcher.Logs.GetChildLogger("icinga2").
			With(zap.Int64("source-id", src.ID)).
			Debug("Launching postponed Event Stream Client")
		launcher.launch(src)
	}
	launcher.waitingSources = nil
}

// launch a new Icinga 2 Event Stream API Client based on the config.Source configuration.
func (launcher *Launcher) launch(src *config.Source) {
	logger := launcher.Logs.GetChildLogger("icinga2").With(zap.Int64("source-id", src.ID))

	if src.Type != config.SourceTypeIcinga2 ||
		!src.Icinga2BaseURL.Valid ||
		!src.Icinga2AuthUser.Valid ||
		!src.Icinga2AuthPass.Valid {
		logger.Error("Source is either not of type icinga2 or not fully populated")
		return
	}

	trans := &transport{
		Transport: http.Transport{
			// Hardened TLS config adjusted to Icinga 2's configuration:
			// - https://icinga.com/docs/icinga-2/latest/doc/09-object-types/#objecttype-apilistener
			// - https://icinga.com/docs/icinga-2/latest/doc/12-icinga2-api/#security
			// - https://ssl-config.mozilla.org/#server=go&config=intermediate
			TLSClientConfig: &tls.Config{
				MinVersion: tls.VersionTLS12,
				CipherSuites: []uint16{
					tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
					tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
					tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
					tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
					tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
					tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
				},
			},
		},
		userAgent: "icinga-notifications/" + internal.Version.Version,
	}

	if src.Icinga2CAPem.Valid {
		certPool := x509.NewCertPool()
		if !certPool.AppendCertsFromPEM([]byte(src.Icinga2CAPem.String)) {
			logger.Error("Cannot add custom CA file to CA pool")
			return
		}

		trans.TLSClientConfig.RootCAs = certPool
	}
	if src.Icinga2CommonName.Valid {
		trans.TLSClientConfig.ServerName = src.Icinga2CommonName.String
	}
	if src.Icinga2InsecureTLS.Valid && src.Icinga2InsecureTLS.Bool {
		trans.TLSClientConfig.InsecureSkipVerify = true
	}

	subCtx, subCtxCancel := context.WithCancel(launcher.Ctx)
	client := &Client{
		ApiBaseURL:       src.Icinga2BaseURL.String,
		ApiBasicAuthUser: src.Icinga2AuthUser.String,
		ApiBasicAuthPass: src.Icinga2AuthPass.String,
		ApiHttpTransport: trans,

		EventSourceId: src.ID,
		IcingaWebRoot: daemon.Config().Icingaweb2URL,

		CallbackFn: func(ev *event.Event) {
			l := logger.With(zap.Stringer("event", ev))

			err := eventhandler.ProcessEvent(subCtx, launcher.Db, launcher.Logs, launcher.RuntimeConfig, ev)
			switch {
			case errors.Is(err, incident.ErrSuperfluousStateChange):
				l.Debugw("Stopped processing event with superfluous state change", zap.Error(err))
			case err != nil:
				l.Errorw("Cannot process event", zap.Error(err))
			default:
				l.Debug("Successfully processed event over callback")
			}
		},
		Ctx:       subCtx,
		CtxCancel: subCtxCancel,
		Logger:    logger,
	}

	go client.Process()
	src.Icinga2SourceCancel = subCtxCancel
}
