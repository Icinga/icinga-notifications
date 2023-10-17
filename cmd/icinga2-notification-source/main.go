package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"github.com/icinga/icinga-notifications/internal/event"
	"github.com/icinga/icinga-notifications/internal/eventstream"
	"net/http"
	"time"
)

func main() {
	client := eventstream.Client{
		ApiHost:          "https://localhost:5665",
		ApiBasicAuthUser: "root",
		ApiBasicAuthPass: "icinga",
		ApiHttpTransport: http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
		IcingaWebRoot:    "http://localhost/icingaweb2",
		Ctx:              context.Background(),
		CallbackFn:       func(event event.Event) { fmt.Println(event.FullString()) },
	}

	fmt.Println(client.QueryObjectApiSince("host", time.Now().Add(-time.Minute)))
	fmt.Println(client.QueryObjectApiSince("service", time.Now().Add(-time.Minute)))

	panic(client.ListenEventStream())
}
