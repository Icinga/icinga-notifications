package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/icinga/icinga-notifications/pkg/plugin"
	"net"
	"net/smtp"
	"os"
	"os/user"
	"strings"
)

type Email struct {
	Host string `json:"host"`
	Port string `json:"port"`
	From string `json:"from"`
}

func main() {
	plugin.RunPlugin(&Email{})
}

func (ch *Email) SendNotification(req *plugin.NotificationRequest) error {
	var to []string
	for _, address := range req.Contact.Addresses {
		if address.Type == "email" {
			to = append(to, address.Address)
		}
	}

	if len(to) == 0 {
		return fmt.Errorf("contact user %s doesn't have an e-mail address", req.Contact.FullName)
	}

	var msg bytes.Buffer
	_, _ = fmt.Fprintf(&msg, "To: %s\n", strings.Join(to, ","))
	_, _ = fmt.Fprintf(&msg, "Subject: [#%d] %s %s is %s\n\n", req.Incident.Id, req.Event.Type, req.Object.Name, req.Event.Severity)

	plugin.FormatMessage(&msg, req)

	err := smtp.SendMail(ch.GetServer(), nil, ch.From, to, bytes.ReplaceAll(msg.Bytes(), []byte("\n"), []byte("\r\n")))
	if err != nil {
		return err
	}

	return nil
}

func (ch *Email) SetConfig(jsonStr json.RawMessage) error {
	err := json.Unmarshal(jsonStr, ch)
	if err != nil {
		return fmt.Errorf("failed to load config: %s %w", jsonStr, err)
	}

	if ch.Host == "" {
		ch.Host = "localhost"
	}

	if ch.Port == "" {
		ch.Port = "25"
	}

	if ch.From == "" {
		hostname, err := os.Hostname()
		if err != nil {
			return fmt.Errorf("failed to get the os's hostname: %w", err)
		}

		usr, err := user.Current()
		if err != nil {
			return fmt.Errorf("failed to get the os's current user: %w", err)
		}

		ch.From = usr.Username + "@" + hostname
	}

	return nil
}

func (ch *Email) GetInfo() *plugin.Info {
	return &plugin.Info{Name: "Email"}
}

func (ch *Email) GetServer() string {
	return net.JoinHostPort(ch.Host, ch.Port)
}
