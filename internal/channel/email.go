package channel

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/icinga/noma/internal/event"
	"github.com/icinga/noma/internal/incident"
	"github.com/icinga/noma/internal/recipient"
	"log"
	"net"
	"net/smtp"
	"os"
	"os/user"
	"strconv"
	"strings"
)

type EMail struct {
	config struct {
		Host   string `json:"host"`
		Port   uint16 `json:"port"`
		Sender string `json:"sender"`
	}
}

func NewEMail(config string) (Plugin, error) {
	e := &EMail{}

	err := json.Unmarshal([]byte(config), &e.config)
	if err != nil {
		return nil, err
	}

	if e.config.Host == "" {
		e.config.Host = "localhost"
	}

	if e.config.Port == 0 {
		e.config.Port = 25
	}

	if e.config.Sender == "" {
		hostname, err := os.Hostname()
		if err != nil {
			return nil, err
		}

		usr, err := user.Current()
		if err != nil {
			return nil, err
		}

		e.config.Sender = usr.Username + "@" + hostname
	}

	return e, nil
}

func (e *EMail) Send(contact *recipient.Contact, incident *incident.Incident, event *event.Event) error {
	log.Printf("email: contact=%v incident=%v event=%v", contact, incident, event)

	var to []string
	for _, address := range contact.Addresses {
		if address.Type == "email" {
			to = append(to, address.Address)
		}
	}

	if len(to) == 0 {
		return fmt.Errorf("contact user %s doesn't have an e-mail address", contact.FullName)
	}

	var msg bytes.Buffer
	_, _ = fmt.Fprintf(&msg, "To: %s\n", strings.Join(to, ","))
	_, _ = fmt.Fprintf(&msg, "Subject: %s %s is %s\n\n", event.Type, incident.Object.DisplayName(), event.Severity.String())

	FormatMessage(&msg, incident, event)

	err := smtp.SendMail(e.GetServer(), nil, e.config.Sender, to, bytes.ReplaceAll(msg.Bytes(), []byte("\n"), []byte("\r\n")))
	if err != nil {
		return err
	}

	log.Printf("Successfully sent mail to user %s\n", contact.FullName)

	return nil
}

func (e *EMail) GetServer() string {
	return net.JoinHostPort(e.config.Host, strconv.Itoa(int(e.config.Port)))
}
