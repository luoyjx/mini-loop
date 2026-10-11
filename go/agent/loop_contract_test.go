package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/protocol"
)

type cacheFixtureBlock struct {
	Type  protocol.BlockKind     `json:"type"`
	Text  string                 `json:"text"`
	Cache *protocol.CacheControl `json:"cache_control"`
}
type cacheFixtureContent struct {
	Plain  *string
	Blocks []cacheFixtureBlock
}

func (content *cacheFixtureContent) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return nil
	}
	if len(data) > 0 && data[0] == '"' {
		var value string
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
		content.Plain = &value
		return nil
	}
	return json.Unmarshal(data, &content.Blocks)
}

type cacheFixtureMessage struct {
	Role    protocol.Role
	Content cacheFixtureContent
}
type loopFixtureEvent struct {
	Pattern    StuckPattern
	Detail     string
	Tool       *protocol.ToolName
	Halted     bool
	NudgesUsed int `json:"nudges_used"`
}
type loopContracts struct {
	Caches []struct {
		Name            string
		System          *string
		Messages        []protocol.Message
		TTL             protocol.CacheTTL
		Maximum, Stride int
		Null            bool
		CachedSystem    cacheFixtureContent   `json:"cached_system"`
		CachedMessages  []cacheFixtureMessage `json:"cached_messages"`
		Positions       [][2]int
		SourceUnchanged bool `json:"source_unchanged"`
		FakeTokens      int  `json:"fake_tokens"`
	}
	Decisions []struct {
		Name       string
		Steps      []ToolStep
		Rounds     int
		Thresholds StuckThresholds
		Signal     *StuckSignal
		Reminder   *string
	}
	Hashes []struct {
		Tool       protocol.ToolName
		Input      json.RawMessage
		InputHash  StepHash `json:"input_hash"`
		Output     string
		OutputHash StepHash `json:"output_hash"`
	}
	Loops []struct {
		Name                     string
		Denied                   bool
		MaxNudges                int `json:"max_nudges"`
		Monologue, Null          bool
		Calls                    int
		Output                   string
		Events                   []loopFixtureEvent
		Reminders, Continuations []string
	}
}

