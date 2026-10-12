package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
)

type greetingExtensionInput struct{ Name string }

func (greetingExtensionInput) Validate() error { return nil }

type greetingExtensionCodec struct{ trace *[]string }

func (codec greetingExtensionCodec) DecodeExtensionInput(ctx context.Context, input protocol.ToolInput) (greetingExtensionInput, error) {
	*codec.trace = append(*codec.trace, "decode")
	data, err := input.MarshalJSON()
	if err != nil {
		return greetingExtensionInput{}, err
	}
	value, err := jsonvalue.Decode(string(data))
	if err != nil {
		return greetingExtensionInput{}, err
	}
	name := "default"
	if field, found := value.Lookup("name"); found {
		var ok bool
		name, ok = field.Text()
		if !ok {
			return greetingExtensionInput{}, errors.New("name must be text")
		}
	}
	return greetingExtensionInput{name}, nil
}

type boundaryExtensionInput struct {
	State  string
	Cancel context.CancelFunc
}

func (input boundaryExtensionInput) Validate() error {
	switch input.State {
	case "validation-error":
		return errors.New("invalid extension")
	case "validation-panic":
		panic("private validation data")
	case "validation-cancel":
		input.Cancel()
	}
	return nil
}

type boundaryExtensionCodec struct {
	State  string
	Cancel context.CancelFunc
	Calls  *int
}

func (codec boundaryExtensionCodec) DecodeExtensionInput(context.Context, protocol.ToolInput) (boundaryExtensionInput, error) {
	*codec.Calls++
	switch codec.State {
	case "decode-error":
		return boundaryExtensionInput{}, errors.New("invalid encoding")
	case "decode-panic":
		panic("private codec data")
	case "decode-cancel":
		codec.Cancel()
	}
	return boundaryExtensionInput{State: codec.State, Cancel: codec.Cancel}, nil
}

type boundaryExtensionHandler struct{ Calls *int }

func (handler boundaryExtensionHandler) ExecuteExtensionTool(_ context.Context, _ ToolAuthority, input boundaryExtensionInput) (string, error) {
	*handler.Calls++
	if input.State == "handler-panic" {
		panic("private handler data")
	}
	return "done", nil
}

func TestTypedExtensionCallbackBoundaries(t *testing.T) {
	input, err := protocol.DecodeModelToolInput("greet", []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"decode-error", "decode-panic", "decode-cancel", "validation-error", "validation-panic", "validation-cancel", "handler-panic", "already-cancelled"} {
		t.Run(state, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			decodeCalls, handleCalls := 0, 0
			adapter := extensionToolAdapter[boundaryExtensionInput]{name: "greet", codec: boundaryExtensionCodec{state, cancel, &decodeCalls}, handler: boundaryExtensionHandler{&handleCalls}}
			if state == "already-cancelled" {
				cancel()
			}
			out, err := adapter.ExecuteTool(ctx, gateTestAuthority(ModeAuto), input)
			if err == nil || out != "" || strings.Contains(err.Error(), "private") {
				t.Fatal(out, err)
			}
			wantDecode, wantHandle := 1, 0
			if state == "already-cancelled" {
				wantDecode = 0
			}
			if state == "handler-panic" {
				wantHandle = 1
			}
			if decodeCalls != wantDecode || handleCalls != wantHandle {
				t.Fatal(decodeCalls, handleCalls)
			}
			if strings.Contains(state, "cancel") && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		})
	}
}

