package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/icinga/icinga-go-library/notifications/plugin"
	"github.com/icinga/icinga-notifications/internal"
)

func main() {
	plugin.Run(&RocketChat{})
}

type (
	RocketChat struct {
		URL    string `json:"url"`
		UserID string `json:"user_id"`
		Token  string `json:"token"`

		client *http.Client
		mu     sync.Mutex // Protects access to the above fields.
	}

	// roundTripper is a custom http.RoundTripper that adds the Rocket.Chat authentication headers to each request.
	roundTripper struct {
		http.RoundTripper

		token  string
		userID string
	}
)

func (ch *RocketChat) GetInfo() *plugin.Info {
	configAttrs := plugin.ConfigOptions{
		{
			Name: "url",
			Type: "string",
			Label: map[string]string{
				"en_US": "Rocket.Chat URL",
				"de_DE": "Rocket.Chat URL",
			},
			Required: true,
		},
		{
			Name: "user_id",
			Type: "string",
			Label: map[string]string{
				"en_US": "User ID",
				"de_DE": "Benutzer ID",
			},
			Required: true,
		},
		{
			Name: "token",
			Type: "secret",
			Label: map[string]string{
				"en_US": "Personal Access Token",
				"de_DE": "Persönliches Zugangstoken",
			},
			Required: true,
		},
	}

	return &plugin.Info{
		Name:             "Rocket.Chat",
		Version:          internal.Version.Version,
		Author:           "Icinga GmbH",
		ConfigAttributes: configAttrs,
	}
}

func (ch *RocketChat) SetConfig(jsonStr json.RawMessage) error {
	var tmpRC RocketChat
	if err := plugin.PopulateDefaults(&tmpRC); err != nil {
		return err
	}

	if err := json.Unmarshal(jsonStr, &tmpRC); err != nil {
		return fmt.Errorf("could not unmarshal configuration: %w", err)
	}

	ch.mu.Lock()
	defer ch.mu.Unlock()

	if ch.client != nil && (ch.URL != tmpRC.URL || ch.UserID != tmpRC.UserID || ch.Token != tmpRC.Token) {
		ch.client.CloseIdleConnections()
		ch.client = nil
	}

	ch.URL = tmpRC.URL
	ch.UserID = tmpRC.UserID
	ch.Token = tmpRC.Token
	if ch.client == nil {
		ch.client = &http.Client{
			Timeout:   10 * time.Second,
			Transport: &roundTripper{RoundTripper: http.DefaultTransport, token: ch.Token, userID: ch.UserID},
		}
	}

	return nil
}

func (ch *RocketChat) SendNotification(req *plugin.NotificationRequest) error {
	var output bytes.Buffer
	_, _ = fmt.Fprint(&output, plugin.FormatSubject(req)+"\n\n")

	plugin.FormatMessage(&output, req)

	var roomId string
	for _, address := range req.Contact.Addresses {
		if address.Type == "rocketchat" {
			roomId = address.Address
			break
		}
	}

	if roomId == "" {
		return fmt.Errorf("contact user %s does not specify a rocketchat channel or username", req.Contact.FullName)
	}

	message := struct {
		Channel string `json:"channel"`
		Text    string `json:"text"`
	}{
		Channel: roomId,
		Text:    output.String(),
	}

	body, err := json.Marshal(message)
	if err != nil {
		return err
	}

	ch.mu.Lock()
	client := ch.client
	url := ch.URL
	ch.mu.Unlock()

	request, err := http.NewRequest(http.MethodPost, url+"/api/v1/chat.postMessage", bytes.NewReader(body))
	if err != nil {
		return err
	}

	//nolint:bodyclose // False positive, drainAndClose is called in the defer statement below.
	resp, err := client.Do(request) // #nosec G704 -- no SSRF, trusted user input
	if err != nil {
		return fmt.Errorf("error while sending http request to rocketchat server: %w", err)
	}
	defer drainAndClose(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return errors.New(resp.Status)
	}

	return nil
}

// RoundTrip implements the [http.RoundTripper] interface for the custom roundTripper type.
func (rt *roundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("X-Auth-Token", rt.token)
	req.Header.Set("X-User-Id", rt.userID)
	req.Header.Set("Content-Type", "application/json")
	return rt.RoundTripper.RoundTrip(req)
}

// drainAndClose reads and discards the remaining data from the provided io.ReadCloser and then closes it.
func drainAndClose(r io.ReadCloser) {
	_, _ = io.Copy(io.Discard, r)
	_ = r.Close()
}
