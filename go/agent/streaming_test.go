package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/provider"
)

type streamMask struct{}

func (streamMask) MaskText(text string) string {
	return strings.ReplaceAll(text, "秘密", "[REDACTED]")
}

type streamEventProjection struct {
	Type        SessionEventKind `json:"type"`
	Text        string           `json:"text,omitempty"`
	StreamID    StreamID         `json:"stream_id"`
	Phase       TextPhase        `json:"phase"`
	Provisional bool             `json:"provisional"`
	Ephemeral   bool             `json:"ephemeral"`
}

func TestStreamingEventsAndPartialsMatchPython(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-streams.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Request        protocol.ModelRequest
		DefaultChars   int     `json:"default_coalesce_chars"`
		DefaultSeconds float64 `json:"default_coalesce_seconds"`
		Cases          []struct {
			Name, Wire, Partial string
			Events              []streamEventProjection
			Reply               *protocol.ModelReply
		}
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	fixture.Request.Purpose = protocol.PurposeAgentTurn
	if fixture.DefaultChars != DefaultDeltaCoalesceChars || fixture.DefaultSeconds != DefaultDeltaCoalesceDuration.Seconds() {
		t.Fatal("default coalescing drift")
	}
	for _, v := range fixture.Cases {
		if v.Name == "drop" {
			continue
		} // Reader drop is independently tested by provider; cancellation below records the partial.
		t.Run(v.Name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				w.Write([]byte(v.Wire))
			}))
			defer server.Close()
			client, _ := provider.NewStreaming(provider.Config{APIKey: "fixture-key", BaseURL: server.URL})
			s, _ := NewSession("stream", "owner", FakeProvider{}, &echoExecutor{}, 2)
			s.events.secrets = streamMask{}
			live := s.events.subscribe(false)
			_, err := s.streamingComplete(context.Background(), client, fixture.Request)
			if (err == nil) != (v.Reply != nil) {
				t.Fatal(err)
			}
			live.Close()
			var actual []streamEventProjection
			for record := range live.Events() {
				raw, _ := json.Marshal(record)
				var row streamEventProjection
				json.Unmarshal(raw, &row)
				row.StreamID = "stream_0123456789abcdef"
				actual = append(actual, row)
			}
			if !reflect.DeepEqual(actual, v.Events) {
				t.Fatalf("progress differs: %#v / %#v", actual, v.Events)
			}
			if s.streamedText != v.Partial {
				t.Fatalf("partial %q / %q", s.streamedText, v.Partial)
			}
			if len(s.Events()) != 0 {
				t.Fatal("provisional progress reached replay")
			}
		})
	}
}

type syntheticStream struct {
	calls   atomic.Int32
	pieces  []protocol.StreamDelta
	reply   protocol.ModelReply
	wait    bool
	started chan struct{}
	failed  error
}

