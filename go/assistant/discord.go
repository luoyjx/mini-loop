package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultDiscordAPI is Discord's REST API.
	DefaultDiscordAPI = "https://discord.com/api/v10"
	// discordPoll is the pause between DM reads; one read every three
	// seconds stays far inside the route's rate limit.
	discordPoll = 3 * time.Second
	// discordTextLimit stays under Discord's 2000-character message limit.
	discordTextLimit = 1900
	discordUserAgent = "DiscordBot (https://github.com/luoyjx/mini-loop, 1)"
)

// Discord is a DM between the owner and the owner's bot over Discord's REST
// API: the bot opens the DM, reads it every few seconds and posts into it.
// It needs no gateway connection, so it needs no WebSocket library. Bots
// read message content in DMs without the privileged intent. Only the
// configured user's messages are accepted; the bot's own are skipped.
type Discord struct {
	token  string
	user   string
	base   string
	client *http.Client

	mu sync.Mutex
	dm string
}

func NewDiscord(token, user, base string) (*Discord, error) {
	if token == "" || strings.ContainsAny(token, " \t\r\n") {
		return nil, errors.New("discord needs a bot token (MINILOOP_DISCORD_TOKEN)")
	}
	if _, err := strconv.ParseUint(user, 10, 64); err != nil {
		return nil, errors.New("discord needs your numeric user ID (-discord-user)")
	}
	base, err := remoteBase(base, DefaultDiscordAPI)
	if err != nil {
		return nil, fmt.Errorf("discord: %w", err)
	}
	return &Discord{token: token, user: user, base: base, client: &http.Client{Timeout: 20 * time.Second}}, nil
}

func (d *Discord) Name() string            { return RemoteDiscord }
func (d *Discord) Label() string           { return "Discord" }
func (d *Discord) Account() string         { return "私信 用户 " + d.user }
func (d *Discord) Interval() time.Duration { return discordPoll }

type discordMessage struct {
	ID      string `json:"id"`
	Type    int    `json:"type"`
	Content string `json:"content"`
	Author  struct {
		ID  string `json:"id"`
		Bot bool   `json:"bot"`
	} `json:"author"`
	Reference *struct {
		MessageID string `json:"message_id"`
	} `json:"message_reference"`
}

// Receive reads the DM after cursor, the last message ID seen.
func (d *Discord) Receive(ctx context.Context, cursor string) (RemoteBatch, error) {
	dm, err := d.channel(ctx)
	if err != nil {
		return RemoteBatch{}, err
	}
	var messages []discordMessage
	if cursor == "" {
		if err := d.request(ctx, http.MethodGet, "/channels/"+dm+"/messages?limit=1", nil, &messages); err != nil {
			return RemoteBatch{}, err
		}
		head := "0"
		if len(messages) > 0 {
			head = messages[0].ID
		}
		return RemoteBatch{Cursor: head}, nil
	}
	query := url.Values{"after": {cursor}, "limit": {"50"}}
	if err := d.request(ctx, http.MethodGet, "/channels/"+dm+"/messages?"+query.Encode(), nil, &messages); err != nil {
		return RemoteBatch{}, err
	}
	sort.Slice(messages, func(i, j int) bool { return snowflakeLess(messages[i].ID, messages[j].ID) })
	batch := RemoteBatch{Cursor: cursor}
	for _, message := range messages {
		if snowflakeLess(batch.Cursor, message.ID) {
			batch.Cursor = message.ID
		}
		// 0 is a plain message and 19 a reply; others are system notices.
		if message.Author.ID != d.user || (message.Type != 0 && message.Type != 19) {
			if !message.Author.Bot && message.Author.ID != d.user {
				batch.Messages = append(batch.Messages, RemoteInbound{Sender: "用户 " + message.Author.ID, Refused: "不是配置的用户"})
			}
			continue
		}
		inbound := RemoteInbound{ID: message.ID, Text: strings.TrimSpace(message.Content), Sender: "你"}
		if inbound.Text == "" {
			inbound.Refused = "只支持文字消息"
		}
		if message.Reference != nil {
			inbound.ReplyTo = message.Reference.MessageID
		}
		batch.Messages = append(batch.Messages, inbound)
	}
	return batch, nil
}

// Send posts the line into the DM as one or more messages.
func (d *Discord) Send(ctx context.Context, message RemoteOutbound) ([]string, error) {
	dm, err := d.channel(ctx)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for _, part := range splitRunes(message.Text, discordTextLimit) {
		var sent discordMessage
		body := map[string]any{"content": part, "allowed_mentions": map[string][]string{"parse": {}}}
		if err := d.request(ctx, http.MethodPost, "/channels/"+dm+"/messages", body, &sent); err != nil {
			return ids, err
		}
		ids = append(ids, sent.ID)
	}
	return ids, nil
}

// channel opens the DM with the owner once per process.
func (d *Discord) channel(ctx context.Context) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.dm != "" {
		return d.dm, nil
	}
	var dm struct {
		ID string `json:"id"`
	}
	if err := d.request(ctx, http.MethodPost, "/users/@me/channels", map[string]string{"recipient_id": d.user}, &dm); err != nil {
		return "", err
	}
	if dm.ID == "" {
		return "", errors.New("discord: opening the DM returned no channel")
	}
	d.dm = dm.ID
	return d.dm, nil
}

// request calls one REST route. A 400 on a post is the service refusing
// this message's content.
func (d *Discord) request(ctx context.Context, method, path string, body, result any) error {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(payload)
	}
	request, err := http.NewRequestWithContext(ctx, method, d.base+path, reader)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bot "+d.token)
	request.Header.Set("User-Agent", discordUserAgent)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	route := method + " " + strings.SplitN(path, "?", 2)[0]
	response, err := d.client.Do(request)
	if err != nil {
		return fmt.Errorf("discord %s: %w", route, err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, remoteResponseLimit))
	if err != nil {
		return fmt.Errorf("discord %s: %w", route, err)
	}
	if response.StatusCode >= 300 {
		var problem struct {
			Message    string  `json:"message"`
			RetryAfter float64 `json:"retry_after"`
		}
		_ = json.Unmarshal(data, &problem)
		failure := fmt.Errorf("discord %s: HTTP %d %s", route, response.StatusCode, truncateRunes(problem.Message, 160))
		switch response.StatusCode {
		case http.StatusBadRequest:
			if method == http.MethodPost && strings.HasSuffix(route, "/messages") {
				return &RemoteRejectedError{failure}
			}
		case http.StatusUnauthorized:
			return fmt.Errorf("%w（bot token 不对）", failure)
		case http.StatusForbidden:
			return fmt.Errorf("%w（bot 要和你在同一个服务器里，且你允许服务器成员私信）", failure)
		case http.StatusTooManyRequests:
			return fmt.Errorf("%w（%.1f 秒后再试）", failure, problem.RetryAfter)
		}
		return failure
	}
	if result == nil {
		return nil
	}
	if err := json.Unmarshal(data, result); err != nil {
		return fmt.Errorf("discord %s: decode: %w", route, err)
	}
	return nil
}

// snowflakeLess orders Discord IDs, which are decimal integers.
func snowflakeLess(a, b string) bool {
	if len(a) != len(b) {
		return len(a) < len(b)
	}
	return a < b
}
