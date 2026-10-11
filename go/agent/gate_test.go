package agent

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/luoyjx/mini-loop/go/protocol"
)

type gateHandler struct {
	log    *[]string
	output string
	err    error
	calls  int
}

func (handler *gateHandler) ExecuteTool(_ context.Context, _ ToolAuthority, input protocol.ToolInput) (string, error) {
	handler.calls++
	if handler.log != nil {
		*handler.log = append(*handler.log, "execute")
	}
	if _, ok := input.Bash(); !ok {
		return "", errors.New("wrong input variant")
	}
	return handler.output, handler.err
}

type beforeHookFunc func(context.Context, ToolAuthority, ToolCall) (BeforeDecision, error)

func (hook beforeHookFunc) BeforeTool(ctx context.Context, authority ToolAuthority, call ToolCall) (BeforeDecision, error) {
	return hook(ctx, authority, call)
}

type guardHookFunc func(context.Context, ToolAuthority, ToolCall) (string, bool, error)

func (hook guardHookFunc) GuardTool(ctx context.Context, authority ToolAuthority, call ToolCall) (string, bool, error) {
	return hook(ctx, authority, call)
}

type afterHookFunc func(context.Context, ToolAuthority, ToolCall, string) (string, error)

func (hook afterHookFunc) AfterTool(ctx context.Context, authority ToolAuthority, call ToolCall, output string) (string, error) {
	return hook(ctx, authority, call, output)
}

type observerFunc func(context.Context, ToolAuthority, ToolCall, ToolOutcome) error

func (observer observerFunc) OnResult(ctx context.Context, authority ToolAuthority, call ToolCall, outcome ToolOutcome) error {
	return observer(ctx, authority, call, outcome)
}

type approverFunc func(context.Context, ApprovalRequest) (bool, error)

func (approver approverFunc) Approve(ctx context.Context, request ApprovalRequest) (bool, error) {
	return approver(ctx, request)
}

func gateTestAuthority(mode PermissionMode) ToolAuthority {
	return ToolAuthority{SessionID: "s", OwnerID: "owner", Mode: mode}
}

func gateTestCall(command string) ToolCall {
	return ToolCall{ID: "u1", Input: protocol.BashToolInput(protocol.BashInput{Command: command})}
}