func readLoopContracts(t *testing.T) loopContracts {
	t.Helper()
	data, err := os.ReadFile("../testdata/python-loops.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture loopContracts
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	return fixture
}
func TestCacheWireMatchesPythonProjections(t *testing.T) {
	for _, item := range readLoopContracts(t).Caches {
		t.Run(item.Name, func(t *testing.T) {
			request := protocol.ModelRequest{Model: DefaultModel, MaxTokens: 8000, System: item.System, Messages: item.Messages, Purpose: protocol.PurposeAgentTurn}
			before, _ := json.Marshal(request)
			var policy CachePolicy
			if item.Null {
				policy = NullCachePolicy{}
			} else {
				value, err := NewCachePolicy(CacheConfig{TTL: item.TTL, MaxBreakpoints: item.Maximum, Stride: item.Stride})
				if err != nil {
					t.Fatal(err)
				}
				policy = value
			}
			annotated, err := policy.Annotate(request)
			if err != nil {
				t.Fatal(err)
			}
			if err := annotated.Validate(); err != nil {
				t.Fatal(err)
			}
			count, err := FakePromptTokens(annotated)
			if err != nil || count != item.FakeTokens {
				t.Fatalf("fake tokens %d, want %d, err %v", count, item.FakeTokens, err)
			}
			data, err := json.Marshal(annotated)
			if err != nil {
				t.Fatal(err)
			}
			var wire struct {
				Messages []cacheFixtureMessage
				System   cacheFixtureContent
			}
			if err := json.Unmarshal(data, &wire); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(wire.Messages, item.CachedMessages) || !reflect.DeepEqual(wire.System, item.CachedSystem) {
				t.Fatalf("wire does not match Python: %s", data)
			}
			positions := make([][2]int, 0)
			for _, target := range annotated.Cache.Messages {
				positions = append(positions, [2]int{target.MessageIndex, target.BlockIndex})
			}
			if !reflect.DeepEqual(positions, item.Positions) {
				t.Fatalf("positions %v, want %v", positions, item.Positions)
			}
			after, _ := json.Marshal(request)
			if string(before) != string(after) || !item.SourceUnchanged {
				t.Fatal("cache policy changed history")
			}
			if count := len(positions) + boolCount(annotated.Cache.System != nil); count > item.Maximum {
				t.Fatal("cache breakpoint budget exceeded")
			}
			if annotated.Cache.System != nil {
				annotated.Cache.System.TTL = "mutated"
			}
			if request.Cache.System != nil {
				t.Fatal("annotation aliases request")
			}
		})
	}
}
func TestStuckDecisionsAndHashesMatchPython(t *testing.T) {
	fixture := readLoopContracts(t)
	for _, item := range fixture.Decisions {
		t.Run(item.Name, func(t *testing.T) {
			detector, err := NewStuckDetector(item.Thresholds)
			if err != nil {
				t.Fatal(err)
			}
			signal, err := detector.Inspect(StuckState{item.Steps, item.Rounds})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(signal, item.Signal) {
				t.Fatalf("signal %#v, want %#v", signal, item.Signal)
			}
			if signal != nil && (item.Reminder == nil || signal.Reminder() != *item.Reminder) {
				t.Fatal("reminder changed")
			}
		})
	}
	for _, item := range fixture.Hashes {
		input, err := protocol.DecodeToolInput(item.Tool, item.Input)
		if err != nil {
			t.Fatal(err)
		}
		in, err := InputStepHash(input)
		if err != nil {
			t.Fatal(err)
		}
		out, err := OutputStepHash(item.Output)
		if err != nil {
			t.Fatal(err)
		}
		if in != item.InputHash || out != item.OutputHash {
			t.Fatalf("%s hash = %s/%s, want %s/%s", item.Tool, in, out, item.InputHash, item.OutputHash)
		}
	}
}

type loopingProvider struct {
	calls           int
	monologue       bool
	varyingCommands bool
	requests        []protocol.ModelRequest
}

func (provider *loopingProvider) Complete(_ context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
	if err := request.Validate(); err != nil {
		return protocol.ModelReply{}, err
	}
	provider.calls++
	provider.requests = append(provider.requests, request.Clone())
	if provider.monologue {
		return fakeReply([]protocol.Block{protocol.NewTextBlock("loop commentary")}, protocol.StopEndTurn), nil
	}
	command := "printf same"
	if provider.varyingCommands {
		command = fmt.Sprintf("printf value-%d", provider.calls)
	}
	return fakeReply([]protocol.Block{protocol.NewTextBlock("loop commentary"), protocol.NewBashUse(fmt.Sprintf("u%d", provider.calls), command)}, protocol.StopToolUse), nil
}

type resumeStopHook struct{}

func (resumeStopHook) Stop(context.Context, StopContext) (*string, error) {
	text := "keep going"
	return &text, nil
}
func TestActualLoopNudgesAndHaltsMatchPython(t *testing.T) {
	for _, item := range readLoopContracts(t).Loops {
		t.Run(item.Name, func(t *testing.T) {
			provider := &loopingProvider{monologue: item.Monologue}
			handler := &gateHandler{output: "same"}
			catalog := gateTestCatalog(t, handler)
			hooks := GateHooks{}
			if item.Denied {
				hooks.Before = []BeforeHook{beforeHookFunc(func(context.Context, ToolAuthority, ToolCall) (BeforeDecision, error) {
					return DenyToolCall("Error: permission denied by policy"), nil
				})}
			}
			gate, _ := NewToolGate(catalog, DefaultPermissionPolicy(nil), hooks)
			session, err := NewSessionWithGate("s", "owner", provider, gate, ModeAuto, "", 12)
			if err != nil {
				t.Fatal(err)
			}
			thresholds := DefaultStuckThresholds()
			thresholds.MaxNudges = item.MaxNudges
			detector, err := NewStuckDetector(thresholds)
			if err != nil {
				t.Fatal(err)
			}
			session.stuckDetector = detector
			if item.Null {
				session.stuckDetector = NullStuckDetector{}
			}
			if item.Monologue {
				session.stopHooks = []StopHook{resumeStopHook{}}
			}
			output, err := session.Run(context.Background(), "go")
			if err != nil {
				t.Fatal(err)
			}
			if provider.calls != item.Calls || output != item.Output {
				t.Fatalf("calls %d, output %q; want %d, %q", provider.calls, output, item.Calls, item.Output)
			}
			events := make([]loopFixtureEvent, 0)
			for _, record := range session.Events() {
				if event, ok := record.Event.Stuck(); ok {
					signal := event.Signal()
					events = append(events, loopFixtureEvent{signal.Pattern, signal.Detail, signal.Tool, event.Halted(), event.NudgesUsed()})
				}
			}
			if !reflect.DeepEqual(events, item.Events) {
				t.Fatalf("events %#v; want %#v", events, item.Events)
			}
			reminders, continuations := make([]string, 0), make([]string, 0)
			for _, message := range session.Messages() {
				if message.Role != protocol.RoleUser {
					continue
				}
				if plain, ok := message.Content.Plain(); ok && strings.Contains(plain, "<stuck") {
					continuations = append(continuations, plain)
				}
				if blocks, ok := message.Content.Blocks(); ok {
					for _, block := range blocks {
						if text, ok := block.Text(); ok && strings.Contains(text.Text, "<stuck") {
							reminders = append(reminders, text.Text)
						}
					}
				}
			}
			if !reflect.DeepEqual(reminders, item.Reminders) || !reflect.DeepEqual(continuations, item.Continuations) {
				t.Fatal("nudge did not ride the paired result/continuation")
			}
			if err := protocol.ValidateTranscript(session.Messages()); err != nil {
				t.Fatal(err)
			}
		})
	}
}
