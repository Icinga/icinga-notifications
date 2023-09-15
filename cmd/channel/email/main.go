package main

import (
	"bytes"
	"fmt"
	"github.com/icinga/icinga-notifications/pluginLoader"
	"net"
	"net/smtp"
	"strings"
)

type Email struct {
	Host string `json:"host"`
	Port string `json:"port"`
	From string `json:"from"`
}

func main() {
	pluginLoader.RunPlugin(&Email{})
}

func (ch *Email) Send(req pluginLoader.NotificationRequest) error {
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
	_, _ = fmt.Fprintf(&msg, "Subject: [#%d] %s %s is %s\n\n", req.Incident.Id, req.Event.Type, req.Incident.ObjectDisplayName, req.Event.Severity.String())

	pluginLoader.FormatMessage(&msg, req)

	err := smtp.SendMail(ch.GetServer(), nil, ch.From, to, bytes.ReplaceAll(msg.Bytes(), []byte("\n"), []byte("\r\n")))
	if err != nil {
		return err
	}

	return nil
}

func (ch *Email) GetServer() string {
	return net.JoinHostPort(ch.Host, ch.Port)
}
