package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"
	"github.com/google/uuid"
	"github.com/icinga/icinga-notifications/internal"
	"github.com/icinga/icinga-notifications/pkg/plugin"
	"github.com/icinga/icingadb/pkg/types"
	"github.com/jhillyerd/enmime"
	"net"
	"net/mail"
)

const (
	EncryptionNone     = "none"
	EncryptionStartTLS = "starttls"
	EncryptionTLS      = "tls"
)

type Email struct {
	Host       string `json:"host"`
	Port       string `json:"port"`
	SenderName string `json:"sender_name"`
	SenderMail string `json:"sender_mail"`
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
		From(ch.SenderName, ch.SenderMail).
		Subject(plugin.FormatSubject(req)).
		Header("Message-Id", fmt.Sprintf("<%s-%s>", uuid.New().String(), ch.SenderMail)).
		Text(msg.Bytes()).
		Send(ch)
}

func (ch *Email) Send(reversePath string, recipients []string, msg []byte) error {
	var (
		client *smtp.Client
		err    error
	)

	switch ch.Encryption {
	case EncryptionStartTLS:
		client, err = smtp.DialStartTLS(ch.GetServer(), nil)
	case EncryptionTLS:
		client, err = smtp.DialTLS(ch.GetServer(), nil)
	case EncryptionNone:
		client, err = smtp.Dial(ch.GetServer())
	default:
		return fmt.Errorf("unsupported mail encryption type %q", ch.Encryption)
	}
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

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

	if (ch.User == "") != (ch.Password == "") {
		return fmt.Errorf("user and password fields must both be set or empty")
	}

	return nil
}

func (ch *Email) GetInfo() *plugin.Info {
	elements := []*plugin.ConfigOption{
		{
			Name: "sender_name",
			Type: "string",
			Label: map[string]string{
				"en_US": "Sender Name",
				"de_DE": "Absendername",
			},
		},
		{
			Name:     "sender_mail",
			Type:     "string",
			Required: true,
			Label: map[string]string{
				"en_US": "Sender Address",
				"de_DE": "Absenderadresse",
			},
			Default: "icinga@example.com",
		},
		{
			Name:     "host",
			Type:     "string",
			Required: true,
			Label: map[string]string{
				"en_US": "SMTP Host",
				"de_DE": "SMTP Host",
			},
		},
		{
			Name:     "port",
			Type:     "number",
			Required: true,
			Label: map[string]string{
				"en_US": "SMTP Port",
				"de_DE": "SMTP Port",
			},
			Min: types.Int{NullInt64: sql.NullInt64{Int64: 1, Valid: true}},
			Max: types.Int{NullInt64: sql.NullInt64{Int64: 65535, Valid: true}},
		},
		{
			Name: "user",
			Type: "string",
			Label: map[string]string{
				"en_US": "SMTP User",
				"de_DE": "SMTP Benutzer",
			},
			Default: "user@example.com",
		},
		{
			Name: "password",
			Type: "secret",
			Label: map[string]string{
				"en_US": "SMTP Password",
				"de_DE": "SMTP Passwort",
			},
		},
		{
			Name:     "encryption",
			Type:     "option",
			Required: true,
			Label: map[string]string{
				"en_US": "SMTP Transport Encryption",
				"de_DE": "SMTP Transportverschlüsselung",
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
