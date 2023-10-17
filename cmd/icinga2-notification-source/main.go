package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"github.com/icinga/icinga-notifications/internal/event"
	"github.com/icinga/icinga-notifications/internal/eventstream"
	"net/http"
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
	panic(client.ListenEventStream())
}
