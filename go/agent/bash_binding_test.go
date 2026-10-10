package agent

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/luoyjx/mini-loop/go/protocol"
)

type bindingExecutorProbe struct {
	trace *[]string
	calls int
}

func (p *bindingExecutorProbe) ExecuteTool(_ context.Context, _ ToolAuthority, input protocol.ToolInput) (string, error) {
	p.calls++
	*p.trace = append(*p.trace, "execute")
	value, _ := input.Bash()
	return "executed:" + value.Command, nil
}

func TestBashBindingMatchesActualSourceExecutionAndHookOrder(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-provider-tool-input-consumer.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name, Mode, Output string
			Input              string `json:"input_json"`
			Trace              []string
			Outcomes           []struct {
				Output         string
				Denied, Failed bool
			}
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 51 {
		t.Fatal("Source consumer inventory changed")
	}
	matched := 0
	for _, row := range fixture.Cases {
		t.Run(row.Name, func(t *testing.T) {
			input, err := protocol.DecodeModelToolInput(protocol.ToolBash, []byte(row.Input))
			if err != nil {
				t.Fatal(err)
			}
			trace := []string{}
			executor := &bindingExecutorProbe{trace: &trace}
			policy, err := NewPermissionPolicy(nil, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			var observed ToolOutcome
			gate, err := NewToolGate(gateTestCatalog(t, executor), policy, GateHooks{
				Before: []BeforeHook{beforeHookFunc(func(_ context.Context, _ ToolAuthority, _ ToolCall) (BeforeDecision, error) {
					trace = append(trace, "before")
					switch row.Mode {
					case "rewrite":
						return RewriteToolCall(protocol.BashToolInput(protocol.BashInput{Command: "rewritten"})), nil
					case "deny":
						return DenyToolCall("denied by source probe"), nil
					}
					return KeepToolCall(), nil
				})},
				Guards: []GuardHook{guardHookFunc(func(context.Context, ToolAuthority, ToolCall) (string, bool, error) {
					trace = append(trace, "guard")
					return "", false, nil
				})},
				After: []AfterHook{afterHookFunc(func(_ context.Context, _ ToolAuthority, _ ToolCall, output string) (string, error) {
					trace = append(trace, "after")
					return output, nil
				})},
				Observers: []ResultObserver{observerFunc(func(_ context.Context, _ ToolAuthority, _ ToolCall, outcome ToolOutcome) error {
					trace = append(trace, "result")
					observed = outcome
					return nil
				})},
			})
			if err != nil {
				t.Fatal(err)
			}
			outcome, err := gate.Dispatch(context.Background(), gateTestAuthority(ModeAuto), ToolCall{ID: "fixture-call", Input: input})
			if err != nil {
				t.Fatal(err)
			}
			expected := row.Outcomes[0]
			if outcome.Output != row.Output || outcome.Failed != expected.Failed || outcome.Denied != expected.Denied || observed.Output != row.Output {
				t.Fatal("Source binding outcome differs", outcome, row.Output)
			}
			if !reflect.DeepEqual(trace, row.Trace) {
				t.Fatal("Source hook/effect order differs", trace, row.Trace)
			}
			if _, fault := input.BashBindingFailure(); fault && row.Mode != "rewrite" && executor.calls != 0 {
				t.Fatal("invalid binding reached an executor")
			}
			matched++
		})
	}
	if matched != 51 {
		t.Fatal("supported Source consumer cases changed", matched)
	}
}

func TestBashBindingStoredEventRetainsFailedArguments(t *testing.T) {
	for _, raw := range []string{`{}`, `{"command":"true","z_extra":1,"a_extra":2}`, `null`, `[1,true]`, `"hello"`, `17`, `true`, `13.75`} {
		data := []byte(`{"seq":1,"ts":10,"session":"s","transcript_epoch":2,"type":"tool_use","name":"bash","id":"u","span_id":"","parent_span_id":"","action_id":"","display":{"verb":"","object":""},"input":` + raw + `}`)
		record := assertStoredEventRoundTrip(t, data)
		failure, ok := record.Event.toolUse.Input.BashBindingFailure()
		if !ok {
			t.Fatal("stored event lost binding failure")
		}
		if failure.Kind() == protocol.BashUnexpectedParameter && failure.Parameter() != "z_extra" {
			t.Fatal("stored event changed parameter order")
		}
	}
}
func TestBashBindingPermissionDenialPrecedesFailure(t *testing.T) {
	input, err := protocol.DecodeModelToolInput(protocol.ToolBash, []byte(`{"command":"true","extra":1}`))
	if err != nil {
		t.Fatal(err)
	}
	trace := []string{}
	executor := &bindingExecutorProbe{trace: &trace}
	rule, err := NewPermissionRule("binding-deny", "policy denied", RuleDeny, func(ToolAuthority, ToolCall, ToolRisk, bool) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	policy, err := NewPermissionPolicy([]PermissionRule{rule}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	gate, err := NewToolGate(gateTestCatalog(t, executor), policy, GateHooks{})
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := gate.Dispatch(context.Background(), gateTestAuthority(ModeAuto), ToolCall{ID: "denied", Input: input})
	if err != nil || !outcome.Denied || executor.calls != 0 {
		t.Fatal("binding bypassed permission", outcome, err)
	}
}
