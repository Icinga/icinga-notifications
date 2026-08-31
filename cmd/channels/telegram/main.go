package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/icinga/icinga-go-library/notifications"
	"github.com/icinga/icinga-go-library/notifications/jsonrpc"
	"github.com/icinga/icinga-go-library/notifications/plugin"
	"github.com/icinga/icinga-go-library/utils"
	"github.com/icinga/icinga-notifications/internal"
)

func main() {
	plugin.Run(&Telegram{})
}

type Telegram struct {
	BotToken string `json:"bot_token"`

	mu sync.Mutex

	rpcCtx context.Context
	rpcEp  *jsonrpc.Endpoint
}

// state represents the state of the Telegram channel, holding the last message sent to a chat for this incident.
type state struct {
	MessageID int64 `json:"message_id"`
}

func (s state) String() string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// replyParameters describes the message a sendMessage request replies to.
// API Doc: https://core.telegram.org/bots/api#replyparameters
type replyParameters struct {
	MessageID int64 `json:"message_id"`

	// AllowSendingWithoutReply lets Telegram send the message even if the replied to message is gone
	// (if original message was deleted).
	AllowSendingWithoutReply bool `json:"allow_sending_without_reply"`
}

// ReceiveEndpoint implements the [plugin.RPCEndpointReceiver] interface.
func (ch *Telegram) ReceiveEndpoint(ctx context.Context, ep *jsonrpc.Endpoint) {
	ch.rpcCtx = ctx
	ch.rpcEp = ep
}

func (ch *Telegram) GetInfo() *plugin.Info {
	configAttrs := plugin.ConfigOptions{
		{
			Name: "bot_token",
			Type: "secret",
			Label: map[string]string{
				"en_US": "Bot token",
				"de_DE": "Bot Token",
			},
			Help: map[string]string{
				"en_US": "Telegram bot API token from BotFather",
				"de_DE": "Telegram Bot API Token von BotFather",
			},
			Required: true,
		},
	}

	return &plugin.Info{
		Name:             "Telegram",
		Version:          internal.Version.Version,
		Author:           "Icinga GmbH",
		ConfigAttributes: configAttrs,
	}
}

func (ch *Telegram) SetConfig(jsonStr json.RawMessage) error {
	var tmp Telegram
	if err := json.Unmarshal(jsonStr, &tmp); err != nil {
		return fmt.Errorf("could not unmarshal configuration: %w", err)
	}

	ch.mu.Lock()
	defer ch.mu.Unlock()
	ch.BotToken = tmp.BotToken

	return nil
}

func (ch *Telegram) SendNotification(req *plugin.NotificationRequest) error {
	var chatID string
	for _, address := range req.Contact.Addresses {
		if address.Type == "telegram" {
			chatID = address.Address
			break
		}
	}
	if chatID == "" {
		return fmt.Errorf("contact %q has no Telegram address", req.Contact.FullName)
	}

	var output bytes.Buffer
	_, _ = fmt.Fprint(&output, plugin.FormatSubject(req)+"\n\n")
	plugin.FormatMessage(&output, req)

	message := struct {
		ChatID                string           `json:"chat_id"`
		Text                  string           `json:"text"`
		DisableWebPagePreview bool             `json:"disable_web_page_preview"`
		ReplyParameters       *replyParameters `json:"reply_parameters,omitempty"`
	}{
		ChatID: chatID,
		// Telegram limits sendMessage "text" field to 4096 characters.
		// https://core.telegram.org/bots/api#sendmessage
		Text:                  utils.Ellipsize(output.String(), 4096),
		DisableWebPagePreview: true,
	}

	// When this chat has already been notified about this incident, reply to the last message sent for it.
	stateKey := makeStateKey(chatID, req.Incident)
	var knownThread bool

	for _, s := range req.States {
		if stateKey == "" || s.Key != stateKey {
			continue
		}

		var st state
		if err := json.Unmarshal([]byte(s.Value), &st); err != nil {
			return fmt.Errorf("cannot unmarshal channel state %q: %w", s.Key, err)
		}
		knownThread = true
		message.ReplyParameters = &replyParameters{MessageID: st.MessageID, AllowSendingWithoutReply: true}
		break
	}

	messageID, err := ch.sendMessage(message)
	if err != nil {
		return err
	}

	if req.Incident == nil || stateKey == "" {
		return nil
	}

	if req.Incident.IsRecovered && knownThread {
		// The thread is done, no further notifications will reference it.
		states := []plugin.State{{Key: stateKey}}
		if err := ch.rpcEp.Call(ch.rpcCtx, notifications.MethodDeleteState, states, nil); err != nil {
			slog.ErrorContext(ch.rpcCtx, "Failed to delete channel state", "error", err)
		}
	} else if !req.Incident.IsRecovered {
		// Remember this message so that the next notification for this incident replies to it.
		states := []plugin.State{{Key: stateKey, Value: state{MessageID: messageID}.String()}}
		if err := ch.rpcEp.Call(ch.rpcCtx, notifications.MethodUpsertState, states, nil); err != nil {
			slog.ErrorContext(ch.rpcCtx, "Failed to upsert channel state", "error", err)
		}
	}

	return nil
}

// sendMessage posts the given message to the Telegram bot API and returns the message id.
func (ch *Telegram) sendMessage(message any) (int64, error) {
	body, err := json.Marshal(message)
	if err != nil {
		return 0, err
	}

	ch.mu.Lock()
	token := ch.BotToken
	ch.mu.Unlock()

	// The Telegram bot API carries a bot token in the request path,
	// so every request is of the form
	// https://api.telegram.org/bot<token>/method.
	// API Doc: https://core.telegram.org/bots/api#making-requests
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", token)
	client := &http.Client{Timeout: 10 * time.Second}
	//nolint:bodyclose // False positive, drainAndClose is called in the defer statement below.
	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("error while sending http request to Telegram: %w", redactToken(err, token))
	}
	defer drainAndClose(resp.Body)

	var response struct {
		Ok          bool   `json:"ok"`
		Description string `json:"description,omitempty"`
		Result      struct {
			MessageID int64 `json:"message_id"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return 0, fmt.Errorf("cannot decode Telegram response for HTTP status %q: %w", resp.Status, err)
	}
	if resp.StatusCode != http.StatusOK || !response.Ok {
		return 0, fmt.Errorf("Telegram rejected the message with HTTP status %q and description %q",
			resp.Status, response.Description)
	}

	return response.Result.MessageID, nil
}

// redactToken removes the bot token from the request URL an url.Error carries, as this error would leak
// the token into the daemons log:
// https://github.com/golang/go/issues/44819
func redactToken(err error, token string) error {
	var urlErr *url.Error
	if token != "" && errors.As(err, &urlErr) {
		urlErr.URL = strings.ReplaceAll(urlErr.URL, token, "<redacted>")
	}
	return err
}

// makeStateKey composes a unique key for the channel state based on the recipients chat ID and the incident ID.
func makeStateKey(chatID string, i *plugin.Incident) string {
	if i == nil {
		return ""
	}
	h := sha256.New()
	if err := binary.Write(h, binary.BigEndian, i.Id); err != nil {
		return ""
	}
	if err := binary.Write(h, binary.BigEndian, []byte(chatID)); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

func drainAndClose(r io.ReadCloser) {
	_, _ = io.Copy(io.Discard, r)
	_ = r.Close()
}
