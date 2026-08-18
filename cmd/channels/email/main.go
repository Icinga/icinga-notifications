package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/mail"
	"sync"

	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"
	"github.com/google/uuid"
	"github.com/icinga/icinga-go-library/notifications"
	"github.com/icinga/icinga-go-library/notifications/jsonrpc"
	"github.com/icinga/icinga-go-library/notifications/plugin"
	"github.com/icinga/icinga-go-library/types"
	"github.com/icinga/icinga-notifications/internal"
	"github.com/jhillyerd/enmime"
)

func main() {
	plugin.Run(&Email{})
}

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
	Password   string `json:"password"` // #nosec G117 -- exported password field
	Encryption string `json:"encryption"`

	mu sync.Mutex // Protects access to the above fields.

	// rpcCtx and rpcEp are used to make RPC calls back to Icinga Notifications.
	rpcCtx context.Context
	rpcEp  *jsonrpc.Endpoint
}

// state represents the state of the Email channel, including the last message ID sent to a recipient.
//
// It is used to track the state of notifications sent to a specific recipient and incident combination.
type state struct {
	LastMessageID string `json:"last_message_id"`
}

func (s state) String() string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// ReceiveEndpoint implements the [plugin.RPCEndpointReceiver] interface.
func (ch *Email) ReceiveEndpoint(ctx context.Context, ep *jsonrpc.Endpoint) {
	ch.rpcCtx = ctx
	ch.rpcEp = ep
}

func (ch *Email) GetInfo() *plugin.Info {
	configAttrs := plugin.ConfigOptions{
		{
			Name: "host",
			Type: "string",
			Label: map[string]string{
				"en_US": "SMTP Host",
				"de_DE": "SMTP Host",
			},
			Required: true,
		},
		{
			Name: "port",
			Type: "number",
			Label: map[string]string{
				"en_US": "SMTP Port",
				"de_DE": "SMTP Port",
			},
			Required: true,
			Min:      types.Int{NullInt64: sql.NullInt64{Int64: 1, Valid: true}},
			Max:      types.Int{NullInt64: sql.NullInt64{Int64: 65535, Valid: true}},
		},
		{
			Name: "sender_name",
			Type: "string",
			Label: map[string]string{
				"en_US": "Sender Name",
				"de_DE": "Absendername",
			},
			Default:  "Icinga",
			Required: true,
		},
		{
			Name: "sender_mail",
			Type: "string",
			Label: map[string]string{
				"en_US": "Sender Address",
				"de_DE": "Absenderadresse",
			},
			Required: true,
		},
		{
			Name: "user",
			Type: "string",
			Label: map[string]string{
				"en_US": "SMTP User",
				"de_DE": "SMTP Benutzer",
			},
			Help: map[string]string{
				"en_US": "When configuring an SMTP user, an SMTP password must also be set.",
				"de_DE": "Das Setzen eines SMTP Benutzers erfordert ebenfalls ein SMTP Passwort.",
			},
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

	return &plugin.Info{
		Name:             "Email",
		Version:          internal.Version.Version,
		Author:           "Icinga GmbH",
		ConfigAttributes: configAttrs,
	}
}

func (ch *Email) SetConfig(jsonStr json.RawMessage) error {
	var tmpEm Email
	err := plugin.PopulateDefaults(&tmpEm)
	if err != nil {
		return err
	}

	err = json.Unmarshal(jsonStr, &tmpEm)
	if err != nil {
		return fmt.Errorf("failed to load config: %s %w", jsonStr, err)
	}

	if (tmpEm.User == "") != (tmpEm.Password == "") {
		return fmt.Errorf("user and password fields must both be set or empty")
	}

	ch.mu.Lock()
	defer ch.mu.Unlock()

	ch.Host = tmpEm.Host
	ch.Port = tmpEm.Port
	ch.SenderName = tmpEm.SenderName
	ch.SenderMail = tmpEm.SenderMail
	ch.User = tmpEm.User
	ch.Password = tmpEm.Password
	ch.Encryption = tmpEm.Encryption

	return nil
}

func (ch *Email) SendNotification(req *plugin.NotificationRequest) error {
	var to *mail.Address
	for _, address := range req.Contact.Addresses {
		if address.Type == "email" {
			to = &mail.Address{Name: req.Contact.FullName, Address: address.Address}
			break
		}
	}

	if to == nil {
		return fmt.Errorf("contact user %s does not have an e-mail address", req.Contact.FullName)
	}

	var msg bytes.Buffer
	plugin.FormatMessage(&msg, req)

	compositeKey := makeStateKey(to, req.Incident)

	ch.mu.Lock()
	messageID := fmt.Sprintf("<%s-%s>", uuid.New().String(), ch.SenderMail)
	b := enmime.Builder().
		ToAddrs([]mail.Address{*to}).
		From(ch.SenderName, ch.SenderMail).
		Subject(plugin.FormatSubject(req)).
		Header("Message-Id", messageID)
	ch.mu.Unlock()

	for _, ss := range req.States {
		if ss.Key != compositeKey {
			continue
		}
		var s state
		if err := json.Unmarshal([]byte(ss.Value), &s); err != nil {
			return err
		}
		b = b.Header("In-Reply-To", s.LastMessageID).Header("References", s.LastMessageID)
	}

	if err := b.Text(msg.Bytes()).Send(ch); err != nil {
		return err
	}

	if req.Incident != nil && req.Incident.IsRecovered {
		if err := ch.rpcEp.Call(ch.rpcCtx, notifications.MethodDeleteState, []plugin.State{{Key: compositeKey}}, nil); err != nil {
			slog.ErrorContext(ch.rpcCtx, "Failed to delete channel state", "error", err)
		}
	} else if req.Incident != nil {
		ss := []plugin.State{
			{Key: compositeKey, Value: state{LastMessageID: messageID}.String()},
		}
		if err := ch.rpcEp.Call(ch.rpcCtx, notifications.MethodUpsertState, ss, nil); err != nil {
			slog.ErrorContext(ch.rpcCtx, "Failed to upsert channel state", "error", err)
		}
	}

	return nil
}

// Send implements the enmime.Sender interface.
func (ch *Email) Send(reversePath string, recipients []string, msg []byte) error {
	var (
		client *smtp.Client
		err    error
	)

	ch.mu.Lock()
	serverAddr := net.JoinHostPort(ch.Host, ch.Port)
	encryption := ch.Encryption
	password := ch.Password
	username := ch.User
	ch.mu.Unlock()

	switch encryption {
	case EncryptionStartTLS:
		client, err = smtp.DialStartTLS(serverAddr, nil)
	case EncryptionTLS:
		client, err = smtp.DialTLS(serverAddr, nil)
	case EncryptionNone:
		client, err = smtp.Dial(serverAddr)
	default:
		return fmt.Errorf("unsupported mail encryption type %q", encryption)
	}
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

	if password != "" {
		if err = client.Auth(sasl.NewPlainClient("", username, password)); err != nil {
			return err
		}
	}

	if err := client.SendMail(reversePath, recipients, bytes.NewReader(msg)); err != nil {
		return err
	}

	return client.Quit()
}

// makeStateKey composes a unique key for the channel state based on the recipient's email address and the incident ID.
func makeStateKey(to *mail.Address, i *plugin.Incident) string {
	if i == nil {
		return ""
	}
	h := sha256.New()
	if err := binary.Write(h, binary.BigEndian, i.Id); err != nil {
		return ""
	}
	if err := binary.Write(h, binary.BigEndian, []byte(to.Address)); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}
