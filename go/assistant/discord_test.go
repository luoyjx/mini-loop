package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
)

const (
	testDiscordToken = "discord-secret"
	testDiscordUser  = "1001"
	testDiscordBot   = "2002"
)

// fakeDiscord serves the DM routes for one bot and one user.
type fakeDiscord struct {
	mu       sync.Mutex
	messages []map[string]any
	afters   []string
	opened   int
	sendCode int
	nextID   int
}

func (f *fakeDiscord) add(id, author string, bot bool, kind int, content string) map[string]any {
	message := map[string]any{"id": id, "type": kind, "content": content, "author": map[string]any{"id": author, "bot": bot}}
	f.messages = append(f.messages, message)
	return message
}

func (f *fakeDiscord) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bot "+testDiscordToken || !strings.HasPrefix(r.Header.Get("User-Agent"), "DiscordBot (") {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{"message": "401: Unauthorized"})
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/users/@me/channels":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["recipient_id"] != testDiscordUser {
				t.Errorf("DM opened with %q", body["recipient_id"])
			}
			f.opened++
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "dm-1"})
		case r.Method == http.MethodGet && r.URL.Path == "/channels/dm-1/messages":
			after := r.URL.Query().Get("after")
			f.afters = append(f.afters, after)
			result := []map[string]any{}
			for _, message := range f.messages {
				if after == "" || snowflakeLess(after, message["id"].(string)) {
					result = append(result, message)
				}
			}
			// Discord lists newest first.
			sort.Slice(result, func(i, j int) bool { return snowflakeLess(result[j]["id"].(string), result[i]["id"].(string)) })
			if after == "" && len(result) > 1 {
				result = result[:1]
			}
			_ = json.NewEncoder(w).Encode(result)
		case r.Method == http.MethodPost && r.URL.Path == "/channels/dm-1/messages":
			if f.sendCode != 0 {
				w.WriteHeader(f.sendCode)
				_ = json.NewEncoder(w).Encode(map[string]any{"message": "Cannot send messages to this user", "retry_after": 1.5})
				return
			}
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if mentions := body["allowed_mentions"].(map[string]any)["parse"].([]any); len(mentions) != 0 {
				t.Errorf("mentions are off: %v", mentions)
			}
			f.nextID++
			id := "30000000000000000" + string(rune('0'+f.nextID))
			f.add(id, testDiscordBot, true, 0, body["content"].(string))
			_ = json.NewEncoder(w).Encode(map[string]any{"id": id})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

func TestDiscordReadsTheDMAndSkipsItsOwnMessages(t *testing.T) {
	api := &fakeDiscord{}
	api.add("100000000000000001", testDiscordUser, false, 0, "history")
	server := httptest.NewServer(api.handler(t))
	defer server.Close()
	discord, err := NewDiscord(testDiscordToken, testDiscordUser, server.URL)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	batch, err := discord.Receive(ctx, "")
	if err != nil || batch.Cursor != "100000000000000001" || len(batch.Messages) != 0 {
		t.Fatalf("a first start pins the newest message: %+v %v", batch, err)
	}
	ids, err := discord.Send(ctx, RemoteOutbound{Text: "❓ 周二还是周三？"})
	if err != nil || len(ids) != 1 {
		t.Fatalf("send %v %v", ids, err)
	}
	reply := api.add("200000000000000003", testDiscordUser, false, 19, " 周三 ")
	reply["message_reference"] = map[string]any{"message_id": ids[0]}
	api.add("200000000000000002", testDiscordUser, false, 0, "记住 周五不开会")
	api.add("200000000000000004", testDiscordUser, false, 0, "")
	api.add("200000000000000005", testDiscordUser, false, 7, "joined")
	api.add("200000000000000006", "3003", false, 0, "a stranger")
	batch, err = discord.Receive(ctx, batch.Cursor)
	if err != nil {
		t.Fatal(err)
	}
	want := []RemoteInbound{
		{ID: "200000000000000002", Text: "记住 周五不开会", Sender: "你"},
		{ID: "200000000000000003", Text: "周三", ReplyTo: ids[0], Sender: "你"},
		{ID: "200000000000000004", Sender: "你", Refused: "只支持文字消息"},
		{Sender: "用户 3003", Refused: "不是配置的用户"},
	}
	if len(batch.Messages) != len(want) {
		t.Fatalf("messages %+v", batch.Messages)
	}
	for i := range want {
		if batch.Messages[i] != want[i] {
			t.Errorf("message %d: %+v, want %+v", i, batch.Messages[i], want[i])
		}
	}
	if batch.Cursor != "300000000000000001" {
		t.Fatalf("the cursor is the newest ID, the bot's own included: %q", batch.Cursor)
	}
	if api.opened != 1 {
		t.Fatalf("the DM is opened once: %d", api.opened)
	}
}

func TestDiscordSendClassifiesFailures(t *testing.T) {
	api := &fakeDiscord{}
	server := httptest.NewServer(api.handler(t))
	defer server.Close()
	discord, err := NewDiscord(testDiscordToken, testDiscordUser, server.URL)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	ids, err := discord.Send(ctx, RemoteOutbound{Text: strings.Repeat("长", discordTextLimit+1)})
	if err != nil || len(ids) != 2 {
		t.Fatalf("a long line takes two messages: %v %v", ids, err)
	}
	var rejected *RemoteRejectedError
	for code, want := range map[int]string{http.StatusForbidden: "同一个服务器", http.StatusTooManyRequests: "1.5 秒后再试"} {
		api.sendCode = code
		_, err := discord.Send(ctx, RemoteOutbound{Text: "x"})
		if err == nil || !strings.Contains(err.Error(), want) || errors.As(err, &rejected) {
			t.Fatalf("%d: %v", code, err)
		}
	}
	api.sendCode = http.StatusBadRequest
	if _, err := discord.Send(ctx, RemoteOutbound{Text: "x"}); !errors.As(err, &rejected) {
		t.Fatalf("a 400 on a post is the service refusing this message: %v", err)
	}
	wrong, _ := NewDiscord("wrong", testDiscordUser, server.URL)
	if _, err := wrong.Receive(ctx, "1"); err == nil || !strings.Contains(err.Error(), "token") {
		t.Fatalf("a bad token says so: %v", err)
	}
}

func TestNewDiscordChecksItsSettings(t *testing.T) {
	for name, build := range map[string]func() (*Discord, error){
		"no token":  func() (*Discord, error) { return NewDiscord("", testDiscordUser, "") },
		"no user":   func() (*Discord, error) { return NewDiscord(testDiscordToken, "", "") },
		"user name": func() (*Discord, error) { return NewDiscord(testDiscordToken, "alice", "") },
		"plain http": func() (*Discord, error) {
			return NewDiscord(testDiscordToken, testDiscordUser, "http://discord.example.test")
		},
		"token spaces": func() (*Discord, error) { return NewDiscord("a b", testDiscordUser, "") },
	} {
		if _, err := build(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestSnowflakesOrderNumerically(t *testing.T) {
	if !snowflakeLess("99", "100") || snowflakeLess("100", "99") || snowflakeLess("5", "5") {
		t.Fatal("snowflakes compare as numbers")
	}
}