func TestTypedExtensionConstructorAndBinding(t *testing.T) {
	trace, received := []string{}, []string{}
	codec, handler := greetingExtensionCodec{&trace}, greetingExtensionHandler{&trace, &received}
	traits := ToolTraits{Risk: RiskRead, Readonly: true}
	schema := protocol.ToolSchema{Name: "greet", Description: "custom", InputSchema: protocol.InputSchema{Type: protocol.SchemaObject}}
	for _, name := range []protocol.ToolName{"bash", "read_file", "mcp__server__tool", ""} {
		copy := schema
		copy.Name = name
		if _, err := NewTypedExtensionTool(copy, traits, codec, handler); err == nil {
			t.Fatalf("accepted reserved/invalid name %q", name)
		}
	}
	if _, err := NewTypedExtensionTool[greetingExtensionInput](schema, traits, nil, handler); err == nil {
		t.Fatal("accepted nil codec")
	}
	if _, err := NewTypedExtensionTool[greetingExtensionInput](schema, traits, codec, nil); err == nil {
		t.Fatal("accepted nil handler")
	}
	definition, err := NewTypedExtensionTool(schema, traits, codec, handler)
	if err != nil {
		t.Fatal(err)
	}
	other, err := protocol.DecodeModelToolInput("other", []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []protocol.ToolInput{other, protocol.BashToolInput(protocol.BashInput{Command: "echo wrong"})} {
		if _, err := definition.handler.ExecuteTool(context.Background(), gateTestAuthority(ModeAuto), input); err == nil {
			t.Fatal("accepted wrong binding")
		}
	}
	if len(trace) != 0 || len(received) != 0 {
		t.Fatal(trace, received)
	}
	// A typed nil can inhabit an interface. Contain its callback panic exactly
	// as other extension failures instead of letting it escape into the loop.
	var nilCodec *greetingExtensionCodec
	definition, err = NewTypedExtensionTool(schema, traits, nilCodec, handler)
	if err != nil {
		t.Fatal(err)
	}
	input, _ := protocol.DecodeModelToolInput("greet", []byte(`{}`))
	if _, err := definition.handler.ExecuteTool(context.Background(), gateTestAuthority(ModeAuto), input); err == nil {
		t.Fatal("typed nil callback escaped")
	}
}

func TestTypedExtensionJournalReplayAndCurrentPermission(t *testing.T) {
	trace, received := []string{}, []string{}
	definition, err := NewTypedExtensionTool(protocol.ToolSchema{Name: "greet", Description: "custom", InputSchema: protocol.InputSchema{Type: protocol.SchemaObject}}, ToolTraits{Risk: RiskWrite}, greetingExtensionCodec{&trace}, greetingExtensionHandler{&trace, &received})
	if err != nil {
		t.Fatal(err)
	}
	catalog, _ := NewToolCatalog(definition)
	journal, _ := NewInMemoryActionJournal(2)
	gate, err := NewJournaledToolGate(catalog, DefaultPermissionPolicy(nil), GateHooks{}, journal)
	if err != nil {
		t.Fatal(err)
	}
	input, _ := protocol.DecodeModelToolInput("greet", []byte(`{"name":"World"}`))
	call := ToolCall{ID: "custom", Input: input}
	authority := gateTestAuthority(ModeAuto)
	authority.RunContext = actionContext(t)
	first, err := gate.Dispatch(context.Background(), authority, call)
	if err != nil || first.Output != "Hello, World!" || first.Replayed {
		t.Fatal(first, err)
	}
	replay, err := gate.Dispatch(context.Background(), authority, call)
	if err != nil || !replay.Replayed || replay.Output != first.Output || !reflect.DeepEqual(trace, []string{"decode", "handle"}) {
		t.Fatal(replay, trace, err)
	}
	authority.Mode = ModeReadonly
	denied, err := gate.Dispatch(context.Background(), authority, call)
	if err != nil || !denied.Denied || denied.Replayed || len(received) != 1 {
		t.Fatal(denied, received, err)
	}
}

type extensionReplyProvider struct{ Requests []protocol.ModelRequest }

func (provider *extensionReplyProvider) Complete(_ context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
	provider.Requests = append(provider.Requests, request.Clone())
	if len(provider.Requests) > 1 {
		return fakeReply([]protocol.Block{protocol.NewTextBlock("done")}, protocol.StopEndTurn), nil
	}
	input, err := protocol.DecodeModelToolInput("greet", []byte(`{"name":"World"}`))
	return fakeReply([]protocol.Block{protocol.NewToolUse("custom", input)}, protocol.StopToolUse), err
}

func TestTypedExtensionAgentLoopPairsOriginalObservation(t *testing.T) {
	trace, received := []string{}, []string{}
	definition, err := NewTypedExtensionTool(protocol.ToolSchema{Name: "greet", Description: "custom", InputSchema: protocol.InputSchema{Type: protocol.SchemaObject}}, ToolTraits{Risk: RiskRead, Readonly: true}, greetingExtensionCodec{&trace}, greetingExtensionHandler{&trace, &received})
	if err != nil {
		t.Fatal(err)
	}
	catalog, _ := NewToolCatalog(definition)
	gate, _ := NewToolGate(catalog, DefaultPermissionPolicy(nil), GateHooks{})
	provider := &extensionReplyProvider{}
	session, err := NewSessionWithGate("s", "owner", provider, gate, ModeAuto, "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Run(context.Background(), "greet"); err != nil {
		t.Fatal(err)
	}
	if len(provider.Requests) != 2 || !reflect.DeepEqual(received, []string{"World"}) {
		t.Fatal(provider.Requests, received)
	}
	messages := session.Messages()
	blocks, _ := messages[1].Content.Blocks()
	use, _ := blocks[0].ToolUse()
	if _, ok := use.Input.UnknownModelToolObservation(); !ok {
		t.Fatal("codec payload entered transcript")
	}
	blocks, _ = messages[2].Content.Blocks()
	result, _ := blocks[0].ToolResult()
	if result.ToolUseID != "custom" || result.IsError || result.Content != "Hello, World!" {
		t.Fatal(result)
	}
	assertToolResultTelemetry(t, session, "custom", false, false)
}

type greetingExtensionHandler struct{ trace, received *[]string }

func (handler greetingExtensionHandler) ExecuteExtensionTool(ctx context.Context, authority ToolAuthority, input greetingExtensionInput) (string, error) {
	*handler.trace = append(*handler.trace, "handle")
	*handler.received = append(*handler.received, input.Name)
	if input.Name == "boom" {
		return "", errors.New("extension failed")
	}
	return "Hello, " + input.Name + "!", nil
}

func TestTypedExtensionMatchesSourceRegisteredTool(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-extension-tools.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name, Mode, Risk, Output string
			Input                    string `json:"input_json"`
			Rewrite                  bool
			BeforeDeny               bool `json:"before_deny"`
			GuardDeny                bool `json:"guard_deny"`
			Trace, Received          []string
			Results                  []struct {
				Output         string
				Denied, Failed bool
			}
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil || len(fixture.Cases) != 14 {
		t.Fatal(err, len(fixture.Cases))
	}
	for _, row := range fixture.Cases {
		t.Run(row.Name, func(t *testing.T) {
			trace, received := []string{}, []string{}
			definition, err := NewTypedExtensionTool(protocol.ToolSchema{Name: "greet", Description: "Typed extension test", InputSchema: protocol.InputSchema{Type: protocol.SchemaObject}}, ToolTraits{Risk: ToolRisk(row.Risk), Readonly: row.Risk == "read"}, greetingExtensionCodec{&trace}, greetingExtensionHandler{&trace, &received})
			if err != nil {
				t.Fatal(err)
			}
			catalog, err := NewToolCatalog(definition)
			if err != nil {
				t.Fatal(err)
			}
			var observed ToolOutcome
			gate, err := NewToolGate(catalog, DefaultPermissionPolicy(nil), GateHooks{
				Before: []BeforeHook{beforeHookFunc(func(context.Context, ToolAuthority, ToolCall) (BeforeDecision, error) {
					trace = append(trace, "before")
					if row.BeforeDeny {
						return DenyToolCall("before blocked"), nil
					}
					if row.Rewrite {
						input, err := protocol.DecodeModelToolInput("greet", []byte(`{"name":"rewritten"}`))
						return RewriteToolCall(input), err
					}
					return KeepToolCall(), nil
				})},
				Guards: []GuardHook{guardHookFunc(func(context.Context, ToolAuthority, ToolCall) (string, bool, error) {
					trace = append(trace, "guard")
					return "guard blocked", row.GuardDeny, nil
				})},
				After: []AfterHook{afterHookFunc(func(_ context.Context, _ ToolAuthority, _ ToolCall, text string) (string, error) {
					trace = append(trace, "after")
					return text + "|after", nil
				})},
				Observers: []ResultObserver{observerFunc(func(_ context.Context, _ ToolAuthority, _ ToolCall, out ToolOutcome) error {
					trace = append(trace, "result")
					observed = out
					return nil
				})},
			})
			if err != nil {
				t.Fatal(err)
			}
			input, err := protocol.DecodeModelToolInput("greet", []byte(row.Input))
			if err != nil {
				t.Fatal(err)
			}
			out, err := gate.Dispatch(context.Background(), gateTestAuthority(PermissionMode(row.Mode)), ToolCall{ID: "custom", Input: input})
			if err != nil || out.Output != row.Output || out.Denied != row.Results[0].Denied || out.Failed != row.Results[0].Failed || observed.Output != row.Output || !reflect.DeepEqual(received, row.Received) {
				t.Fatal("Source outcome differs", out, row.Output, received, row.Received, err)
			}
			if row.Name == "readonly-write" {
				// Native guards precede immutable permission evaluation. Source's
				// default PermissionHook can deny from before_tool; retain this
				// existing scheduling adaptation instead of hiding its trace.
				if !reflect.DeepEqual(trace, []string{"before", "guard", "result"}) || !reflect.DeepEqual(row.Trace, []string{"before", "result"}) {
					t.Fatal("readonly adaptation changed", trace, row.Trace)
				}
			} else if !reflect.DeepEqual(trace, row.Trace) {
				t.Fatal("Source hook/codec order differs", trace, row.Trace)
			}
		})
	}
}
