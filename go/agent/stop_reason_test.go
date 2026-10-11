package agent

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/luoyjx/mini-loop/go/protocol"
)

type scriptedStopProvider struct {
	replies []protocol.ModelReply
	calls   int
	lengths []int
}

func (provider *scriptedStopProvider) Complete(_ context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
	messages := request.Messages
	provider.lengths = append(provider.lengths, len(messages))
	reply := provider.replies[provider.calls]
	provider.calls++
	return reply.Clone(), nil
}

func TestStopReasonConstantsTrackPythonManifest(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-contract-manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		MaxResumptions    int    `json:"max_resumptions"`
		RefusalNotice     string `json:"refusal_notice"`
		UnknownToolResult string `json:"unknown_tool_result"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.MaxResumptions != maxResumptions || manifest.RefusalNotice != refusalNotice || manifest.UnknownToolResult != unknownToolResult {
		t.Fatalf("Python stop contract changed: %+v", manifest)
	}
}

func TestPausedTurnResumesWithoutInventingUserMessage(t *testing.T) {
	provider := &scriptedStopProvider{replies: []protocol.ModelReply{
		fakeReply([]protocol.Block{protocol.NewTextBlock("Let me search")}, protocol.StopPauseTurn),
		fakeReply([]protocol.Block{protocol.NewTextBlock("Found it: 42")}, protocol.StopEndTurn),
	}}
	session, err := NewSession("s", "owner", provider, echoExecutor{}, 10)
	if err != nil {
		t.Fatal(err)
	}
	answer, err := session.Run(context.Background(), "go")
	if err != nil || answer != "Found it: 42" {
		t.Fatalf("pause resumed to %q: %v", answer, err)
	}
	if provider.calls != 2 || provider.lengths[0] != 1 || provider.lengths[1] != 2 {
		t.Fatalf("pause changed transcript shape: %+v", provider.lengths)
	}
	events := session.StopEvents()
	if len(events) != 1 || events[0].Kind() != EventTurnPaused || events[0].Resumption() != 1 {
		t.Fatalf("missing typed pause event: %+v", events)
	}
}

func TestRepeatedPauseIsBounded(t *testing.T) {
	replies := make([]protocol.ModelReply, maxResumptions+1)
	for i := range replies {
		replies[i] = fakeReply([]protocol.Block{protocol.NewTextBlock("p")}, protocol.StopPauseTurn)
	}
	provider := &scriptedStopProvider{replies: replies}
	session, err := NewSession("s", "owner", provider, echoExecutor{}, len(replies)+1)
	if err != nil {
		t.Fatal(err)
	}
	answer, err := session.Run(context.Background(), "go")
	if err != nil || answer != "p" || provider.calls != len(replies) {
		t.Fatalf("pause bound produced %q after %d calls: %v", answer, provider.calls, err)
	}
	events := session.StopEvents()
	if len(events) != maxResumptions+1 || events[len(events)-1].Kind() != EventProviderStopUnhandled {
		t.Fatalf("pause bound events: %+v", events)
	}
}

func TestRefusalAndUnknownStopRemainObservable(t *testing.T) {
	for _, tc := range []struct {
		name   string
		reply  protocol.ModelReply
		answer string
		kind   StopEventKind
	}{
		{"refusal", fakeReply([]protocol.Block{}, protocol.StopRefusal), refusalNotice, EventProviderRefusal},
		{"refusal-with-text", fakeReply([]protocol.Block{protocol.NewTextBlock("I cannot help")}, protocol.StopRefusal), "I cannot help", EventProviderRefusal},
		{"unknown", fakeReply([]protocol.Block{protocol.NewTextBlock("partial")}, "model_context_window_exceeded"), "partial", EventProviderStopUnhandled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider := &scriptedStopProvider{replies: []protocol.ModelReply{tc.reply}}
			session, err := NewSession("s", "owner", provider, echoExecutor{}, 2)
			if err != nil {
				t.Fatal(err)
			}
			answer, err := session.Run(context.Background(), "go")
			if err != nil || answer != tc.answer {
				t.Fatalf("answer=%q error=%v", answer, err)
			}
			events := session.StopEvents()
			if len(events) != 1 || events[0].Kind() != tc.kind || events[0].Reason() != tc.reply.StopReason {
				t.Fatalf("stop event=%+v", events)
			}
		})
	}
}

func TestToolContentOverridesInconsistentStopReason(t *testing.T) {
	provider := &scriptedStopProvider{replies: []protocol.ModelReply{
		fakeReply([]protocol.Block{protocol.NewBashUse("u1", "echo handled: go")}, protocol.StopEndTurn),
		fakeReply([]protocol.Block{protocol.NewTextBlock("done")}, protocol.StopEndTurn),
	}}
	session, err := NewSession("s", "owner", provider, echoExecutor{}, 3)
	if err != nil {
		t.Fatal(err)
	}
	answer, err := session.Run(context.Background(), "go")
	if err != nil || answer != "done" || provider.calls != 2 {
		t.Fatalf("tool block was ignored because stop_reason was end_turn: %q, %v", answer, err)
	}
	if err := protocol.ValidateTranscript(session.Messages()); err != nil {
		t.Fatal(err)
	}
}
