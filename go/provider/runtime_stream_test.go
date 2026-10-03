package provider_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/provider"
)

func TestRealStreamingToolRoundPreservesSignedHistoryUsageAndPhases(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-streams.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct{ Cases []struct{ Name, Wire string } }
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	wire := fixture.Cases[0].Wire
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Stream   bool
			Model    string
			Messages []protocol.Message
			Tools    []protocol.ToolSchema
			System   json.RawMessage
		}
		// Cache metadata is intentionally decoded at the wire adapter, not into
		// stored Message, which rejects cache annotations in its content blocks.
		var raw struct {
			Stream   bool
			Model    string
			Messages []struct {
				Role    protocol.Role
				Content json.RawMessage
			}
			Tools  []protocol.ToolSchema
			System json.RawMessage
		}
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			t.Error(err)
			return
		}
		input.Stream = raw.Stream
		input.Model = raw.Model
		input.Tools = raw.Tools
		if !input.Stream || input.Model != "requested" || len(input.Tools) != 10 || !strings.Contains(string(raw.System), "cache_control") {
			t.Error("stream request lost model/catalogue")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if calls.Add(1) == 1 {
			io.WriteString(w, wire)
			return
		}
		signed, paired := false, false
		for _, m := range raw.Messages {
			var blocks []struct {
				Type      protocol.BlockKind
				Signature string
				ToolUseID string `json:"tool_use_id"`
				Input     json.RawMessage
			}
			if json.Unmarshal(m.Content, &blocks) == nil {
				for _, b := range blocks {
					if b.Type == protocol.BlockThinking && b.Signature == "signed-proof" {
						signed = true
					}
					if b.Type == protocol.BlockToolResult && b.ToolUseID == "tool-stream" {
						paired = true
					}
				}
			}
		}
		if !signed || !paired {
			t.Error("second request lost signed thinking or tool pairing")
		}
		io.WriteString(w, "event: message_start\ndata: {\"message\":{\"id\":\"final\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"served-final\",\"content\":[],\"usage\":{\"input_tokens\":25,\"output_tokens\":0}}}\n\nevent: content_block_start\ndata: {\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\nevent: content_block_delta\ndata: {\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"done\"}}\n\nevent: content_block_stop\ndata: {\"index\":0}\n\nevent: message_delta\ndata: {\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":{\"output_tokens\":2}}\n\nevent: message_stop\ndata: {}\n\n")
	}))
	defer server.Close()
	client, _ := provider.NewStreaming(provider.Config{APIKey: "fixture-key", BaseURL: server.URL})
	manager, err := agent.NewSessionManager(agent.ManagerConfig{WorkspaceRoot: t.TempDir(), Defaults: agent.SessionDefaults{PermissionMode: agent.ModeAuto, Model: "requested", MaxTokens: 64000}, Services: agent.ManagerServices{Provider: client}})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Stop(context.Background())
	s, err := manager.Create(context.Background(), agent.CreateSessionRequest{Owner: "owner", PermissionMode: agent.ModeAuto})
	if err != nil {
		t.Fatal(err)
	}
	live := s.Subscribe(false)
	if text, err := s.Run(context.Background(), "write"); err != nil || text != "done" {
		t.Fatal(text, err)
	}
	live.Close()
	info := s.Info()
	landed, err := os.ReadFile(info.Workspace + "/artifact.txt")
	if err != nil || string(landed) != "你好\n" {
		t.Fatal(string(landed), err)
	}
	if err := protocol.ValidateTranscript(s.Messages()); err != nil {
		t.Fatal(err)
	}
	var starts []agent.StreamID
	var phases []agent.TextPhase
	for record := range live.Events() {
		if start, ok := record.Event.StreamStart(); ok {
			starts = append(starts, start.StreamID)
		}
		if text, ok := record.Event.AssistantText(); ok {
			phases = append(phases, text.Phase)
			if text.StreamID != starts[len(starts)-1] {
				t.Fatal("final correlation missing")
			}
		}
	}
	if len(starts) != 2 || starts[0] == starts[1] || len(phases) != 2 || phases[0] != agent.PhaseCommentary || phases[1] != agent.PhaseFinalAnswer {
		t.Fatal(starts, phases)
	}
	ends := 0
	for _, record := range s.Events() {
		if record.Event.Ephemeral() {
			t.Fatal("live delta replayed")
		}
		if end, ok := record.Event.ModelEnd(); ok {
			ends++
			if end.Usage == nil || end.ServedModel == nil || end.PromptTokens == nil {
				t.Fatal("streaming usage missing")
			}
			if ends == 1 && (*end.ServedModel != "served-stream" || *end.PromptTokens != 27 || end.Usage.OutputTokens != 9) {
				t.Fatal(end)
			}
		}
	}
	if ends != 2 || calls.Load() != 2 {
		t.Fatal(ends, calls.Load())
	}
}
func TestActualStreamingCancellationAndTimeoutCloseResponse(t *testing.T) {
	for _, timeout := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancel", true: "timeout"}[timeout], func(t *testing.T) {
			started := make(chan struct{})
			closed := make(chan struct{})
			release := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				io.Copy(io.Discard, r.Body)
				r.Body.Close()
				w.Header().Set("Content-Type", "text/event-stream")
				io.WriteString(w, ": ready\n\n")
				w.(http.Flusher).Flush()
				close(started)
				select {
				case <-r.Context().Done():
				case <-release:
				}
				close(closed)
			}))
			defer server.Close()
			defer close(release)
			cfg := provider.Config{APIKey: "key", BaseURL: server.URL}
			if timeout {
				cfg.Timeout = 150 * time.Millisecond
			}
			c, _ := provider.NewStreaming(cfg)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			finished := make(chan error, 1)
			go func() {
				_, err := c.CompleteStream(ctx, protocol.ModelRequest{Model: "requested", MaxTokens: 100, Messages: []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent("go")}}, Purpose: protocol.PurposeAgentTurn}, nil)
				finished <- err
			}()
			select {
			case <-started:
			case <-time.After(3 * time.Second):
				t.Fatal("not started")
			}
			if !timeout {
				cancel()
			}
			select {
			case err := <-finished:
				if timeout {
					var f *provider.Failure
					if !errors.As(err, &f) || f.Kind != provider.FailureTimeout {
						t.Fatal(err)
					}
				} else if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("owned response not cancelled")
			}
			select {
			case <-closed:
			case <-time.After(3 * time.Second):
				t.Fatal("response still open")
			}
		})
	}
}
func TestSharedStreamingClientConcurrentGenerationsStayDetached(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Model string }
		json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "text/event-stream")
		reply := "event: message_start\ndata: {\"message\":{\"id\":\"" + req.Model + "\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"" + req.Model + "\",\"content\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\nevent: message_delta\ndata: {\"delta\":{\"stop_reason\":\"refusal\"},\"usage\":{\"output_tokens\":0}}\n\nevent: message_stop\ndata: {}\n\n"
		io.WriteString(w, reply)
	}))
	defer server.Close()
	c, _ := provider.NewStreaming(provider.Config{APIKey: "key", BaseURL: server.URL})
	finished := make(chan error, 32)
	for i := 0; i < 32; i++ {
		go func(i int) {
			model := strings.Repeat("m", i+1)
			reply, err := c.Complete(context.Background(), protocol.ModelRequest{Model: model, MaxTokens: 64000, Messages: []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent("go")}}, Purpose: protocol.PurposeAgentTurn})
			if err == nil && (reply.ID != model || reply.Model != model) {
				err = errors.New("generation mixed")
			}
			finished <- err
		}(i)
	}
	for i := 0; i < 32; i++ {
		if err := <-finished; err != nil {
			t.Fatal(err)
		}
	}
}