func (p *syntheticStream) Complete(context.Context, protocol.ModelRequest) (protocol.ModelReply, error) {
	panic("stream selected direct provider")
}
func (p *syntheticStream) CompleteStream(ctx context.Context, _ protocol.ModelRequest, emit func(protocol.StreamDelta) error) (protocol.ModelReply, error) {
	p.calls.Add(1)
	for _, piece := range p.pieces {
		if err := emit(piece); err != nil {
			return protocol.ModelReply{}, err
		}
	}
	if p.started != nil {
		close(p.started)
	}
	if p.wait {
		<-ctx.Done()
		return protocol.ModelReply{}, ctx.Err()
	}
	return p.reply.Clone(), p.failed
}
func streamedReply(text string) protocol.ModelReply {
	return protocol.ModelReply{ID: "message", Type: protocol.ReplyMessage, Role: protocol.RoleAssistant, Model: "served", Content: []protocol.Block{protocol.NewTextBlock(text)}, StopReason: protocol.StopEndTurn, Usage: protocol.TokenUsage{InputTokens: 11, OutputTokens: 3}}
}
func TestManagedStreamingCancellationKeepsOnlyShownAnswerAndClearsPartial(t *testing.T) {
	shown := strings.Repeat("已显示", 70)
	p := &syntheticStream{pieces: []protocol.StreamDelta{{Kind: protocol.DeltaThinking, Text: strings.Repeat("thought", 40)}, {Kind: protocol.DeltaText, Text: shown}, {Kind: protocol.DeltaText, Text: "unflushed"}}, wait: true, started: make(chan struct{})}
	s, err := NewManagedSession(RuntimeConfig{ID: "stream", Owner: "owner", Workspace: t.TempDir(), Provider: p, Bash: &echoExecutor{}, Mode: ModeAuto, MaxRounds: 2})
	if err != nil {
		t.Fatal(err)
	}
	live := s.Subscribe(false)
	defer live.Close()
	finished := make(chan error, 1)
	go func() { _, err := s.Run(context.Background(), "write"); finished <- err }()
	select {
	case <-p.started:
	case <-time.After(3 * time.Second):
		t.Fatal("stream did not begin")
	}
	if ok, err := s.Cancel(context.Background(), "cancel fixture"); !ok || err != nil {
		t.Fatal("cancel was not admitted", err)
	}
	select {
	case err = <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancel hung")
	}
	messages := s.Messages()
	last := messages[len(messages)-1]
	text := streamText(last.Content)
	if text != shown+"\n[Turn interrupted: cancel fixture]" {
		t.Fatalf("wrong recovery text %q", text)
	}
	if s.core.streamedText != "" {
		t.Fatal("partial survived repair")
	}
	if err := protocol.ValidateTranscript(messages); err != nil {
		t.Fatal(err)
	}
	for _, r := range s.Events() {
		if r.Event.Ephemeral() {
			t.Fatal("progress in replay")
		}
	}
	// A completed generation clears partials before an interruption, including
	// internal compaction requests that never become assistant history.
	p.wait = false
	p.started = nil
	p.reply = streamedReply("complete")
	if _, err := s.core.streamingComplete(context.Background(), p, protocol.ModelRequest{Model: "requested", MaxTokens: 100, Messages: messages, Purpose: protocol.PurposeCompaction}); err != nil {
		t.Fatal(err)
	}
	if s.core.streamedText != "" {
		t.Fatal("completed stream left stale text")
	}
	s.core.recordInterruption("after completion")
	if last := s.Messages(); streamText(last[len(last)-1].Content) != "[Turn interrupted: after completion]" {
		t.Fatal("duplicated completed/internal text")
	}
}
func TestStreamingFinalCorrelationUsageAndFreshGenerations(t *testing.T) {
	p := &syntheticStream{pieces: []protocol.StreamDelta{{Kind: protocol.DeltaText, Text: "complete"}}, reply: streamedReply("complete")}
	s, err := NewManagedSession(RuntimeConfig{ID: "stream", Owner: "owner", Workspace: t.TempDir(), Provider: p, Bash: &echoExecutor{}, Mode: ModeAuto, MaxRounds: 2})
	if err != nil {
		t.Fatal(err)
	}
	live := s.Subscribe(false)
	for _, prompt := range []string{"one", "two"} {
		if text, err := s.Run(context.Background(), prompt); err != nil || text != "complete" {
			t.Fatal(text, err)
		}
	}
	live.Close()
	var starts, finals []StreamID
	for record := range live.Events() {
		if start, ok := record.Event.StreamStart(); ok {
			starts = append(starts, start.StreamID)
			if !start.Provisional || start.Phase != PhaseCommentary || !record.Event.Ephemeral() {
				t.Fatal(start)
			}
		}
		if delta, ok := record.Event.AssistantDelta(); ok {
			if !delta.Provisional || delta.Phase != PhaseCommentary || delta.StreamID != starts[len(starts)-1] {
				t.Fatal(delta)
			}
		}
		if final, ok := record.Event.AssistantText(); ok {
			finals = append(finals, final.StreamID)
			if final.Phase != PhaseFinalAnswer {
				t.Fatal(final)
			}
		}
	}
	if len(starts) != 2 || starts[0] == starts[1] || !reflect.DeepEqual(starts, finals) {
		t.Fatal(starts, finals)
	}
	if meter := s.core.TokenMeter(); meter.Observations != 2 {
		t.Fatal(meter)
	}
}
func TestStreamingConsumerRejectsInvalidAndUnboundedProgress(t *testing.T) {
	for _, piece := range []protocol.StreamDelta{{Kind: "unknown", Text: "bad"}, {Kind: protocol.DeltaText, Text: "\xff"}, {Kind: protocol.DeltaText, Text: strings.Repeat("x", protocol.MaxWireBytes+1)}} {
		p := &syntheticStream{pieces: []protocol.StreamDelta{piece}, reply: streamedReply("valid")}
		s, _ := NewSession("stream", "owner", p, &echoExecutor{}, 2)
		if _, err := s.streamingComplete(context.Background(), p, protocol.ModelRequest{}); err == nil {
			t.Fatal("invalid progress accepted")
		}
	}
}

func streamText(content protocol.Content) string {
	if plain, ok := content.Plain(); ok {
		return plain
	}
	blocks, _ := content.Blocks()
	var text string
	for _, block := range blocks {
		if b, ok := block.Text(); ok {
			text += b.Text
		}
	}
	return text
}

type delegatingStream struct{ calls atomic.Int32 }

func (p *delegatingStream) Complete(context.Context, protocol.ModelRequest) (protocol.ModelReply, error) {
	panic("child lost stream seam")
}
func (p *delegatingStream) CompleteStream(ctx context.Context, _ protocol.ModelRequest, emit func(protocol.StreamDelta) error) (protocol.ModelReply, error) {
	n := p.calls.Add(1)
	if err := emit(protocol.StreamDelta{Kind: protocol.DeltaText, Text: "progress"}); err != nil {
		return protocol.ModelReply{}, err
	}
	reply := streamedReply("done")
	if n == 1 {
		reply.Content = []protocol.Block{protocol.NewToolUseWithoutCaller("child", protocol.TaskToolInput(protocol.TaskInput{Prompt: "explore", AgentType: streamRolePointer("Explore")}))}
		reply.StopReason = protocol.StopToolUse
	}
	return reply, nil
}
func TestFreshChildInheritsStreamingProviderWithIndependentIDs(t *testing.T) {
	p := &delegatingStream{}
	s, err := NewManagedSession(RuntimeConfig{ID: "stream-parent", Owner: "owner", Workspace: t.TempDir(), Provider: p, Bash: &echoExecutor{}, Mode: ModeAuto, MaxRounds: 3})
	if err != nil {
		t.Fatal(err)
	}
	live := s.Subscribe(false)
	defer live.Close()
	if text, err := s.Run(context.Background(), "delegate"); err != nil || text != "done" {
		t.Fatal(text, err)
	}
	live.Close()
	seen := map[StreamID]bool{}
	child := false
	for r := range live.Events() {
		if start, ok := r.Event.StreamStart(); ok {
			if seen[start.StreamID] {
				t.Fatal("generation ID shared")
			}
			seen[start.StreamID] = true
			if r.Scope.Depth == 1 {
				child = true
			}
		}
	}
	if p.calls.Load() != 3 || len(seen) != 3 || !child {
		t.Fatal(p.calls.Load(), seen, child)
	}
}
func streamRolePointer(s protocol.AgentType) *protocol.AgentType { return &s }
