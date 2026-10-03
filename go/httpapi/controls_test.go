package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/protocol"
)

type httpControlFixture struct {
	HTTP struct {
		Cases []struct {
			Name, Path, Token string
			Body              json.RawMessage
			Status            int
			Response          json.RawMessage
		}
		BusyPending   int `json:"busy_pending"`
		RunCount      int `json:"run_count"`
		PendingAfter  int `json:"pending_after"`
		Interjections []string
		Postures      []string
	}
}
type controlFlowProvider struct {
	entered, release chan struct{}
	calls            atomic.Int32
}

func (p *controlFlowProvider) Complete(ctx context.Context, r protocol.ModelRequest) (protocol.ModelReply, error) {
	if p.calls.Add(1) == 1 {
		close(p.entered)
		select {
		case <-ctx.Done():
			return protocol.ModelReply{}, ctx.Err()
		case <-p.release:
		}
		return protocol.ModelReply{ID: "todo", Type: protocol.ReplyMessage, Role: protocol.RoleAssistant, Model: r.Model, Content: []protocol.Block{protocol.NewToolUse("todo", protocol.TodoWriteToolInput(protocol.TodoWriteInput{Items: []protocol.TodoItem{}}))}, StopReason: protocol.StopToolUse, Usage: protocol.TokenUsage{InputTokens: 200, OutputTokens: 3}}, nil
	}
	return (doneProvider{}).Complete(ctx, r)
}
func wrappers(messages []protocol.Message, tag string) []string {
	result := []string{}
	for _, m := range messages {
		if text, ok := m.Content.Plain(); ok && strings.HasPrefix(text, "<"+tag+">") {
			result = append(result, text)
		}
	}
	return result
}
func TestModeAndSteerHTTPMatchActualPython(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-controls.json")
	if err != nil {
		t.Fatal(err)
	}
	expected := decode[httpControlFixture](t, data).HTTP
	p := &controlFlowProvider{entered: make(chan struct{}), release: make(chan struct{})}
	m := testManager(t, p)
	s := testServer(t, Config{Manager: m, Auth: tokenAuth(t)})
	v := create(t, s, "token-a")
	session, err := m.Get("alice", v.ID)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	started := false
	released := false
	defer func() {
		if started && !released {
			close(p.release)
		}
	}()
	for _, c := range expected.Cases {
		if c.Name == "busy-steer" {
			started = true
			go func() { _, err := session.Run(context.Background(), "go"); done <- err }()
			receive(t, p.entered)
		}
		if c.Name == "idle-steer" {
			if session.Info().PendingSteering != expected.BusyPending {
				t.Fatal("pending count differs")
			}
			close(p.release)
			released = true
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		}
		w := request(s, "POST", "/sessions/"+string(v.ID)+"/"+c.Path, string(c.Body), c.Token)
		if w.Code != c.Status {
			t.Fatal(c.Name, w.Code, w.Body.String())
		}
		actual := normalizeJSON(t, w.Body.Bytes(), map[string]string{string(v.ID): "session"}, nil)
		source := normalizeJSON(t, c.Response, nil, nil)
		if !bytes.Equal(actual, source) {
			t.Fatalf("%s actual %s source %s", c.Name, actual, source)
		}
	}
	wait(t, func() bool { v := session.Info(); return v.RunCount == expected.RunCount && !v.Busy })
	final := session.Info()
	if final.PendingSteering != expected.PendingAfter {
		t.Fatal(final)
	}
	if !reflect.DeepEqual(wrappers(session.Messages(), "user_interjection"), expected.Interjections) || !reflect.DeepEqual(wrappers(session.Messages(), "posture_update"), expected.Postures) {
		t.Fatal("control history differs")
	}
	for _, path := range []string{"mode", "steer"} {
		for _, body := range []string{`{}`, `null`, `{"mode":"unsafe"}`} {
			w := request(s, "POST", "/sessions/"+string(v.ID)+"/"+path, body, "token-a")
			if w.Code != 422 {
				t.Fatal(path, body, w.Code)
			}
		}
	}
}
func TestIdleSteerOutlivesRequestAndManagerShutdownJoinsIt(t *testing.T) {
	p := &blockedProvider{entered: make(chan struct{})}
	m := testManager(t, p)
	s := testServer(t, Config{Manager: m, Auth: tokenAuth(t)})
	session := create(t, s, "token-a")
	ctx, cancel := context.WithCancel(context.Background())
	r := httptest.NewRequest("POST", "/sessions/"+string(session.ID)+"/steer", strings.NewReader(`{"message":"work"}`)).WithContext(ctx)
	r.Header.Set("Authorization", "Bearer token-a")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	cancel()
	if w.Code != 200 || decode[agent.SteeringReceipt](t, w.Body.Bytes()).Delivered != agent.DeliveryNewTurn {
		t.Fatal(w.Code, w.Body.String())
	}
	receive(t, p.entered)
	managed, err := m.Get("alice", session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !managed.Info().Busy {
		t.Fatal("request cancellation killed owned wakeup")
	}
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer stopCancel()
	if err := m.Stop(stopCtx); err != nil {
		t.Fatal(err)
	}
	if managed.Info().Busy || managed.Info().RunCount != 1 {
		t.Fatal("background turn not joined")
	}
}
func TestConcurrentWakeupsShareOneActiveHolder(t *testing.T) {
	p := &controlFlowProvider{entered: make(chan struct{}), release: make(chan struct{})}
	m := testManager(t, p)
	s := testServer(t, Config{Manager: m, Auth: tokenAuth(t)})
	v := create(t, s, "token-a")
	first, err := m.Steer("alice", v.ID, "first")
	if err != nil || first.Delivered != agent.DeliveryNewTurn {
		t.Fatal(first, err)
	}
	receive(t, p.entered)
	defer func() {
		select {
		case <-p.release:
		default:
			close(p.release)
		}
	}()
	var workers sync.WaitGroup
	receipts := make(chan agent.SteeringReceipt, 32)
	errors := make(chan error, 32)
	for i := 0; i < 32; i++ {
		workers.Add(1)
		go func() { defer workers.Done(); v, err := m.Steer("alice", v.ID, "next"); receipts <- v; errors <- err }()
	}
	workers.Wait()
	close(receipts)
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	for v := range receipts {
		if v.Delivered != agent.DeliverySteering || !v.Busy {
			t.Fatal("duplicate idle holder", v)
		}
	}
	session, err := m.Get("alice", v.ID)
	if err != nil {
		t.Fatal(err)
	}
	if session.Info().PendingSteering != 32 {
		t.Fatal("wakeups lost")
	}
	close(p.release)
	wait(t, func() bool { return !session.Info().Busy })
	if session.Info().RunCount != 1 || session.Info().PendingSteering != 0 || len(wrappers(session.Messages(), "user_interjection")) != 1 {
		t.Fatal("wakeups not delivered once")
	}
}
func TestSteerUsesOwnerRateBudgetAndModeDoesNot(t *testing.T) {
	p := &blockedProvider{entered: make(chan struct{})}
	m := testManager(t, p)
	s := testServer(t, Config{Manager: m, Auth: tokenAuth(t), RateLimitPerMinute: 1, Now: func() time.Time { return time.Unix(125, 0) }})
	v := create(t, s, "token-a")
	first := request(s, "POST", "/sessions/"+string(v.ID)+"/steer", `{"message":"first"}`, "token-a")
	if first.Code != 200 {
		t.Fatal(first.Code)
	}
	receive(t, p.entered)
	over := request(s, "POST", "/sessions/"+string(v.ID)+"/steer", `{"message":"over"}`, "token-a")
	if over.Code != 429 || over.Header().Get("Retry-After") != "55" {
		t.Fatal(over.Code)
	}
	for _, mode := range []string{"readonly", "auto", "interactive"} {
		w := request(s, "POST", "/sessions/"+string(v.ID)+"/mode", `{"mode":"`+mode+`"}`, "token-a")
		if w.Code != 200 {
			t.Fatal(w.Code)
		}
	}
	managed, _ := m.Get("alice", v.ID)
	if managed.Info().PendingSteering != 0 {
		t.Fatal("over-budget steering queued")
	}
	managed.Cancel(context.Background(), "test")
}
