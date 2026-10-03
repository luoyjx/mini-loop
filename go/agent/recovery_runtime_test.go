package agent

import (
	"context"
	"encoding/json"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/provider"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestDroppedStreamRegeneratesWithoutSplicingAndReleasesModelPermit(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		r.Body.Close()
		w.Header().Set("Content-Type", "text/event-stream")
		n := calls.Add(1)
		io.WriteString(w, "event: message_start\ndata: {\"message\":{\"id\":\"msg\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"served\",\"content\":[],\"usage\":{\"input_tokens\":7,\"output_tokens\":0}}}\n\nevent: content_block_start\ndata: {\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n")
		piece := "complete"
		if n == 1 {
			piece = strings.Repeat("partial", 40)
		}
		encoded, _ := json.Marshal(piece)
		io.WriteString(w, "event: content_block_delta\ndata: {\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":"+string(encoded)+"}}\n\n")
		if n == 1 {
			return
		}
		io.WriteString(w, "event: content_block_stop\ndata: {\"index\":0}\n\nevent: message_delta\ndata: {\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":3}}\n\nevent: message_stop\ndata: {}\n\n")
	}))
	defer server.Close()
	client, _ := provider.NewStreaming(provider.Config{APIKey: "key", BaseURL: server.URL})
	limiter, _ := NewConcurrencyLimiter(1)
	waiter := &recoveryTestWaiter{check: func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		lease, err := limiter.Acquire(ctx)
		if err != nil {
			t.Fatal("recovery wait held model permit", err)
		}
		lease.Release()
	}}
	recovery, _ := NewDefaultRecovery(RecoveryConfig{Waiter: waiter, Jitter: func() float64 { return 0 }})
	s, err := NewManagedSession(RuntimeConfig{ID: "retry", Owner: "owner", Provider: client, Recovery: recovery, Bash: &echoExecutor{}, Mode: ModeAuto, MaxRounds: 2, Workspace: t.TempDir(), ModelLimiter: limiter})
	if err != nil {
		t.Fatal(err)
	}
	live := s.Subscribe(false)
	defer live.Close()
	if text, err := s.Run(context.Background(), "write"); err != nil || text != "complete" {
		t.Fatal(text, err)
	}
	live.Close()
	var starts []StreamID
	deltas := 0
	for r := range live.Events() {
		if start, ok := r.Event.StreamStart(); ok {
			starts = append(starts, start.StreamID)
		}
		if _, ok := r.Event.AssistantDelta(); ok {
			deltas++
		}
	}
	if calls.Load() != 2 || len(starts) != 2 || starts[0] == starts[1] || deltas != 2 || len(waiter.waits) != 1 {
		t.Fatal(calls.Load(), starts, deltas, waiter.waits)
	}
	if len(s.Messages()) != 2 || streamText(s.Messages()[1].Content) != "complete" || s.core.streamedText != "" {
		t.Fatal("partial spliced into final history")
	}
	ends, retries := 0, 0
	for _, r := range s.Events() {
		if e, ok := r.Event.ModelEnd(); ok {
			ends++
			if e.Status != ModelCompleted || e.Usage.OutputTokens != 3 {
				t.Fatal(e)
			}
		}
		if e, ok := r.Event.Recovery(); ok && e.Action == RecoveryRetry {
			retries++
		}
	}
	if ends != 1 || retries != 1 {
		t.Fatal(ends, retries)
	}
}

type fallbackProvider struct {
	calls  int
	models []string
}

