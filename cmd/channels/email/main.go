package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/mail"
	"strconv"
	"sync"

	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"
	"github.com/google/uuid"
	"github.com/icinga/icinga-go-library/config"
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

	AuthMethodNone = "none"
)

type Email struct {
	Host       string `json:"host"`
	Port       string `json:"port"`
	SenderName string `json:"sender_name"`
	SenderMail string `json:"sender_mail"`
	User       string `json:"user"`
	Password   string `json:"password"` // #nosec G117 -- exported password field
	Encryption string `json:"encryption"`
	// AuthMethod is a SASL Authentication Mechanism.
	AuthMethod           string `json:"auth_method"`
	AuthExternalIdentity string `json:"auth_external_identity"`

	TlsServerName string `json:"tls_server_name"`
	TlsCert       string `json:"tls_cert"`
	TlsKey        string `json:"tls_key"`
	TlsCa         string `json:"tls_ca"`
	TlsInsecure   bool   `json:"tls_insecure"`

	tlsConf *tls.Config

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
			Children: []plugin.ChildOption{
				{
					Name: "tls_server_name",
					Type: "string",
					Label: map[string]string{
						"en_US": "TLS Server Name",
						"de_DE": "TLS-Servername",
					},
					Help: map[string]string{
						"en_US": "Use this server name for the TLS handshake instead of the SMTP host.",
						"de_DE": "Verwende diesen Servernamen anstelle des SMTP Hosts für den TLS-Handshake.",
					},
					ParentValues: []any{EncryptionStartTLS, EncryptionTLS},
				},
				{
					Name: "tls_cert",
					Type: "text",
					Label: map[string]string{
						"en_US": "TLS Client Certificate",
						"de_DE": "TLS Client-Zertifikat",
					},
					Help: map[string]string{
						"en_US": "If set, use this client certificate. Either a file path to a PEM file or a PEM block. Requires TLS Client Key.",
						"de_DE": "Falls gesetzt, verwende dieses Client-Zertifikat. Entweder ein Dateipfad zu einer PEM-Datei oder ein PEM-Block. Benötigt TLS Client-Schlüssel.",
					},
					ParentValues: []any{EncryptionStartTLS, EncryptionTLS},
				},
				{
					Name: "tls_key",
					Type: "text",
					Label: map[string]string{
						"en_US": "TLS Client Key",
						"de_DE": "TLS Client-Schlüssel",
					},
					Help: map[string]string{
						"en_US": "If set, use this client key. Either a file path to a PEM file or a PEM block.",
						"de_DE": "Falls gesetzt, verwende diesen Client-Schlüssel. Entweder ein Dateipfad zu einer PEM-Datei oder ein PEM-Block.",
					},
					ParentValues: []any{EncryptionStartTLS, EncryptionTLS},
				},
				{
					Name: "tls_ca",
					Type: "text",
					Label: map[string]string{
						"en_US": "TLS CA Certificate",
						"de_DE": "TLS CA-Zertifikat",
					},
					Help: map[string]string{
						"en_US": "If set, use a custom CA instead of the OS CA store. Either a file path to a PEM file or a PEM block.",
						"de_DE": "Falls gesetzt, verwende diese CA anstelle des OS CA-Stores. Entweder ein Dateipfad zu einer PEM-Datei oder ein PEM-Block.",
					},
					ParentValues: []any{EncryptionStartTLS, EncryptionTLS},
				},
				{
					Name: "tls_insecure",
					Type: "bool",
					Label: map[string]string{
						"en_US": "No TLS Verification",
						"de_DE": "Keine TLS-Verifizierung",
					},
					Help: map[string]string{
						"en_US": "Skip TLS verification. This might be insecure!",
						"de_DE": "Führe keine TLS-Verifizierung durch. Dies vermag unsicher zu sein!",
					},
					ParentValues: []any{EncryptionStartTLS, EncryptionTLS},
				},
			},
		},
		{
			Name:     "auth_method",
			Type:     "option",
			Required: true,
			Label: map[string]string{
				"en_US": "SMTP Authentication Method",
				"de_DE": "SMTP Authentifizierungsmethode",
			},
			Help: map[string]string{
				"en_US": "The method has to be supported by the SMTP server. PLAIN and LOGIN require the SMTP user " +
					"and password, while OAUTHBEARER expects a bearer token in the SMTP password field. " +
					"EXTERNAL uses TLS client certificates, and requires a transport encryption. " +
					"If the NONE method is chosen, the SMTP server will be contacted without authentication.",
				"de_DE": "Die Methode muss vom SMTP Server unterstützt werden. PLAIN und LOGIN benötigen den SMTP " +
					"Benutzer und das Passwort, während OAUTHBEARER ein Bearer Token im SMTP Passwortfeld erwartet. " +
					"EXTERNAL verwendet TLS-Client-Zertifikate und benötigt somit eine Transportverschlüsselung. " +
					"Ist die NONE Methode gewählt, wird der SMTP Server ohne Authentifizierung kontaktiert.",
			},
			Options: map[string]string{
				AuthMethodNone:   "NONE",
				sasl.Plain:       sasl.Plain,
				sasl.Login:       sasl.Login,
				sasl.OAuthBearer: sasl.OAuthBearer,
				sasl.External:    sasl.External,
			},
			Children: []plugin.ChildOption{
				{
					Name:     "user",
					Type:     "string",
					Required: true,
					Label: map[string]string{
						"en_US": "SMTP User",
						"de_DE": "SMTP Benutzer",
					},
					Help: map[string]string{
						"en_US": "When configuring an SMTP user, an SMTP password must also be set.",
						"de_DE": "Das Setzen eines SMTP Benutzers erfordert ebenfalls ein SMTP Passwort.",
					},
					ParentValues: []any{sasl.Plain, sasl.Login, sasl.OAuthBearer},
				},
				{
					Name:     "password",
					Type:     "secret",
					Required: true,
					Label: map[string]string{
						"en_US": "SMTP Password",
						"de_DE": "SMTP Passwort",
					},
					ParentValues: []any{sasl.Plain, sasl.Login, sasl.OAuthBearer},
				},
				{
					Name: "auth_external_identity",
					Type: "string",
					Label: map[string]string{
						"en_US": "SASL Identity",
						"de_DE": "SASL-Identität",
					},
					Help: map[string]string{
						"en_US": "Optional SASL Identity instead of what the client certificate provides.",
						"de_DE": "Optionale SASL-Identität anstelle der Informationen aus dem Client-Zertifikat.",
					},
					ParentValues: []any{sasl.External},
				},
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

	// Channels configured before the authentication method became selectable carry no auth_method field and were
	// authenticated with PLAIN whenever an SMTP password was set.
	if tmpEm.AuthMethod == "" && (tmpEm.User != "" || tmpEm.Password != "") {
		tmpEm.AuthMethod = sasl.Plain
	}

	if tmpEm.AuthMethod != "" {
		switch tmpEm.AuthMethod {
		case AuthMethodNone:
			// Nothing to validate for this method.
		case sasl.Plain, sasl.Login, sasl.OAuthBearer:
			// All three carry a username and a secret, the latter being a bearer token for OAUTHBEARER.
			if tmpEm.User == "" || tmpEm.Password == "" {
				return fmt.Errorf("user and password fields are required for the %s authentication method", tmpEm.AuthMethod)
			}
		case sasl.External:
			if tmpEm.Encryption != EncryptionStartTLS && tmpEm.Encryption != EncryptionTLS {
				return fmt.Errorf("SASL EXTERNAL requires either STARTTLS or TLS as the transport encryption")
			}
			if tmpEm.TlsCert == "" || tmpEm.TlsKey == "" {
				return fmt.Errorf("client certificate and key are required for SASL EXTERNAL")
			}
		default:
			return fmt.Errorf("unsupported SMTP authentication method %q", tmpEm.AuthMethod)
		}
	}

	baseTlsConf := config.TLS{
		Enable:   tmpEm.Encryption == EncryptionStartTLS || tmpEm.Encryption == EncryptionTLS,
		Cert:     tmpEm.TlsCert,
		Key:      tmpEm.TlsKey,
		Ca:       tmpEm.TlsCa,
		Insecure: tmpEm.TlsInsecure,
	}
	serverName := tmpEm.Host
	if tmpEm.TlsServerName != "" {
		serverName = tmpEm.TlsServerName
	}
	tlsConf, err := baseTlsConf.MakeConfig(serverName)
	if err != nil {
		return fmt.Errorf("cannot create TLS configuration: %w", err)
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
	ch.AuthMethod = tmpEm.AuthMethod
	ch.AuthExternalIdentity = tmpEm.AuthExternalIdentity
	ch.TlsServerName = tmpEm.TlsServerName
	ch.TlsCert = tmpEm.TlsCert
	ch.TlsKey = tmpEm.TlsKey
	ch.TlsCa = tmpEm.TlsCa
	ch.TlsInsecure = tmpEm.TlsInsecure
	ch.tlsConf = tlsConf

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
	host, port := ch.Host, ch.Port
	serverAddr := net.JoinHostPort(host, port)
	encryption := ch.Encryption
	password := ch.Password
	username := ch.User
	authMethod := ch.AuthMethod
	authExternalIdentity := ch.AuthExternalIdentity
	tlsConf := ch.tlsConf
	ch.mu.Unlock()

	switch encryption {
	case EncryptionStartTLS:
		client, err = smtp.DialStartTLS(serverAddr, tlsConf)
	case EncryptionTLS:
		client, err = smtp.DialTLS(serverAddr, tlsConf)
	case EncryptionNone:
		client, err = smtp.Dial(serverAddr)
	default:
		return fmt.Errorf("unsupported mail encryption type %q", encryption)
	}
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

	if authMethod != "" && authMethod != AuthMethodNone {
		var auth sasl.Client
		switch authMethod {
		case sasl.Plain:
			auth = sasl.NewPlainClient("", username, password)
		case sasl.Login:
			auth = sasl.NewLoginClient(username, password)
		case sasl.OAuthBearer:
			smtpPort, err := strconv.Atoi(port)
			if err != nil {
				return fmt.Errorf("invalid SMTP port %q: %w", port, err)
			}

			auth = sasl.NewOAuthBearerClient(&sasl.OAuthBearerOptions{
				Username: username,
				Token:    password,
				Host:     host,
				Port:     smtpPort,
			})
		case sasl.External:
			auth = sasl.NewExternalClient(authExternalIdentity)
		default:
			return fmt.Errorf("unsupported SMTP authentication method %q", authMethod)
		}

		if err := client.Auth(auth); err != nil {
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
