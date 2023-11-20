package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"
	"github.com/icinga/icinga-notifications/internal"
	"github.com/icinga/icinga-notifications/pkg/plugin"
	"github.com/icinga/icingadb/pkg/types"
	"github.com/jhillyerd/enmime"
	"net"
	"net/mail"
	"os"
	"os/user"
)

const (
	EncryptionNone     = "none"
	EncryptionStartTLS = "starttls"
	EncryptionTLS      = "tls"
)

type Email struct {
	Host       string `json:"host"`
	Port       string `json:"port"`
	From       string `json:"from"`
	User       string `json:"user"`
	Password   string `json:"password"`
	Encryption string `json:"encryption"`
}

func main() {
	plugin.RunPlugin(&Email{})
}

func (ch *Email) SendNotification(req *plugin.NotificationRequest) error {
	var to []mail.Address
	for _, address := range req.Contact.Addresses {
		if address.Type == "email" {
			to = append(to, mail.Address{Name: req.Contact.FullName, Address: address.Address})
		}
	}

	if len(to) == 0 {
		return fmt.Errorf("contact user %s doesn't have an e-mail address", req.Contact.FullName)
	}

	var msg bytes.Buffer
	plugin.FormatMessage(&msg, req)

	return enmime.Builder().
		ToAddrs(to).
		From("", ch.From).
		Subject(fmt.Sprintf("[#%d] %s %s is %s", req.Incident.Id, req.Event.Type, req.Object.Name, req.Incident.Severity)).
		Text(msg.Bytes()).
		Send(ch)
}

func (ch *Email) Send(reversePath string, recipients []string, msg []byte) error {
	var (
		client *smtp.Client
		err    error
	)

	if ch.Encryption == EncryptionTLS {
		client, err = smtp.DialTLS(ch.GetServer(), nil)
	} else {
		client, err = smtp.Dial(ch.GetServer())
	}
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

	if err = client.Hello("localhost"); err != nil {
		return err
	}

	if ch.Encryption == EncryptionStartTLS {
		if err = client.StartTLS(nil); err != nil {
			return err
		}
	}

	if ch.Password != "" {
		if err = client.Auth(sasl.NewPlainClient("", ch.User, ch.Password)); err != nil {
			return err
		}
	}

	if err := client.SendMail(reversePath, recipients, bytes.NewReader(msg)); err != nil {
		return err
	}

	return client.Quit()
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

	if ch.User == "" {
		ch.User = ch.From
	}

	return nil
}

func (ch *Email) GetInfo() *plugin.Info {
	elements := []*plugin.ConfigOption{
		{
			Name: "host",
			Type: "string",
			Label: map[string]string{
				"en_US": "SMTP Host",
				"de_DE": "SMTP Host",
			},
		},
		{
			Name: "port",
			Type: "number",
			Label: map[string]string{
				"en_US": "SMTP Port",
				"de_DE": "SMTP Port",
			},
			Min: types.Int{NullInt64: sql.NullInt64{Int64: 1, Valid: true}},
			Max: types.Int{NullInt64: sql.NullInt64{Int64: 65535, Valid: true}},
		},
		{
			Name: "from",
			Type: "string",
			Label: map[string]string{
				"en_US": "From",
				"de_DE": "Von",
			},
			Default: "icinga@example.com",
		},
		{
			Name: "user",
			Type: "string",
			Label: map[string]string{
				"en_US": "User",
				"de_DE": "Benutzer",
			},
			Default: "user@example.com",
		},
		{
			Name: "password",
			Type: "secret",
			Label: map[string]string{
				"en_US": "Password",
				"de_DE": "Passwort",
			},
		},
		{
			Name:     "encryption",
			Type:     "option",
			Required: true,
			Label: map[string]string{
				"en_US": "TLS / SSL",
				"de_DE": "TLS / SSL",
			},
			Options: map[string]string{
				EncryptionNone:     "None",
				EncryptionStartTLS: "STARTTLS",
				EncryptionTLS:      "TLS",
			},
		},
	}

	configAttrs, err := json.Marshal(elements)
	if err != nil {
		panic(err)
	}

	return &plugin.Info{
		Name:             "Email",
		Version:          internal.Version.Version,
		Author:           "Icinga GmbH",
		ConfigAttributes: configAttrs,
	}
}

func (ch *Email) GetServer() string {
	return net.JoinHostPort(ch.Host, ch.Port)
}
