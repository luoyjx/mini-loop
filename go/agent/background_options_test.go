package agent

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/background"
	"github.com/luoyjx/mini-loop/go/protocol"
)

func TestBackgroundOptionsMatchSourceAcceptedCallAndSettledTask(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-provider-background-options.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name, Output, Result, Status, Check string
			Input                               string   `json:"input_json"`
			Default                             []string `json:"default_candidate"`
			Proposed                            []string `json:"proposed_candidate"`
			Trace                               []string
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 48 {
		t.Fatal("Source option inventory changed")
	}
	for _, row := range fixture.Cases {
		t.Run(row.Name, func(t *testing.T) {
			input, err := protocol.DecodeModelToolInput(protocol.ToolBackgroundRun, []byte(row.Input))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(DefaultGrantCandidate(input).Tokens(), row.Default) || !reflect.DeepEqual(ProposedGrantCandidate(input).Tokens(), row.Proposed) {
				t.Fatal("Source approval candidate changed")
			}
			readonly := backgroundRuntime(t, t.TempDir(), &recordingProvider{}, true)
			denied, err := readonly.gate.Dispatch(context.Background(), ToolAuthority{SessionID: readonly.id, OwnerID: readonly.owner, Workspace: readonly.executionRoot(), Mode: ModeReadonly}, ToolCall{ID: "readonly-options", Input: input})
			if err != nil || !denied.Denied || readonly.backgroundInitialized() {
				t.Fatal("observation supplied authority", denied, err)
			}
			s := backgroundRuntime(t, t.TempDir(), &recordingProvider{}, true)
			trace := []string{}
			gate, err := NewToolGate(s.gate.catalog, s.gate.policy, GateHooks{
				Before: []BeforeHook{beforeHookFunc(func(context.Context, ToolAuthority, ToolCall) (BeforeDecision, error) {
					trace = append(trace, "before")
					return KeepToolCall(), nil
				})},
				Guards: []GuardHook{guardHookFunc(func(context.Context, ToolAuthority, ToolCall) (string, bool, error) {
					trace = append(trace, "guard")
					return "", false, nil
				})},
				After: []AfterHook{afterHookFunc(func(_ context.Context, _ ToolAuthority, _ ToolCall, text string) (string, error) {
					trace = append(trace, "after")
					return text, nil
				})},
				Observers: []ResultObserver{observerFunc(func(context.Context, ToolAuthority, ToolCall, ToolOutcome) error {
					trace = append(trace, "result")
					return nil
				})},
			})
			if err != nil {
				t.Fatal(err)
			}
			s.gate = gate
			outcome := backgroundDispatch(t, s, input)
			if outcome.Failed || outcome.Denied || outcome.Output != row.Output || !reflect.DeepEqual(trace, row.Trace) {
				t.Fatal("accepted call changed", outcome, trace)
			}
			manager, err := s.background.get(context.Background(), false)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := manager.Wait(ctx, background.ID("bg_0001")); err != nil {
				t.Fatal(err)
			}
			record, ok := manager.Get(background.ID("bg_0001"))
			if !ok || string(record.Status) != row.Status || record.Result == nil || *record.Result != row.Result || manager.Check(record.ID) != row.Check {
				t.Fatal("Source task result changed", record, row.Result)
			}
			notifications := manager.Drain()
			if len(notifications) != 1 || notifications[0].Result != row.Result || string(notifications[0].Status) != row.Status || manager.LiveCount() != 0 {
				t.Fatal("Source settlement changed", notifications)
			}
		})
	}
}
