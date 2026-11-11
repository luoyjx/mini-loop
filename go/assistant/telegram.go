package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultTelegramAPI is the public Bot API server.
	DefaultTelegramAPI = "https://api.telegram.org"
	// telegramLongPoll is how long getUpdates waits for a message.
	telegramLongPoll = 25 * time.Second
	// telegramTextLimit stays under the Bot API's 4096 UTF-16 units even
	// when every rune needs two.
	telegramTextLimit = 2000
	// remoteResponseLimit caps a service response the daemon reads.
	remoteResponseLimit = 4 << 20
)

// Telegram is a private chat between the owner and the owner's bot, through
// the Bot API's getUpdates long poll and sendMessage. Only the configured
// private chat is accepted; in a private chat its ID is the owner's user ID.
// The token sits in the request path, so every error is scrubbed of it.
type Telegram struct {
	token  string
	chat   int64
	base   string
	client *http.Client
	wait   time.Duration
}

// NewTelegram checks the token and chat and returns the channel. base is the
// Bot API server; tests and self-hosted Bot API servers set it.
func NewTelegram(token string, chat int64, base string) (*Telegram, error) {
	if token == "" || strings.ContainsAny(token, "/?# \t\r\n") {
		return nil, errors.New("telegram needs a bot token (MINILOOP_TELEGRAM_TOKEN)")
	}
	if chat <= 0 {
		return nil, errors.New("telegram needs your user ID (-telegram-chat); a private chat's ID is your user ID")
	}
	base, err := remoteBase(base, DefaultTelegramAPI)
	if err != nil {
		return nil, fmt.Errorf("telegram: %w", err)
	}
	return &Telegram{token: token, chat: chat, base: base, client: &http.Client{Timeout: telegramLongPoll + 15*time.Second}, wait: telegramLongPoll}, nil
}

func (t *Telegram) Name() string            { return RemoteTelegram }
func (t *Telegram) Label() string           { return "Telegram" }
func (t *Telegram) Account() string         { return fmt.Sprintf("私聊 %d", t.chat) }
func (t *Telegram) Interval() time.Duration { return 0 }

type telegramUpdate struct {
	UpdateID int64            `json:"update_id"`
	Message  *telegramMessage `json:"message"`
}

type telegramMessage struct {
	MessageID int64 `json:"message_id"`
	From      *struct {
		ID int64 `json:"id"`
	} `json:"from"`
	Chat struct {
		ID   int64  `json:"id"`
		Type string `json:"type"`
	} `json:"chat"`
	Text    string           `json:"text"`
	ReplyTo *telegramMessage `json:"reply_to_message"`
}

// Receive long-polls getUpdates from cursor, the next update ID. Passing it
// as the offset confirms everything before it to Telegram.
func (t *Telegram) Receive(ctx context.Context, cursor string) (RemoteBatch, error) {
	var updates []telegramUpdate
	if cursor == "" {
		// offset -1 returns only the newest pending update; starting after
		// it skips whatever waited before the first start.
		if err := t.call(ctx, "getUpdates", map[string]any{"offset": -1, "timeout": 0}, &updates); err != nil {
			return RemoteBatch{}, err
		}
		next := int64(0)
		if len(updates) > 0 {
			next = updates[len(updates)-1].UpdateID + 1
		}
		return RemoteBatch{Cursor: strconv.FormatInt(next, 10)}, nil
	}
	offset, err := strconv.ParseInt(cursor, 10, 64)
	if err != nil {
		return RemoteBatch{}, fmt.Errorf("telegram cursor %q: %w", cursor, err)
	}
	body := map[string]any{"offset": offset, "timeout": int(t.wait / time.Second), "allowed_updates": []string{"message"}}
	if err := t.call(ctx, "getUpdates", body, &updates); err != nil {
		return RemoteBatch{}, err
	}
	batch := RemoteBatch{Cursor: cursor}
	for _, update := range updates {
		batch.Cursor = strconv.FormatInt(update.UpdateID+1, 10)
		message := update.Message
		if message == nil {
			continue
		}
		if message.Chat.Type != "private" || message.Chat.ID != t.chat || message.From == nil || message.From.ID != t.chat {
			batch.Messages = append(batch.Messages, RemoteInbound{Sender: fmt.Sprintf("chat %d", message.Chat.ID), Refused: "不是配置的私聊"})
			continue
		}
		text := strings.TrimSpace(message.Text)
		if text == "/start" || strings.HasPrefix(text, "/start ") {
			// Telegram sends /start when the chat opens.
			continue
		}
		inbound := RemoteInbound{ID: strconv.FormatInt(message.MessageID, 10), Text: text, Sender: "你"}
		if text == "" {
			inbound.Refused = "只支持文字消息"
		}
		if message.ReplyTo != nil {
			inbound.ReplyTo = strconv.FormatInt(message.ReplyTo.MessageID, 10)
		}
		batch.Messages = append(batch.Messages, inbound)
	}
	return batch, nil
}

