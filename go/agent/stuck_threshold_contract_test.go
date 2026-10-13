package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/protocol"
)

// These rows are emitted by the public Python constructors and actual Agent
// turns. They are external observations, not reimplementations of its rules.
type stuckThresholdFault struct {
	ExceptionClass string `json:"exception_class"`
	Message        string `json:"message"`
	Stage          string `json:"stage"`
}

type stuckThresholdCase struct {
	Name               string               `json:"name"`
	Construction       string               `json:"construction"`
	Requested          *StuckThresholds     `json:"requested"`
	Thresholds         *StuckThresholds     `json:"thresholds"`
	MaxNudges          *int                 `json:"max_nudges"`
	Steps              []ToolStep           `json:"steps"`
	RoundsWithoutTools int                  `json:"rounds_without_tools"`
	ConstructorError   *stuckThresholdFault `json:"constructor_error"`
	InspectionError    *stuckThresholdFault `json:"inspection_error"`
	Signal             *StuckSignal         `json:"signal"`
	Reminder           *string              `json:"reminder"`
}

type stuckThresholdRequest struct {
	Messages []protocol.Message   `json:"messages"`
	Error    *stuckThresholdFault `json:"error"`
}

// Pointers retain absence versus false or empty text in the captured projection.
// Span IDs and wall-clock timing are excluded by the producer, while selected
// payloads and their order remain actual emitted values.
type stuckThresholdEvent struct {
	Type            SessionEventKind          `json:"type"`
	Agent           *string                   `json:"agent,omitempty"`
	Depth           *int                      `json:"depth,omitempty"`
	MessageID       *MessageID                `json:"message_id,omitempty"`
	ParentMessageID *MessageID                `json:"parent_message_id,omitempty"`
	Name            *protocol.ToolName        `json:"name,omitempty"`
	Input           *protocol.ToolInput       `json:"input,omitempty"`
	ID              *string                   `json:"id,omitempty"`
	Output          *string                   `json:"output,omitempty"`
	Error           *stuckThresholdEventError `json:"error,omitempty"`
	Denied          *bool                     `json:"denied,omitempty"`
	Replayed        *bool                     `json:"replayed,omitempty"`
	Pattern         *StuckPattern             `json:"pattern,omitempty"`
	Detail          *string                   `json:"detail,omitempty"`
	Tool            *protocol.ToolName        `json:"tool"`
	Halted          *bool                     `json:"halted,omitempty"`
	NudgesUsed      *int                      `json:"nudges_used,omitempty"`
}

func (event *stuckThresholdEvent) UnmarshalJSON(data []byte) error {
	value, err := decodeStuckThresholdEvent(data, true)
	if err != nil {
		return err
	}
	*event = value
	return nil
}

func decodeStuckThresholdEvent(data []byte, strict bool) (stuckThresholdEvent, error) {
	// Tool input is the sibling-name union at this external boundary. Its
	// bytes are decoded immediately and never retained in the fixture value.
	type eventWire struct {
		Type            SessionEventKind          `json:"type"`
		Agent           *string                   `json:"agent,omitempty"`
		Depth           *int                      `json:"depth,omitempty"`
		MessageID       *MessageID                `json:"message_id,omitempty"`
		ParentMessageID *MessageID                `json:"parent_message_id,omitempty"`
		Name            *protocol.ToolName        `json:"name,omitempty"`
		Input           json.RawMessage           `json:"input,omitempty"`
		ID              *string                   `json:"id,omitempty"`
		Output          *string                   `json:"output,omitempty"`
		Error           *stuckThresholdEventError `json:"error,omitempty"`
		Denied          *bool                     `json:"denied,omitempty"`
		Replayed        *bool                     `json:"replayed,omitempty"`
		Pattern         *StuckPattern             `json:"pattern,omitempty"`
		Detail          *string                   `json:"detail,omitempty"`
		Tool            *protocol.ToolName        `json:"tool"`
		Halted          *bool                     `json:"halted,omitempty"`
		NudgesUsed      *int                      `json:"nudges_used,omitempty"`
	}
	var wire eventWire
	decoder := json.NewDecoder(bytes.NewReader(data))
	if strict {
		decoder.DisallowUnknownFields()
	}
	if err := decoder.Decode(&wire); err != nil {
		return stuckThresholdEvent{}, err
	}
	value := stuckThresholdEvent{Type: wire.Type, Agent: wire.Agent, Depth: wire.Depth, MessageID: wire.MessageID, ParentMessageID: wire.ParentMessageID, Name: wire.Name, ID: wire.ID, Output: wire.Output, Error: wire.Error, Denied: wire.Denied, Replayed: wire.Replayed, Pattern: wire.Pattern, Detail: wire.Detail, Tool: wire.Tool, Halted: wire.Halted, NudgesUsed: wire.NudgesUsed}
	if len(wire.Input) != 0 {
		if wire.Name == nil {
			return stuckThresholdEvent{}, errors.New("stuck fixture tool input is missing its sibling name")
		}
		input, err := protocol.DecodeToolInput(*wire.Name, wire.Input)
		if err != nil {
			return stuckThresholdEvent{}, err
		}
		value.Input = &input
	}
	return value, nil
}

