package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/icinga/icinga-notifications/internal"
	"github.com/icinga/icinga-notifications/pkg/plugin"
	"net/http"
	"time"
)

func main() {
	plugin.RunPlugin(&RocketChat{})
}

type RocketChat struct {
	URL    string `json:"url"`
	UserID string `json:"user_id"`
	Token  string `json:"token"`
}

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
	err := plugin.PopulateDefaults(ch)
	if err != nil {
		return err
	}

	return json.Unmarshal(jsonStr, ch)
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

	request, err := http.NewRequest(http.MethodPost, ch.URL+"/api/v1/chat.postMessage", bytes.NewReader(body))
	if err != nil {
		return err
	}

	request.Header.Set("X-Auth-Token", ch.Token)
	request.Header.Set("X-User-Id", ch.UserID)
	request.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("error while sending http request to rocketchat server: %w", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return errors.New(resp.Status)
	}

	return nil
}
