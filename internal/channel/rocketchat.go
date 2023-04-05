package channel

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/icinga/noma/internal/event"
	"github.com/icinga/noma/internal/incident"
	"github.com/icinga/noma/internal/recipient"
	"log"
	"net/http"
	"time"
)

type RocketChat struct {
	config struct {
		URL    string `json:"url"`
		UserID string `json:"user_id"`
		Token  string `json:"token"`
	}
}

func NewRocketChat(config string) (Plugin, error) {
	r := new(RocketChat)

	err := json.Unmarshal([]byte(config), &r.config)
	if err != nil {
		return nil, err
	}

	return r, nil
}

func (r *RocketChat) Send(contact *recipient.Contact, incident *incident.Incident, event *event.Event) error {
	log.Printf("rocketchat: contact=%v incident=%v event=%v", contact, incident, event)

	var output bytes.Buffer
	FormatMessage(&output, incident, event)

	var roomId string
	for _, address := range contact.Addresses {
		if address.Type == "rocketchat" {
			roomId = address.Address
			break
		}
	}

	if roomId == "" {
		return fmt.Errorf("contact user %s doesn't specify a rocketchat channel or username", contact.FullName)
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

	req, err := http.NewRequest(http.MethodPost, r.config.URL+"/api/v1/chat.postMessage", bytes.NewReader(body))
	if err != nil {
		return err
	}

	req.Header.Set("X-Auth-Token", r.config.Token)
	req.Header.Set("X-User-Id", r.config.UserID)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("error while sending http request to rocketchat server: %s", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.New(resp.Status)
	}

	log.Printf("Successfully sent a rocketchat message to user %s\n", contact.FullName)

	return nil
}
