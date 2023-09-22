package main

import (
	"crypto/tls"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
)

func main() {
	req, err := http.NewRequest(http.MethodPost, "https://localhost:5665/v1/events", strings.NewReader(`{"queue":"icinga-notifications","types":["StateChange","AcknowledgementSet","AcknowledgementCleared"]}`))
	if err != nil {
		panic(err)
	}

	req.SetBasicAuth("root", "icinga")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}

	res, err := client.Do(req)
	if err != nil {
		panic(err)
	}

	jsonR, jsonW := io.Pipe()
	go func() {
		_, err = io.Copy(io.MultiWriter(os.Stdout, jsonW), res.Body)
		if err != nil {
			panic(err)
		}
	}()

	dec := json.NewDecoder(jsonR)
	for {
		var event Icinga2Event
		err := dec.Decode(&event)
		if err != nil {
			panic(err)
		}
		log.Printf("%#v", &event)
	}
}

type Icinga2Event struct {
	Acknowledgement bool `json:"acknowledgement"`
	CheckResult     struct {
		Output string `json:"output"`
	} `json:"check_result"`
	Host      string  `json:"host"`
	Service   string  `json:"service"`
	State     int     `json:"state"`
	StateType int     `json:"state_type"`
	Timestamp float64 `json:"timestamp"`
	Type      string  `json:"type"`
}