type stuckThresholdEventError struct {
	Failed *bool
	Text   *string
}

func (value *stuckThresholdEventError) UnmarshalJSON(data []byte) error {
	var failed bool
	if err := json.Unmarshal(data, &failed); err == nil {
		value.Failed = &failed
		return nil
	}
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return err
	}
	value.Text = &text
	return nil
}

type stuckThresholdTurn struct {
	Prompt             string                   `json:"prompt"`
	Output             *string                  `json:"output"`
	Error              *stuckThresholdFault     `json:"error"`
	Requests           []stuckThresholdRequest  `json:"requests"`
	Events             []stuckThresholdEvent    `json:"events"`
	EventTypes         []SessionEventKind       `json:"event_types"`
	HandlerCommands    []string                 `json:"handler_commands"`
	RecentSteps        []ToolStep               `json:"recent_steps"`
	Messages           []protocol.Message       `json:"messages"`
	RoundsWithoutTools int                      `json:"rounds_without_tools"`
	NudgesUsed         int                      `json:"nudges_used"`
	LastText           string                   `json:"last_text"`
	TranscriptError    *stuckThresholdFault     `json:"transcript_error"`
	ModelCalls         int                      `json:"model_calls"`
	SessionStatus      *SessionStatus           `json:"session_status,omitempty"`
	Busy               *bool                    `json:"busy,omitempty"`
	RunCount           *int                     `json:"run_count,omitempty"`
	TerminalEvents     []stuckThresholdTerminal `json:"terminal_events,omitempty"`
}

type stuckThresholdTerminal struct {
	Type   SessionEventKind `json:"type"`
	Status *SessionStatus   `json:"status,omitempty"`
	Error  *string          `json:"error,omitempty"`
	Text   *string          `json:"text,omitempty"`
	Phase  *TextPhase       `json:"phase,omitempty"`
}

type stuckThresholdLoop struct {
	Name         string               `json:"name"`
	Thresholds   StuckThresholds      `json:"thresholds"`
	Pattern      string               `json:"pattern"`
	FinishAfter  int                  `json:"finish_after"`
	MaxRounds    int                  `json:"max_rounds"`
	Construction string               `json:"construction"`
	TurnCount    int                  `json:"turn_count"`
	Turns        []stuckThresholdTurn `json:"turns"`
}

type stuckThresholdFixture struct {
	SourceSHA256 map[string]string      `json:"source_sha256"`
	Versions     stuckThresholdVersions `json:"versions"`
	Scope        []string               `json:"scope"`
	Cases        []stuckThresholdCase   `json:"cases"`
	Loops        []stuckThresholdLoop   `json:"loops"`
	ManagedLoops []stuckThresholdLoop   `json:"managed_loops"`
}

type stuckThresholdVersions struct {
	Python string `json:"python"`
}

