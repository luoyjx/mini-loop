package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"testing"

	"github.com/luoyjx/mini-loop/go/background"
	"github.com/luoyjx/mini-loop/go/protocol"
)

func TestBackgroundModelArgumentsMatchActualSourceConsumers(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-provider-background-input.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name, Mode, Output string
			Input              string `json:"input_json"`
			FinalInput         string `json:"final_input_json"`
			Counter            int    `json:"background_counter"`
			Initialized        bool   `json:"background_initialized"`
			Live               int    `json:"background_live"`
			Trace              []string
			Events             []struct{ Display ToolDisplay }
			Outcomes           []struct{ Failed, Denied bool }
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 18 {
		t.Fatal("Source non-text command inventory changed")
	}
	for i, row := range fixture.Cases {
		t.Run(row.Name+"/"+row.Mode+"/"+strconv.Itoa(i), func(t *testing.T) {
			input, err := protocol.DecodeModelToolInput(protocol.ToolBackgroundRun, []byte(row.Input))
			if err != nil {
				t.Fatal(err)
			}
			if label := ToolLabel(input); label != row.Events[0].Display {
				t.Fatal("Source display changed", label, row.Events[0].Display)
			}
			s := backgroundRuntime(t, t.TempDir(), &recordingProvider{}, true)
			trace := []string{}
			var observed ToolOutcome
			var final protocol.ToolInput
			gate, err := NewToolGate(s.gate.catalog, s.gate.policy, GateHooks{
				Before: []BeforeHook{beforeHookFunc(func(context.Context, ToolAuthority, ToolCall) (BeforeDecision, error) {
					trace = append(trace, "before")
					if row.Mode == "rewrite" {
						return RewriteToolCall(protocol.BackgroundRunToolInput(protocol.BackgroundRunInput{Command: "printf background-input-rewritten"})), nil
					}
					if row.Mode == "deny" {
						return DenyToolCall("denied by source probe"), nil
					}
					return KeepToolCall(), nil
				})},
				Guards: []GuardHook{guardHookFunc(func(context.Context, ToolAuthority, ToolCall) (string, bool, error) {
					trace = append(trace, "guard")
					return "denied by source guard", row.Mode == "guard-deny", nil
				})},
				After: []AfterHook{afterHookFunc(func(_ context.Context, _ ToolAuthority, _ ToolCall, output string) (string, error) {
					trace = append(trace, "after")
					return output, nil
				})},
				Observers: []ResultObserver{observerFunc(func(_ context.Context, _ ToolAuthority, call ToolCall, outcome ToolOutcome) error {
					trace = append(trace, "result")
					observed = outcome
					final = call.Input
					return nil
				})},
			})
			if err != nil {
				t.Fatal(err)
			}
			s.gate = gate
			out := backgroundDispatch(t, s, input)
			if out.Output != row.Output || out.Failed != row.Outcomes[0].Failed || out.Denied != row.Outcomes[0].Denied || observed.Output != row.Output {
				t.Fatal("Source outcome changed", out, row.Output)
			}
			if !reflect.DeepEqual(trace, row.Trace) {
				t.Fatal("Source hook order changed", trace, row.Trace)
			}
			if s.backgroundInitialized() != row.Initialized || s.backgroundLive() != row.Live {
				t.Fatal("Source background initialization/effects changed")
			}
			if row.Initialized {
				manager, err := s.background.get(context.Background(), false)
				if err != nil {
					t.Fatal(err)
				}
				next, err := manager.Run(context.Background(), background.Request{Command: "printf next"})
				if err != nil || string(next.ID) != fmt.Sprintf("bg_%04d", row.Counter+1) {
					t.Fatal("Source task allocation changed", next, err)
				}
			}

			wire, err := json.Marshal(final)
			if err != nil {
				t.Fatal(err)
			}
			var expected protocol.ToolInput
			expected, err = protocol.DecodeModelToolInput(protocol.ToolBackgroundRun, []byte(row.FinalInput))
			if err != nil {
				t.Fatal(err)
			}
			expectedWire, err := json.Marshal(expected)
			if err != nil || string(wire) != string(expectedWire) {
				t.Fatal("Source rewrite/final input changed", string(wire), err)
			}
			if row.Mode != "rewrite" {
				if _, ok := out.CommandResult(); ok {
					t.Fatal("non-text command fabricated process metadata")
				}
			}
		})
	}
}
