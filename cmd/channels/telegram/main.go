package main

import (
	"bytes"
	"context"
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

	"github.com/icinga/icinga-go-library/notifications/jsonrpc"
	"github.com/icinga/icinga-go-library/notifications/plugin"
	"github.com/icinga/icinga-go-library/utils"
	"github.com/icinga/icinga-notifications/internal"
	"go.uber.org/zap/zapcore"
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

func (ch *Telegram) SendNotification(req *plugin.NotificationRequest) (*plugin.DeliveryResult, error) {
	if len(req.Contact.Addresses) == 0 {
		return nil, fmt.Errorf("contact %q has no Telegram address", req.Contact.FullName)
	}
	chatID := req.Contact.Addresses[0].Address

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
		Text:                  utils.EllipsizeRunes(output.String(), 4096),
		DisableWebPagePreview: true,
	}

	// When this chat has already been notified about this incident, reply to the last message sent for it.
	if req.State.Value != "" {
		var st state
		if err := json.Unmarshal([]byte(req.State.Value), &st); err != nil {
			ch.notifyLog(zapcore.WarnLevel, "Failed to unmarshal channel state",
				"state_key", req.State.Key, "state_value", req.State.Value, "error", err)
		} else {
			message.ReplyParameters = &replyParameters{MessageID: st.MessageID, AllowSendingWithoutReply: true}
		}
	}

	messageID, err := ch.sendMessage(message)
	if err != nil {
		return nil, err
	}

	if req.Incident != nil && !req.Incident.IsRecovered {
		// Remember this message so that the next notification for this incident replies to it.
		return &plugin.DeliveryResult{
			State: plugin.State{
				Key:   req.State.Key,
				Value: state{MessageID: messageID}.String(),
			},
		}, nil
	}

	return nil, nil
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

// notifyLog logs a message to the notification log, if the RPC endpoint is available.
func (ch *Telegram) notifyLog(lvl zapcore.Level, msg string, fields ...any) {
	if ch.rpcCtx == nil || ch.rpcEp == nil {
		slog.Error("RPC endpoint not available, cannot log notification message", "message", msg)
		return
	}
	if err := ch.rpcEp.NotifyLog(ch.rpcCtx, lvl, msg, fields...); err != nil {
		slog.ErrorContext(ch.rpcCtx, "Failed to log notification message", "error", err)
	}
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

func drainAndClose(r io.ReadCloser) {
	_, _ = io.Copy(io.Discard, r)
	_ = r.Close()
}
