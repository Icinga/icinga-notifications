package main

import (
	"context"
	"crypto/tls"
	"github.com/icinga/icinga-notifications/internal/event"
	"github.com/icinga/icinga-notifications/internal/eventstream"
	"github.com/icinga/icingadb/pkg/logging"
	"go.uber.org/zap"
	"net/http"
	"time"
)

func main() {
	logs, err := logging.NewLogging("ici2-noma", zap.DebugLevel, logging.CONSOLE, nil, time.Second)
	if err != nil {
		panic(err)
	}

	client := eventstream.Client{
		ApiHost:          "https://localhost:5665",
		ApiBasicAuthUser: "root",
		ApiBasicAuthPass: "icinga",
		ApiHttpTransport: http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},

		IcingaWebRoot:                    "http://localhost/icingaweb2",
		IcingaNotificationsEventSourceId: 1,

		CallbackFn: func(event.Event) { /* nop */ },
		Ctx:        context.Background(),
		Logger:     logs.GetLogger(),
	}
	client.Process()
}