func gateTestCatalog(t *testing.T, handler ToolHandler) *ToolCatalog {
	t.Helper()
	definition, err := NewToolDefinition(protocol.ToolBash, ToolTraits{Risk: RiskExec, Capabilities: []Capability{CapabilityProcessExec}}, handler)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := NewToolCatalog(definition)
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

func TestGateOrdersRewriteGuardPermissionExecuteAfterObserver(t *testing.T) {
	var log []string
	handler := &gateHandler{log: &log, output: "raw"}
	rule, err := NewPermissionRule("review", "Review command", RuleAsk,
		func(_ ToolAuthority, call ToolCall, _ ToolRisk, _ bool) bool {
			log = append(log, "permission")
			input, _ := call.Input.Bash()
			if input.Command != "echo rewritten" {
				t.Fatalf("permission saw %q", input.Command)
			}
			return true
		})
	if err != nil {
		t.Fatal(err)
	}
	policy, err := NewPermissionPolicy([]PermissionRule{rule}, approverFunc(func(_ context.Context, request ApprovalRequest) (bool, error) {
		log = append(log, "approval")
		input, _ := request.Call.Input.Bash()
		return input.Command == "echo rewritten", nil
	}), nil)
	if err != nil {
		t.Fatal(err)
	}
	hooks := GateHooks{
		Before: []BeforeHook{beforeHookFunc(func(_ context.Context, _ ToolAuthority, _ ToolCall) (BeforeDecision, error) {
			log = append(log, "before")
			return RewriteToolCall(protocol.BashToolInput(protocol.BashInput{Command: "echo rewritten"})), nil
		})},
		Guards: []GuardHook{guardHookFunc(func(_ context.Context, _ ToolAuthority, call ToolCall) (string, bool, error) {
			log = append(log, "guard")
			input, _ := call.Input.Bash()
			if input.Command != "echo rewritten" {
				t.Fatalf("guard saw %q", input.Command)
			}
			return "", false, nil
		})},
		After: []AfterHook{afterHookFunc(func(_ context.Context, _ ToolAuthority, _ ToolCall, output string) (string, error) {
			log = append(log, "after")
			return output + "-after", nil
		})},
		Observers: []ResultObserver{observerFunc(func(_ context.Context, _ ToolAuthority, call ToolCall, outcome ToolOutcome) error {
			log = append(log, "observer")
			input, _ := call.Input.Bash()
			if input.Command != "echo rewritten" || outcome.Output != "raw-after" || outcome.IsError() {
				t.Fatalf("observer saw wrong final outcome: %+v", outcome)
			}
			return nil
		})},
	}
	gate, err := NewToolGate(gateTestCatalog(t, handler), policy, hooks)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := gate.Dispatch(context.Background(), gateTestAuthority(ModeInteractive), gateTestCall("echo original"))
	if err != nil || outcome.Output != "raw-after" || handler.calls != 1 {
		t.Fatalf("dispatch outcome=%+v error=%v calls=%d", outcome, err, handler.calls)
	}
	want := []string{"before", "guard", "permission", "approval", "execute", "after", "observer"}
	if !reflect.DeepEqual(log, want) {
		t.Fatalf("phase order=%v want=%v", log, want)
	}
	events := outcome.PermissionEvents()
	if len(events) != 1 || events[0].Decision != PermissionAllow || events[0].Rule != "review" {
		t.Fatalf("approval audit=%+v", events)
	}
}

func TestGuardDenialCannotBeLaunderedByPostHook(t *testing.T) {
	var log []string
	handler := &gateHandler{log: &log, output: "ran"}
	hooks := GateHooks{
		Before: []BeforeHook{beforeHookFunc(func(_ context.Context, _ ToolAuthority, _ ToolCall) (BeforeDecision, error) {
			log = append(log, "before")
			return RewriteToolCall(protocol.BashToolInput(protocol.BashInput{Command: "rm -rf /"})), nil
		})},
		Guards: []GuardHook{
			guardHookFunc(func(_ context.Context, _ ToolAuthority, call ToolCall) (string, bool, error) {
				log = append(log, "guard-one")
				input, _ := call.Input.Bash()
				return "Denied: destructive", input.Command == "rm -rf /", nil
			}),
			guardHookFunc(func(_ context.Context, _ ToolAuthority, _ ToolCall) (string, bool, error) {
				log = append(log, "guard-two")
				return "", false, nil
			}),
		},
		After: []AfterHook{afterHookFunc(func(_ context.Context, _ ToolAuthority, _ ToolCall, _ string) (string, error) {
			log = append(log, "after")
			return "laundered", nil
		})},
		Observers: []ResultObserver{observerFunc(func(_ context.Context, _ ToolAuthority, _ ToolCall, outcome ToolOutcome) error {
			log = append(log, "observer")
			if !outcome.Denied || outcome.Output != "Denied: destructive" {
				t.Fatalf("observer saw %+v", outcome)
			}
			return nil
		})},
	}
	gate, err := NewToolGate(gateTestCatalog(t, handler), DefaultPermissionPolicy(nil), hooks)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := gate.Dispatch(context.Background(), gateTestAuthority(ModeAuto), gateTestCall("echo safe"))
	if err != nil || !outcome.Denied || handler.calls != 0 {
		t.Fatalf("outcome=%+v err=%v", outcome, err)
	}
	want := []string{"before", "guard-one", "observer"}
	if !reflect.DeepEqual(log, want) {
		t.Fatalf("phase order=%v want=%v", log, want)
	}
}

func TestObserverFailureIsContained(t *testing.T) {
	handler := &gateHandler{output: "done"}
	second := false
	hooks := GateHooks{Observers: []ResultObserver{
		observerFunc(func(context.Context, ToolAuthority, ToolCall, ToolOutcome) error { return errors.New("observer broke") }),
		observerFunc(func(context.Context, ToolAuthority, ToolCall, ToolOutcome) error { panic("observer panic") }),
		observerFunc(func(_ context.Context, _ ToolAuthority, _ ToolCall, outcome ToolOutcome) error {
			second = true
			if outcome.Output != "done" {
				t.Fatalf("wrong outcome %+v", outcome)
			}
			return nil
		}),
	}}
	gate, err := NewToolGate(gateTestCatalog(t, handler), DefaultPermissionPolicy(nil), hooks)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := gate.Dispatch(context.Background(), gateTestAuthority(ModeInteractive), gateTestCall("echo hi"))
	if err != nil || outcome.Output != "done" || !second || len(gate.Problems()) != 2 {
		t.Fatalf("observer failure changed dispatch: %+v, %v", outcome, err)
	}
}
