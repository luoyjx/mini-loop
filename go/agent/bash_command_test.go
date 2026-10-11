package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"reflect"
	"slices"
	"strconv"
	"testing"

	"github.com/luoyjx/mini-loop/go/protocol"
)

func TestNonTextBashCommandsMatchActualSourceConsumers(t *testing.T) {
	assertBashConsumers(t, "../testdata/python-provider-bash-command.json", 36)
}

func TestNonfiniteBashFieldsMatchActualSourceConsumers(t *testing.T) {
	assertBashConsumers(t, "../testdata/python-provider-nonfinite-bash.json", 36)
}

func assertBashConsumers(t *testing.T, fixturePath string, count int) {
	t.Helper()
	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name, Mode, Output string
			Input              string `json:"input_json"`
			FinalInput         string `json:"final_input_json"`
			Enabled            bool   `json:"background_enabled"`
			Initialized        bool   `json:"background_initialized"`
			Live               int    `json:"background_live"`
			Trace              []string
			Default            []string `json:"default_candidate"`
			Proposed           []string `json:"proposed_candidate"`
			Outcomes           []struct{ Failed, Denied bool }
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != count {
		t.Fatal("Source non-text command inventory changed")
	}
	for i, row := range fixture.Cases {
		t.Run(row.Name+"/"+row.Mode+"/"+strconv.Itoa(i), func(t *testing.T) {
			input, err := protocol.DecodeModelToolInput(protocol.ToolBash, []byte(row.Input))
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(DefaultGrantCandidate(input).Tokens(), row.Default) || !slices.Equal(ProposedGrantCandidate(input).Tokens(), row.Proposed) {
				t.Fatal("Source candidate projection changed")
			}
			s := backgroundRuntime(t, t.TempDir(), &recordingProvider{}, row.Enabled)
			trace := []string{}
			var observed ToolOutcome
			var final protocol.ToolInput
			gate, err := NewToolGate(s.gate.catalog, s.gate.policy, GateHooks{
				Before: []BeforeHook{beforeHookFunc(func(context.Context, ToolAuthority, ToolCall) (BeforeDecision, error) {
					trace = append(trace, "before")
					if row.Mode == "rewrite" {
						return RewriteToolCall(protocol.BashToolInput(protocol.BashInput{Command: "printf command-rewritten"})), nil
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
			observation, err := final.Observation()
			if err != nil {
				t.Fatal(err)
			}
			wire, err := observation.SortedPythonJSON()
			if err != nil {
				t.Fatal(err)
			}
			var expected protocol.ToolInput
			expected, err = protocol.DecodeModelToolInput(protocol.ToolBash, []byte(row.FinalInput))
			if err != nil {
				t.Fatal(err)
			}
			expectedObservation, err := expected.Observation()
			if err != nil {
				t.Fatal(err)
			}
			expectedWire, err := expectedObservation.SortedPythonJSON()
			if err != nil || string(wire) != string(expectedWire) {
				t.Fatal("Source rewrite/final input changed", string(wire), err)
			}
			if out.Failed || out.Denied {
				if _, ok := out.CommandResult(); ok {
					t.Fatal("non-text command fabricated process metadata")
				}
			}
		})
	}
}

func TestNonTextBashCommandsMatchActualSourcePermissionProjection(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-provider-bash-command.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Permissions []struct {
			Name   string
			Mode   PermissionMode
			Input  string `json:"input_json"`
			Output *string
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Permissions) != 20 {
		t.Fatal("Source permission inventory changed")
	}
	for _, row := range fixture.Permissions {
		t.Run(row.Name+"/"+string(row.Mode), func(t *testing.T) {
			input, err := protocol.DecodeModelToolInput(protocol.ToolBash, []byte(row.Input))
			if err != nil {
				t.Fatal(err)
			}
			out, err := DefaultPermissionPolicy(nil).Evaluate(context.Background(), gateTestAuthority(row.Mode), ToolCall{ID: "permission", Input: input}, RiskExec, true)
			if err != nil {
				t.Fatal(err)
			}
			if out.Denied() != (row.Output != nil) || row.Output != nil && out.Message() != *row.Output {
				t.Fatal("Source str(command) permission result changed", out.Message(), row.Output)
			}
		})
	}
}

func TestNonTextBashCommandStoredEventsRetainObservedValues(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-provider-bash-command.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Input string `json:"input_json"`
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, row := range fixture.Cases {
		wire := []byte(`{"seq":1,"ts":10,"session":"s","transcript_epoch":2,"type":"tool_use","name":"bash","id":"u","span_id":"","parent_span_id":"","action_id":"","display":{"verb":"","object":""},"input":` + row.Input + `}`)
		record := assertStoredEventRoundTrip(t, wire)
		input := record.Event.toolUse.Input
		if _, ok := input.BashCommandObservation(); !ok {
			t.Fatal("event observation became executable text")
		}
		encoded, err := json.Marshal(input)
		var expected bytes.Buffer
		if err := json.Compact(&expected, []byte(row.Input)); err != nil {
			t.Fatal(err)
		}
		if err != nil || string(encoded) != expected.String() {
			t.Fatal("event changed command kind/value", string(encoded), err)
		}
	}
}
