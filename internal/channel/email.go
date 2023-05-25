package channel

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/icinga/icinga-notifications/internal/contracts"
	"github.com/icinga/icinga-notifications/internal/event"
	"github.com/icinga/icinga-notifications/internal/recipient"
	"net"
	"net/smtp"
	"os"
	"os/user"
	"strconv"
	"strings"
)

type EMail struct {
	config struct {
		Host string `json:"host"`
		Port uint16 `json:"port"`
		From string `json:"from"`
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

	if e.config.From == "" {
		hostname, err := os.Hostname()
		if err != nil {
			return nil, err
		}

		usr, err := user.Current()
		if err != nil {
			return nil, err
		}

		e.config.From = usr.Username + "@" + hostname
	}

	return e, nil
}

func (e *EMail) Send(contact *recipient.Contact, incident contracts.Incident, event *event.Event, icingaweb2Url string) error {
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
	_, _ = fmt.Fprintf(&msg, "Subject: [#%d] %s %s is %s\n\n", incident.ID(), event.Type, incident.ObjectDisplayName(), event.Severity.String())

	FormatMessage(&msg, incident, event, icingaweb2Url)

	err := smtp.SendMail(e.GetServer(), nil, e.config.From, to, bytes.ReplaceAll(msg.Bytes(), []byte("\n"), []byte("\r\n")))
	if err != nil {
		return err
	}

	return nil
}

func (e *EMail) GetServer() string {
	return net.JoinHostPort(e.config.Host, strconv.Itoa(int(e.config.Port)))
}
