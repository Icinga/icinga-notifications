package main

import (
	"bytes"
	"context"
	"database/sql"
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

	// state is a map of composite keys to State objects, used to track the state of notifications sent to recipients.
	//
	// The composite key is generated using the recipient's email address and the incident ID, ensuring that each
	// recipient-incident combination has a unique state entry. Currently, the only state tracked is the last message
	// ID sent to a recipient for a specific incident, which is used to set the "In-Reply-To" and "References" headers
	// in subsequent emails. We might extend this in the future to include additional state information as needed.
	state map[string]State
	mu    sync.Mutex // Protects access to the above fields.

	// rpcCtx and rpcEp are used to make RPC calls back to Icinga Notifications.
	rpcCtx context.Context
	rpcEp  *jsonrpc.Endpoint
}

// State represents the state of the Email channel, including the last message ID sent to a recipient.
//
// It is used to track the state of notifications sent to a specific recipient and incident combination.
type State struct {
	LastMessageID string `json:"last_message_id"`
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

	if err := ch.mergeState(req.State); err != nil {
		return err
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

	if s, exists := ch.state[compositeKey]; exists {
		b = b.Header("In-Reply-To", s.LastMessageID).Header("References", s.LastMessageID)
	}
	ch.mu.Unlock()

	if err := b.Text(msg.Bytes()).Send(ch); err != nil {
		return err
	}

	if req.Incident.IsRecovered {
		if err := ch.rpcEp.Call(ch.rpcCtx, notifications.MethodDeleteState, compositeKey, nil); err != nil {
			slog.ErrorContext(ch.rpcCtx, "Failed to delete channel state", "error", err)
		}
	} else {
		s := State{LastMessageID: messageID}
		ch.mu.Lock()
		ch.state[compositeKey] = s
		ch.mu.Unlock()
		ch.callUpsertState(compositeKey, s)
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

// callUpsertState is a helper function to call the UpsertState RPC method on the Icinga Notifications.
func (ch *Email) callUpsertState(key string, s State) {
	if err := ch.rpcEp.Call(ch.rpcCtx, notifications.MethodUpsertState, map[string]string{key: jsonMust(s)}, nil); err != nil {
		slog.ErrorContext(ch.rpcCtx, "Failed to upsert channel state", "error", err)
	}
}

// mergeState merges the provided state into the existing state of the Email channel.
func (ch *Email) mergeState(stateM map[string]string) error {
	ch.mu.Lock()
	defer ch.mu.Unlock()

	if ch.state == nil {
		ch.state = make(map[string]State)
	}

	for k, v := range stateM {
		var s State
		if err := json.Unmarshal([]byte(v), &s); err != nil {
			return err
		}
		ch.state[k] = s
	}
	return nil
}

// makeStateKey composes a unique key for the channel state based on the recipient's email address and the incident ID.
func makeStateKey(to *mail.Address, i *plugin.Incident) string {
	return fmt.Sprintf("%s-#%d", to.Address, i.Id)
}

// jsonMust is a helper function that marshals the given value to JSON and panics if it fails.
func jsonMust(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}
