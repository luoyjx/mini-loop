package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/luoyjx/mini-loop/go/protocol"
)

func TestUnknownModelToolsMatchActualSourceGate(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-provider-unknown-tool.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name, Mode, Output string
			ToolName           protocol.ToolName `json:"tool_name"`
			Permission         PermissionMode    `json:"permission_mode"`
			Input              string            `json:"input_json"`
			FinalInput         string            `json:"final_input_json"`
			Trace              []string
			Events             []struct{ Display ToolDisplay }
			Outcomes           []struct{ Failed, Denied bool }
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 32 {
		t.Fatal("Source missing-tool inventory changed")
	}
	for _, row := range fixture.Cases {
		t.Run(row.Name, func(t *testing.T) {
			input, err := protocol.DecodeModelToolInput(row.ToolName, []byte(row.Input))
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := input.UnknownModelToolObservation(); !ok {
				t.Fatal("unknown model data became executable input")
			}
			if ToolLabel(input) != row.Events[0].Display {
				t.Fatal("Source display changed")
			}
			trace := []string{}
			var final protocol.ToolInput
			var observed ToolOutcome
			handler := &gateHandler{}
			gate, err := NewToolGate(gateTestCatalog(t, handler), DefaultPermissionPolicy(nil), GateHooks{
				Before: []BeforeHook{beforeHookFunc(func(context.Context, ToolAuthority, ToolCall) (BeforeDecision, error) {
					trace = append(trace, "before")
					if row.Mode == "deny" {
						return DenyToolCall("denied by source probe"), nil
					}
					if row.Mode == "rewrite" {
						next, err := protocol.DecodeModelToolInput(row.ToolName, []byte(`{"rewritten":true}`))
						return RewriteToolCall(next), err
					}
					return KeepToolCall(), nil
				})},
				Guards: []GuardHook{guardHookFunc(func(context.Context, ToolAuthority, ToolCall) (string, bool, error) {
					trace = append(trace, "guard")
					return "denied by source guard", row.Mode == "guard-deny", nil
				})},
				After: []AfterHook{afterHookFunc(func(_ context.Context, _ ToolAuthority, _ ToolCall, output string) (string, error) {
					trace = append(trace, "after")
					if row.Mode == "after-replace" {
						return "after:" + output, nil
					}
					return output, nil
				})},
				Observers: []ResultObserver{observerFunc(func(_ context.Context, _ ToolAuthority, call ToolCall, outcome ToolOutcome) error {
					trace = append(trace, "result")
					final = call.Input
					observed = outcome
					return nil
				})},
			})
			if err != nil {
				t.Fatal(err)
			}
			out, err := gate.Dispatch(context.Background(), gateTestAuthority(row.Permission), ToolCall{ID: "unknown-tool-call", Input: input})
			if err != nil {
				t.Fatal(err)
			}
			if out.Output != row.Output || observed.Output != row.Output || out.Failed != row.Outcomes[0].Failed || out.Denied != row.Outcomes[0].Denied {
				t.Fatal("Source outcome changed", out, row.Output)
			}
			if !reflect.DeepEqual(trace, row.Trace) {
				t.Fatal("Source hook order changed", trace, row.Trace)
			}
			if len(out.PermissionEvents()) != 0 || handler.calls != 0 {
				t.Fatal("unknown invocation acquired authority or executed a handler")
			}
			wire, err := json.Marshal(final)
			expected := []byte(row.FinalInput)
			var compact bytes.Buffer
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Compact(&compact, expected); err != nil {
				t.Fatal(err)
			}
			if string(wire) != compact.String() {
				t.Fatal("Source rewrite/observation changed", string(wire), row.FinalInput)
			}
		})
	}
}

func TestUnknownModelObservationCannotEnterCustomHandler(t *testing.T) {
	handler := &gateHandler{}
	schema := protocol.ToolSchema{Name: "missing-tool", Description: "native codec deliberately absent", InputSchema: protocol.InputSchema{Type: protocol.SchemaObject}}
	definition, err := NewToolDefinitionWithSchema(schema, ToolTraits{Risk: RiskRead, Readonly: true}, handler)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := NewToolCatalog(definition)
	if err != nil {
		t.Fatal(err)
	}
	gate, err := NewToolGate(catalog, DefaultPermissionPolicy(nil), GateHooks{})
	if err != nil {
		t.Fatal(err)
	}
	input, err := protocol.DecodeModelToolInput("missing-tool", []byte(`{"command":"printf inert"}`))
	if err != nil {
		t.Fatal(err)
	}
	out, err := gate.Dispatch(context.Background(), gateTestAuthority(ModeReadonly), ToolCall{ID: "unknown", Input: input})
	if err != nil || !out.Failed || out.Output != "Error: tool requires a named native input decoder: missing-tool" || handler.calls != 0 {
		t.Fatal("inert observation reached custom execution", out, err)
	}
}

func TestStoredUnknownModelToolEventRemainsInert(t *testing.T) {
	for _, raw := range []string{`{}`, `null`, `["secret-value"]`, `17`} {
		data := []byte(`{"seq":1,"ts":10,"session":"s","transcript_epoch":1,"type":"tool_use","name":"future","input":` + raw + `}`)
		record, err := DecodeStoredEvent(data)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := record.Event.toolUse.Input.UnknownModelToolObservation(); !ok {
			t.Fatal("stored unknown tool became executable arguments")
		}
		encoded, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		again, err := DecodeStoredEvent(encoded)
		if err != nil {
			t.Fatal(err)
		}
		wire, err := json.Marshal(again.Event.toolUse.Input)
		if err != nil || string(wire) != raw {
			t.Fatal("stored unknown argument root changed", string(wire), err)
		}
	}
}