func (p *fallbackProvider) Complete(_ context.Context, q protocol.ModelRequest) (protocol.ModelReply, error) {
	p.calls++
	p.models = append(p.models, q.Model)
	if p.calls <= 3 {
		return protocol.ModelReply{}, &provider.Failure{Kind: provider.FailureStatus, Status: 529, Message: "overloaded"}
	}
	return streamedReply("done"), nil
}
func TestFallbackModelPersistsButChildRecoveryStateIsFresh(t *testing.T) {
	p := &fallbackProvider{}
	policy, _ := NewDefaultRecovery(RecoveryConfig{FallbackModel: "backup", Waiter: &recoveryTestWaiter{}})
	s, err := NewRuntimeSession(RuntimeConfig{ID: "fallback", Owner: "owner", Provider: p, Recovery: policy, Bash: &echoExecutor{}, Mode: ModeAuto, MaxRounds: 2, Workspace: t.TempDir(), Model: "primary"})
	if err != nil {
		t.Fatal(err)
	}
	for _, prompt := range []string{"one", "two"} {
		if text, err := s.Run(context.Background(), prompt); err != nil || text != "done" {
			t.Fatal(text, err)
		}
	}
	if p.models[3] != "backup" || p.models[4] != "backup" || s.model != "primary" || s.recoveryModel != "backup" {
		t.Fatal(p.models, s.model, s.recoveryModel)
	}
	summary, err := s.Delegate(context.Background(), "child", RoleExplore)
	if err != nil || summary != "done" {
		t.Fatal(summary, err)
	}
	if p.models[len(p.models)-1] != "primary" {
		t.Fatal("child inherited transient fallback state", p.models)
	}
}
func TestReactiveRequestPreservesPairingAndRebasesOnlyRetainedCache(t *testing.T) {
	q := protocol.ModelRequest{Model: "m", MaxTokens: 100, Purpose: protocol.PurposeAgentTurn}
	q.Messages = []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent(strings.Repeat("old", 500))}, {Role: protocol.RoleAssistant, Content: protocol.BlockContent(protocol.NewToolUseWithoutCaller("u", protocol.BashToolInput(protocol.BashInput{Command: "pwd"})))}, {Role: protocol.RoleUser, Content: protocol.BlockContent(protocol.NewToolResult("u", "cwd", false))}}
	for i := 0; i < 5; i++ {
		q.Messages = append(q.Messages, protocol.Message{Role: protocol.RoleUser, Content: protocol.PlainContent("tail")})
	}
	q.Cache.Messages = []protocol.CacheBreakpoint{{MessageIndex: 0, BlockIndex: 0, Control: protocol.CacheControl{Type: protocol.CacheEphemeral}}, {MessageIndex: 2, BlockIndex: 0, Control: protocol.CacheControl{Type: protocol.CacheEphemeral}}}
	out := reactiveRequest(q)
	if err := out.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(out.Cache.Messages) != 1 || out.Cache.Messages[0].MessageIndex != 2 || len(q.Cache.Messages) != 2 {
		t.Fatal(out.Cache, q.Cache)
	}
	blocks, _ := out.Messages[1].Content.Blocks()
	if _, ok := blocks[0].ToolUse(); !ok {
		t.Fatal("pair severed")
	}
}

func TestNeutralSSEErrorTypeStillRetries(t *testing.T) {
	for _, code := range []string{"overloaded_error", "rate_limit_error"} {
		t.Run(code, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				io.Copy(io.Discard, r.Body)
				r.Body.Close()
				w.Header().Set("Content-Type", "text/event-stream")
				if calls.Add(1) == 1 {
					io.WriteString(w, "event: error\ndata: {\"error\":{\"type\":\""+code+"\",\"message\":\"temporary issue\"}}\n\n")
					return
				}
				io.WriteString(w, "event: message_start\ndata: {\"message\":{\"id\":\"msg\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"served\",\"content\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\nevent: message_delta\ndata: {\"delta\":{\"stop_reason\":\"refusal\"},\"usage\":{\"output_tokens\":0}}\n\nevent: message_stop\ndata: {}\n\n")
			}))
			defer server.Close()
			client, _ := provider.NewStreaming(provider.Config{APIKey: "key", BaseURL: server.URL})
			waiter := &recoveryTestWaiter{}
			policy, _ := NewDefaultRecovery(RecoveryConfig{Waiter: waiter})
			s, err := NewRuntimeSession(RuntimeConfig{ID: "neutral", Owner: "owner", Provider: client, Recovery: policy, Bash: &echoExecutor{}, Mode: ModeAuto, MaxRounds: 2, Workspace: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			if answer, err := s.Run(context.Background(), "go"); err != nil || answer != refusalNotice {
				t.Fatal(answer, err)
			}
			if calls.Load() != 2 || len(waiter.waits) != 1 {
				t.Fatal(calls.Load(), waiter.waits)
			}
		})
	}
}