// Send posts the line as one or more messages.
func (t *Telegram) Send(ctx context.Context, message RemoteOutbound) ([]string, error) {
	ids := []string{}
	for _, part := range splitRunes(message.Text, telegramTextLimit) {
		var sent telegramMessage
		body := map[string]any{"chat_id": t.chat, "text": part, "link_preview_options": map[string]bool{"is_disabled": true}}
		if err := t.call(ctx, "sendMessage", body, &sent); err != nil {
			return ids, err
		}
		ids = append(ids, strconv.FormatInt(sent.MessageID, 10))
	}
	return ids, nil
}

// call posts one Bot API method and decodes its result. A 400 is the
// service refusing this request's content.
func (t *Telegram) call(ctx context.Context, method string, body, result any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, t.base+"/bot"+t.token+"/"+method, bytes.NewReader(payload))
	if err != nil {
		return errors.New(t.scrub(fmt.Sprintf("telegram %s: %v", method, err)))
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := t.client.Do(request)
	if err != nil {
		return errors.New(t.scrub(fmt.Sprintf("telegram %s: %v", method, err)))
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, remoteResponseLimit))
	if err != nil {
		return errors.New(t.scrub(fmt.Sprintf("telegram %s: %v", method, err)))
	}
	var envelope struct {
		OK          bool            `json:"ok"`
		Result      json.RawMessage `json:"result"`
		Description string          `json:"description"`
		ErrorCode   int             `json:"error_code"`
		Parameters  *struct {
			RetryAfter int `json:"retry_after"`
		} `json:"parameters"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return fmt.Errorf("telegram %s: HTTP %d", method, response.StatusCode)
	}
	if !envelope.OK {
		failure := fmt.Errorf("telegram %s: %d %s", method, envelope.ErrorCode, t.scrub(truncateRunes(envelope.Description, 160)))
		switch {
		case envelope.ErrorCode == http.StatusBadRequest && method == "sendMessage":
			return &RemoteRejectedError{failure}
		case envelope.ErrorCode == http.StatusConflict:
			return fmt.Errorf("%w（另一个程序也在用这个 bot 收消息，或者设了 webhook）", failure)
		case envelope.ErrorCode == http.StatusUnauthorized:
			return fmt.Errorf("%w（bot token 不对）", failure)
		case envelope.Parameters != nil && envelope.Parameters.RetryAfter > 0:
			return fmt.Errorf("%w（%d 秒后再试）", failure, envelope.Parameters.RetryAfter)
		}
		return failure
	}
	if err := json.Unmarshal(envelope.Result, result); err != nil {
		return fmt.Errorf("telegram %s: decode result: %w", method, err)
	}
	return nil
}

// scrub removes the token from text that may quote the request URL.
func (t *Telegram) scrub(text string) string {
	return strings.ReplaceAll(text, t.token, "<token>")
}