func readStuckThresholdFixture(t *testing.T) stuckThresholdFixture {
	t.Helper()
	file, err := os.Open("../testdata/python-stuck-thresholds.json")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var fixture stuckThresholdFixture
	if err := decoder.Decode(&fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 61 || len(fixture.Loops) != 12 || len(fixture.ManagedLoops) != 2 {
		t.Fatalf("actual Source inventory = %d cases/%d bare loops/%d managed loops; want 61/12/2", len(fixture.Cases), len(fixture.Loops), len(fixture.ManagedLoops))
	}
	return fixture
}

func assertStuckThresholdFault(t *testing.T, err error, want *stuckThresholdFault) {
	t.Helper()
	if want == nil {
		if err != nil {
			t.Fatalf("unexpected native fault: %v", err)
		}
		return
	}
	if err == nil {
		t.Fatalf("missing Source fault %s: %s (%s)", want.ExceptionClass, want.Message, want.Stage)
	}
	var fault *StuckInspectionError
	if !errors.As(err, &fault) || string(fault.ExceptionClass()) != want.ExceptionClass || fault.Error() != want.Message {
		t.Fatalf("native fault %T: %v; want %s: %s (%s)", err, err, want.ExceptionClass, want.Message, want.Stage)
	}
}

func TestStuckThresholdsMatchActualPythonConstructorsAndInspection(t *testing.T) {
	fixture := readStuckThresholdFixture(t)
	seen := make(map[string]bool, len(fixture.Cases))
	for _, row := range fixture.Cases {
		t.Run(row.Name, func(t *testing.T) {
			if row.Name == "" || seen[row.Name] {
				t.Fatal("constructor observation names must be unique and nonempty")
			}
			seen[row.Name] = true
			var detector DefaultStuckDetector
			var err error
			switch row.Construction {
			case "omitted", "none":
				if row.Requested != nil {
					t.Fatal("default constructor unexpectedly carries configured thresholds")
				}
				detector = NewDefaultStuckDetector()
			case "configured":
				if row.Requested == nil {
					t.Fatal("configured constructor is missing its six named controls")
				}
				detector, err = NewStuckDetector(*row.Requested)
			default:
				t.Fatalf("unknown construction %q", row.Construction)
			}
			assertStuckThresholdFault(t, err, row.ConstructorError)
			if err != nil {
				return
			}
			if row.Thresholds == nil || detector.thresholds != *row.Thresholds || row.MaxNudges == nil || detector.MaxNudges() != *row.MaxNudges {
				t.Fatalf("constructor lost threshold values: %#v, max_nudges=%d; want %#v, %#v", detector.thresholds, detector.MaxNudges(), row.Thresholds, row.MaxNudges)
			}
			signal, inspectErr := detector.Inspect(StuckState{Steps: row.Steps, RoundsWithoutTools: row.RoundsWithoutTools})
			assertStuckThresholdFault(t, inspectErr, row.InspectionError)
			if inspectErr != nil {
				var fault *StuckInspectionError
				if !errors.As(inspectErr, &fault) {
					t.Fatal("inspection lost its named fault")
				}
				wantPattern := StuckRepeatResult
				if strings.HasPrefix(row.Name, "repeat_action_error-") {
					wantPattern = StuckRepeatError
				}
				if fault.Pattern != wantPattern {
					t.Fatalf("fault pattern %q; want %q", fault.Pattern, wantPattern)
				}
			}
			if !reflect.DeepEqual(signal, row.Signal) {
				t.Fatalf("signal %#v; want %#v", signal, row.Signal)
			}
			var reminder *string
			if signal != nil {
				text := signal.Reminder()
				reminder = &text
			}
			if !reflect.DeepEqual(reminder, row.Reminder) {
				t.Fatalf("reminder %#v; want %#v", reminder, row.Reminder)
			}
		})
	}
}

type stuckThresholdHandler struct{ commands []string }

func (handler *stuckThresholdHandler) ExecuteTool(_ context.Context, _ ToolAuthority, input protocol.ToolInput) (string, error) {
	bash, ok := input.Bash()
	if !ok {
		return "", errors.New("stuck fixture received a non-bash input")
	}
	handler.commands = append(handler.commands, bash.Command)
	return "same", nil
}

type stuckThresholdProvider struct {
	pattern     string
	finishAfter int
	turnCalls   int
	calls       int
	requests    []stuckThresholdRequest
}

func (provider *stuckThresholdProvider) Complete(_ context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
	provider.requests = append(provider.requests, stuckThresholdRequest{Messages: request.Clone().Messages})
	if err := request.Validate(); err != nil {
		return protocol.ModelReply{}, err
	}
	provider.calls++
	provider.turnCalls++
	if provider.finishAfter > 0 && provider.turnCalls > provider.finishAfter {
		return fakeReply([]protocol.Block{protocol.NewTextBlock("done")}, protocol.StopEndTurn), nil
	}
	command := "same"
	switch provider.pattern {
	case "same":
	case "varied-denied":
		command = fmt.Sprintf("q%d", provider.turnCalls)
	case "alternating":
		command = "a"
		if provider.turnCalls%2 == 0 {
			command = "b"
		}
	default:
		return protocol.ModelReply{}, fmt.Errorf("unknown stuck fixture pattern %q", provider.pattern)
	}
	use := protocol.NewToolUseWithoutCaller(fmt.Sprintf("u%d", provider.calls), protocol.BashToolInput(protocol.BashInput{Command: command}))
	return fakeReply([]protocol.Block{protocol.NewTextBlock("loop commentary"), use}, protocol.StopToolUse), nil
}

type stuckThresholdSystemBuilder struct{}

func (stuckThresholdSystemBuilder) BuildSystem(SystemContext) (string, error) {
	return "Stuck threshold contract.", nil
}

func stuckThresholdCatalog(t *testing.T, handler ToolHandler) *ToolCatalog {
	t.Helper()
	var schema protocol.ToolSchema
	if err := json.Unmarshal([]byte(`{"name":"bash","description":"Stuck fixture tool.","input_schema":{"type":"object","properties":{"command":{"type":"string"}},"required":["command"]}}`), &schema); err != nil {
		t.Fatal(err)
	}
	definition, err := NewToolDefinitionWithSchema(schema, ToolTraits{Risk: RiskRead, Readonly: true}, handler)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := NewToolCatalog(definition)
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

func stuckThresholdEvents(t *testing.T, records []SessionEventRecord) []stuckThresholdEvent {
	t.Helper()
	events := make([]stuckThresholdEvent, 0)
	for _, record := range records {
		switch record.Event.Kind() {
		case EventToolUse, EventToolResult, EventStuck, EventError:
		default:
			continue
		}
		data, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		// Native records carry additional known event-header/timing fields;
		// compare the producer's declared projection, not those dynamic fields.
		event, err := decodeStuckThresholdEvent(data, false)
		if err != nil {
			t.Fatal(err)
		}
		events = append(events, event)
	}
	return events
}

func assertStuckThresholdMessages(t *testing.T, got, want []protocol.Message, label string) {
	t.Helper()
	actual, actualErr := json.Marshal(got)
	expected, expectedErr := json.Marshal(want)
	if actualErr != nil || expectedErr != nil {
		t.Fatalf("%s history encoding: %v / %v", label, actualErr, expectedErr)
	}
	if !bytes.Equal(actual, expected) {
		t.Errorf("%s history changed\ngot: %s\nwant: %s", label, actual, expected)
	}
}

func newStuckThresholdLoop(t *testing.T, row stuckThresholdLoop, managed bool) (*Session, *ManagedSession, *stuckThresholdProvider, *stuckThresholdHandler) {
	t.Helper()
	provider := &stuckThresholdProvider{pattern: row.Pattern, finishAfter: row.FinishAfter}
	handler := &stuckThresholdHandler{commands: make([]string, 0)}
	var detector DefaultStuckDetector
	var err error
	if row.Construction == "omitted" {
		detector = NewDefaultStuckDetector()
	} else {
		detector, err = NewStuckDetector(row.Thresholds)
	}
	if err != nil {
		t.Fatal(err)
	}
	hooks := GateHooks{}
	if row.Pattern == "varied-denied" {
		hooks.Before = []BeforeHook{beforeHookFunc(func(context.Context, ToolAuthority, ToolCall) (BeforeDecision, error) {
			return DenyToolCall("Error: permission denied by policy"), nil
		})}
	}
	catalog := stuckThresholdCatalog(t, handler)
	if managed {
		config := RuntimeConfig{ID: "main", Owner: "fixture-human", Provider: provider, Bash: echoExecutor{}, Workspace: t.TempDir(), Mode: ModeAuto, MaxRounds: row.MaxRounds, Tools: catalog, Hooks: hooks, Model: "fixture-model", MaxTokens: 64, SystemBuilder: stuckThresholdSystemBuilder{}, CachePolicy: NullCachePolicy{}, StuckDetector: detector, Injectors: []MessageInjector{}, StopHooks: []StopHook{}}
		outer, err := NewManagedSession(config)
		if err != nil {
			t.Fatal(err)
		}
		return outer.core, outer, provider, handler
	}
	gate, err := NewToolGate(catalog, DefaultPermissionPolicy(nil), hooks)
	if err != nil {
		t.Fatal(err)
	}
	core, err := NewSessionWithGate("main", "fixture-human", provider, gate, ModeAuto, "", row.MaxRounds)
	if err != nil {
		t.Fatal(err)
	}
	core.stuckDetector, core.cachePolicy = detector, NullCachePolicy{}
	core.model, core.maxTokens, core.systemBuilder = "fixture-model", 64, stuckThresholdSystemBuilder{}
	core.stopHooks = []StopHook{}
	return core, nil, provider, handler
}

func stuckThresholdRunContext(t *testing.T, turn int) RunContext {
	t.Helper()
	actor := ActorID("fixture-human")
	run, err := ExplicitHumanRunContext(HumanRunConfig{ActorID: &actor})
	if err != nil {
		t.Fatal(err)
	}
	run.messageID = MessageID(fmt.Sprintf("stuck-turn-%d", turn))
	return run
}

func assertStuckThresholdTurn(t *testing.T, core *Session, provider *stuckThresholdProvider, handler *stuckThresholdHandler, row stuckThresholdTurn, output string, err error, records []SessionEventRecord) {
	t.Helper()
	assertStuckThresholdFault(t, err, row.Error)
	if row.Output == nil {
		if output != "" {
			t.Errorf("fault returned %q; Source returned no output", output)
		}
	} else if output != *row.Output {
		t.Errorf("output %q; want %q", output, *row.Output)
	}
	if provider.turnCalls != row.ModelCalls || len(provider.requests) != len(row.Requests) {
		t.Errorf("model accepted calls/requests = %d/%d; Source = %d/%d", provider.turnCalls, len(provider.requests), row.ModelCalls, len(row.Requests))
	}
	for index, request := range provider.requests {
		if index >= len(row.Requests) {
			break
		}
		assertStuckThresholdMessages(t, request.Messages, row.Requests[index].Messages, fmt.Sprintf("request %d", index))
		if row.Requests[index].Error != nil {
			t.Errorf("Source request %d failed at %s, matching turn unexpectedly crossed provider admission", index, row.Requests[index].Error.Stage)
		}
	}
	if !reflect.DeepEqual(handler.commands, row.HandlerCommands) {
		t.Errorf("handler commands %q; want %q", handler.commands, row.HandlerCommands)
	}
	steps := append([]ToolStep{}, core.recentSteps...)
	if !reflect.DeepEqual(steps, row.RecentSteps) || core.roundsWithoutTools != row.RoundsWithoutTools || core.stuckNudges != row.NudgesUsed {
		t.Errorf("state steps=%#v rounds=%d nudges=%d; Source steps=%#v rounds=%d nudges=%d", steps, core.roundsWithoutTools, core.stuckNudges, row.RecentSteps, row.RoundsWithoutTools, row.NudgesUsed)
	}
	assertStuckThresholdMessages(t, core.Messages(), row.Messages, "retained")
	if got := stuckThresholdEvents(t, records); !reflect.DeepEqual(got, row.Events) {
		t.Errorf("event payload/order changed\ngot: %#v\nwant: %#v", got, row.Events)
	}
	transcriptErr := protocol.ValidateTranscript(core.Messages())
	if (transcriptErr == nil) != (row.TranscriptError == nil) {
		t.Errorf("native transcript validation = %v; Source = %#v", transcriptErr, row.TranscriptError)
	}
}

func TestStuckThresholdAgentTurnsMatchActualPython(t *testing.T) {
	for _, row := range readStuckThresholdFixture(t).Loops {
		t.Run(row.Name, func(t *testing.T) {
			core, _, provider, handler := newStuckThresholdLoop(t, row, false)
			for index, turn := range row.Turns {
				if index > 0 && row.Turns[index-1].Error != nil {
					// The separately named boundary test retains and checks the
					// rejected Source provider attempt and native earlier refusal.
					break
				}
				provider.turnCalls, provider.requests = 0, nil
				handler.commands = make([]string, 0)
				beforeEvents := len(core.Events())
				output, err := core.RunWithContext(context.Background(), turn.Prompt, stuckThresholdRunContext(t, index+1))
				assertStuckThresholdTurn(t, core, provider, handler, turn, output, err, core.Events()[beforeEvents:])
			}
		})
	}
}

// This is evidence of an open caller-stage difference, not a parity pass:
// Source attempts Fake.messages.create and catches InvalidTranscript; native
// Session's compactor transcript validation refuses before Provider.Complete.
// Both retain the actual first fault.
func TestStuckThresholdPostFaultNativeIngressRemainsEarlierThanSource(t *testing.T) {
	observed := 0
	for _, row := range readStuckThresholdFixture(t).Loops {
		if len(row.Turns) != 2 || row.Turns[0].Error == nil {
			continue
		}
		observed++
		t.Run(row.Name, func(t *testing.T) {
			core, _, provider, handler := newStuckThresholdLoop(t, row, false)
			first := row.Turns[0]
			output, err := core.RunWithContext(context.Background(), first.Prompt, stuckThresholdRunContext(t, 1))
			assertStuckThresholdTurn(t, core, provider, handler, first, output, err, core.Events())
			second := row.Turns[1]
			if len(second.Requests) != 1 || second.Requests[0].Error == nil || second.Requests[0].Error.Stage != "provider_create" || second.ModelCalls != 0 || len(second.HandlerCommands) != 0 {
				t.Fatalf("Source post-fault provider observation missing: %#v", second)
			}
			beforeHistory := core.Messages()
			provider.turnCalls, provider.requests = 0, nil
			handler.commands = make([]string, 0)
			output, err = core.RunWithContext(context.Background(), second.Prompt, stuckThresholdRunContext(t, 2))
			if err == nil || !strings.HasPrefix(err.Error(), "compactor transcript:") || output != "" || len(provider.requests) != 0 || provider.turnCalls != 0 || len(handler.commands) != 0 {
				t.Fatalf("native post-fault admission changed: output=%q err=%v requests=%d calls=%d commands=%q", output, err, len(provider.requests), provider.turnCalls, handler.commands)
			}
			wantNative := append(beforeHistory, protocol.Message{Role: protocol.RoleUser, Content: protocol.PlainContent(second.Prompt)})
			assertStuckThresholdMessages(t, core.Messages(), wantNative, "native refused")
			assertStuckThresholdMessages(t, core.Messages(), second.Requests[0].Messages, "Source rejected provider request")
			if second.Output == nil || !strings.HasPrefix(*second.Output, "[Error] ") || second.Error != nil || len(second.Messages) != len(core.Messages())+1 || second.TranscriptError == nil {
				t.Fatalf("Source caught fault/retained error reply missing: %#v", second)
			}
			t.Logf("open boundary: Source %s at %s is caught into %q; native compactor transcript validation refuses before Provider.Complete with %q", second.Requests[0].Error.ExceptionClass, second.Requests[0].Error.Stage, *second.Output, err.Error())
		})
	}
	if observed != 2 {
		t.Fatalf("post-fault Source inventory = %d; want both repeat rules", observed)
	}
}

func TestStuckThresholdManagedInitialFaultMatchesActualPythonTerminal(t *testing.T) {
	for _, row := range readStuckThresholdFixture(t).ManagedLoops {
		t.Run(row.Name, func(t *testing.T) {
			if len(row.Turns) != 2 || row.Turns[0].Error == nil {
				t.Fatal("managed fixture must retain its actual first fault and later turn")
			}
			core, managed, provider, handler := newStuckThresholdLoop(t, row, true)
			first := row.Turns[0]
			output, err := managed.RunWithContext(context.Background(), first.Prompt, stuckThresholdRunContext(t, 1))
			// The bare Agent effects/history and its scoped events match. Wrapper
			// status/error events use the producer's explicit terminal projection;
			// this does not assert wrapper event identity-header equivalence.
			bareFirst := first
			bareFirst.Events = make([]stuckThresholdEvent, 0)
			for _, event := range first.Events {
				if event.Type != EventError {
					bareFirst.Events = append(bareFirst.Events, event)
				}
			}
			coreRecords := make([]SessionEventRecord, 0)
			terminals := make([]stuckThresholdTerminal, 0)
			for _, record := range managed.Events() {
				switch record.Event.Kind() {
				case EventStatus, EventError, EventDone:
					data, marshalErr := json.Marshal(record)
					if marshalErr != nil {
						t.Fatal(marshalErr)
					}
					var terminal stuckThresholdTerminal
					if decodeErr := json.Unmarshal(data, &terminal); decodeErr != nil {
						t.Fatal(decodeErr)
					}
					terminals = append(terminals, terminal)
				default:
					coreRecords = append(coreRecords, record)
				}
			}
			assertStuckThresholdTurn(t, core, provider, handler, bareFirst, output, err, coreRecords)
			if !reflect.DeepEqual(terminals, first.TerminalEvents) {
				t.Errorf("managed terminal outcomes/order = %#v; want %#v", terminals, first.TerminalEvents)
			}
			info := managed.Info()
			if first.SessionStatus == nil || first.Busy == nil || first.RunCount == nil || info.Status != *first.SessionStatus || info.Busy != *first.Busy || info.RunCount != *first.RunCount {
				t.Errorf("managed status/busy/run_count = %s/%v/%d; want %#v/%#v/%#v", info.Status, info.Busy, info.RunCount, first.SessionStatus, first.Busy, first.RunCount)
			}
			second := row.Turns[1]
			if len(second.Requests) != 1 || second.Requests[0].Error == nil || second.Requests[0].Error.Stage != "provider_create" {
				t.Fatal("managed Source post-fault rejected request is missing")
			}
			provider.turnCalls, provider.requests = 0, nil
			handler.commands = make([]string, 0)
			output, err = managed.RunWithContext(context.Background(), second.Prompt, stuckThresholdRunContext(t, 2))
			if err == nil || !strings.HasPrefix(err.Error(), "compactor transcript:") || output != "" || len(provider.requests) != 0 || provider.turnCalls != 0 || len(handler.commands) != 0 {
				t.Fatalf("native managed post-fault boundary changed: output=%q err=%v requests=%d calls=%d commands=%q", output, err, len(provider.requests), provider.turnCalls, handler.commands)
			}
			assertStuckThresholdMessages(t, core.Messages(), second.Requests[0].Messages, "managed Source rejected provider request")
			if second.SessionStatus == nil || second.Output == nil || second.Error != nil || second.TerminalEvents == nil {
				t.Fatal("actual managed Source caught provider outcome is missing")
			}
			t.Logf("open managed boundary: Source status=%s output=%q; native compactor transcript validation returns status=%s err=%q; wrapper identity headers were not compared", *second.SessionStatus, *second.Output, managed.Info().Status, err.Error())
		})
	}
}
