package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

const testTelegramToken = "123456:secret-token"

// fakeBotAPI serves getUpdates and sendMessage for one token.
type fakeBotAPI struct {
	mu       sync.Mutex
	updates  []map[string]any
	offsets  []float64
	sent     []map[string]any
	sendCode int
	pollCode int
}

func (f *fakeBotAPI) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, ok := strings.CutPrefix(r.URL.Path, "/bot"+testTelegramToken+"/")
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error_code": 404, "description": "Not Found"})
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode %s: %v", method, err)
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		reply := func(code int, result any) {
			if code != 0 {
				w.WriteHeader(code)
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error_code": code, "description": "Bad Request: refused for " + testTelegramToken})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": result})
		}
		switch method {
		case "getUpdates":
			offset := body["offset"].(float64)
			f.offsets = append(f.offsets, offset)
			if f.pollCode != 0 {
				reply(f.pollCode, nil)
				return
			}
			result := []map[string]any{}
			for _, update := range f.updates {
				id := update["update_id"].(int)
				if offset == -1 || float64(id) >= offset {
					result = append(result, update)
				}
			}
			if offset == -1 && len(result) > 1 {
				result = result[len(result)-1:]
			}
			reply(0, result)
		case "sendMessage":
			if f.sendCode != 0 {
				reply(f.sendCode, nil)
				return
			}
			f.sent = append(f.sent, body)
			reply(0, map[string]any{"message_id": 900 + len(f.sent)})
		default:
			reply(http.StatusNotFound, nil)
		}
	})
}

func privateMessage(update, message int, from int64, text string) map[string]any {
	return map[string]any{"update_id": update, "message": map[string]any{
		"message_id": message, "from": map[string]any{"id": from}, "chat": map[string]any{"id": from, "type": "private"}, "text": text}}
}

func TestTelegramReceivesOnlyTheOwnersPrivateChat(t *testing.T) {
	api := &fakeBotAPI{updates: []map[string]any{privateMessage(10, 1, 42, "old message")}}
	server := httptest.NewServer(api.handler(t))
	defer server.Close()
	telegram, err := NewTelegram(testTelegramToken, 42, server.URL)
	if err != nil {
		t.Fatal(err)
	}
	telegram.wait = 0
	ctx := context.Background()
	batch, err := telegram.Receive(ctx, "")
	if err != nil || batch.Cursor != "11" || len(batch.Messages) != 0 {
		t.Fatalf("a first start skips pending updates: %+v %v", batch, err)
	}

	reply := privateMessage(14, 6, 42, "周三")
	reply["message"].(map[string]any)["reply_to_message"] = map[string]any{"message_id": 901, "chat": map[string]any{"id": 42, "type": "private"}}
	group := privateMessage(13, 5, 42, "in a group")
	group["message"].(map[string]any)["chat"] = map[string]any{"id": -100, "type": "group"}
	photo := privateMessage(15, 7, 42, "")
	api.updates = append(api.updates,
		privateMessage(11, 2, 42, "/start"),
		privateMessage(12, 3, 7, "a stranger"),
		group, reply, photo,
		map[string]any{"update_id": 16},
	)
	batch, err = telegram.Receive(ctx, batch.Cursor)
	if err != nil {
		t.Fatal(err)
	}
	if batch.Cursor != "17" || api.offsets[len(api.offsets)-1] != 11 {
		t.Fatalf("the offset confirms what was read and the cursor moves past every update: %+v offsets %v", batch.Cursor, api.offsets)
	}
	want := []RemoteInbound{
		{Sender: "chat 7", Refused: "不是配置的私聊"},
		{Sender: "chat -100", Refused: "不是配置的私聊"},
		{ID: "6", Text: "周三", ReplyTo: "901", Sender: "你"},
		{ID: "7", Sender: "你", Refused: "只支持文字消息"},
	}
	if len(batch.Messages) != len(want) {
		t.Fatalf("messages %+v", batch.Messages)
	}
	for i := range want {
		if batch.Messages[i] != want[i] {
			t.Errorf("message %d: %+v, want %+v", i, batch.Messages[i], want[i])
		}
	}
}

func TestTelegramSendSplitsAndClassifiesFailures(t *testing.T) {
	api := &fakeBotAPI{}
	server := httptest.NewServer(api.handler(t))
	telegram, err := NewTelegram(testTelegramToken, 42, server.URL)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	long := strings.Repeat("长", telegramTextLimit+10)
	ids, err := telegram.Send(ctx, RemoteOutbound{Text: long})
	if err != nil || len(ids) != 2 || ids[0] != "901" || ids[1] != "902" {
		t.Fatalf("a long line takes two messages: %v %v", ids, err)
	}
	if api.sent[0]["chat_id"].(float64) != 42 || len([]rune(api.sent[0]["text"].(string))) != telegramTextLimit {
		t.Fatalf("sent %+v", api.sent[0])
	}

	api.sendCode = http.StatusBadRequest
	_, err = telegram.Send(ctx, RemoteOutbound{Text: "x"})
	var rejected *RemoteRejectedError
	if !errors.As(err, &rejected) {
		t.Fatalf("a 400 is the service refusing this message: %v", err)
	}
	if strings.Contains(err.Error(), "secret-token") {
		t.Fatalf("errors never carry the token: %v", err)
	}
	api.pollCode = http.StatusConflict
	if _, err := telegram.Receive(ctx, "1"); err == nil || !strings.Contains(err.Error(), "webhook") {
		t.Fatalf("a 409 explains the conflict: %v", err)
	}
	server.Close()
	_, err = telegram.Send(ctx, RemoteOutbound{Text: "x"})
	if err == nil || strings.Contains(err.Error(), "secret-token") || !strings.Contains(err.Error(), "<token>") {
		t.Fatalf("a transport error quotes the URL without the token: %v", err)
	}
	if errors.As(err, &rejected) {
		t.Fatal("a transport error is retried, not skipped")
	}
}

func TestNewTelegramChecksItsSettings(t *testing.T) {
	for name, build := range map[string]func() (*Telegram, error){
		"no token":       func() (*Telegram, error) { return NewTelegram("", 42, "") },
		"token with a /": func() (*Telegram, error) { return NewTelegram("a/b", 42, "") },
		"no chat":        func() (*Telegram, error) { return NewTelegram(testTelegramToken, 0, "") },
		"group chat":     func() (*Telegram, error) { return NewTelegram(testTelegramToken, -100, "") },
		"plain http":     func() (*Telegram, error) { return NewTelegram(testTelegramToken, 42, "http://api.example.test") },
	} {
		if _, err := build(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	telegram, err := NewTelegram(testTelegramToken, 42, "")
	if err != nil || telegram.base != DefaultTelegramAPI || telegram.Interval() != 0 {
		t.Fatalf("defaults: %+v %v", telegram, err)
	}
}
