package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/secrets"
)

type permissionTrace struct {
	Type, Tool, Decision, Rule string
}

type permissionObservation struct {
	Type, Decision, Rule, Tool, Reason, Agent, Session string
	Depth                                              int
	MessageID                                          string `json:"message_id"`
}

type permissionResult struct {
	ID, Name, Output string
	Error, Denied    bool
}

type permissionHandled struct{ Tool, Command string }

type permissionCapture struct {
	mu      sync.Mutex
	trace   []permissionTrace
	records []SessionEventRecord
	handled []permissionHandled
}

func (capture *permissionCapture) add(row permissionTrace) {
	capture.mu.Lock()
	defer capture.mu.Unlock()
	capture.trace = append(capture.trace, row)
}

func (capture *permissionCapture) OnEvent(_ context.Context, record SessionEventRecord) error {
	capture.mu.Lock()
	defer capture.mu.Unlock()
	capture.records = append(capture.records, record)
	switch record.Event.Kind() {
	case EventToolUse:
		capture.trace = append(capture.trace, permissionTrace{Type: "tool_use", Tool: string(record.Event.toolUse.Name)})
	case EventToolResult:
		capture.trace = append(capture.trace, permissionTrace{Type: "tool_result", Tool: string(record.Event.toolResult.Name)})
	case EventPermission:
		event, _ := record.Event.Permission()
		capture.trace = append(capture.trace, permissionTrace{Type: "permission", Tool: string(event.Tool), Decision: string(event.Decision), Rule: event.Rule})
	}
	return nil
}

type permissionCommand struct {
	Command string `json:"command"`
}

func (permissionCommand) Validate() error { return nil }

type permissionCommandCodec struct{}

func (permissionCommandCodec) DecodeExtensionInput(_ context.Context, input protocol.ToolInput) (permissionCommand, error) {
	data, err := input.MarshalJSON()
	if err != nil {
		return permissionCommand{}, err
	}
	var value permissionCommand
	err = json.Unmarshal(data, &value)
	return value, err
}

type permissionSafeHandler struct {
	capture *permissionCapture
	name    protocol.ToolName
}

func (handler permissionSafeHandler) ExecuteExtensionTool(_ context.Context, _ ToolAuthority, input permissionCommand) (string, error) {
	handler.capture.mu.Lock()
	defer handler.capture.mu.Unlock()
	handler.capture.trace = append(handler.capture.trace, permissionTrace{Type: "handler", Tool: string(handler.name)})
	handler.capture.handled = append(handler.capture.handled, permissionHandled{string(handler.name), input.Command})
	return "handled:" + input.Command, nil
}
func (handler permissionSafeHandler) ExecuteTool(ctx context.Context, authority ToolAuthority, input protocol.ToolInput) (string, error) {
	value, err := (permissionCommandCodec{}).DecodeExtensionInput(ctx, input)
	if err != nil {
		return "", err
	}
	return handler.ExecuteExtensionTool(ctx, authority, value)
}

func TestManagedPermissionEventsMatchActualSource(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-permission-events.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Mask  struct{ Value, Replacement string }
		Cases []struct {
			Name             string
			Mode             PermissionMode
			ToolName         protocol.ToolName `json:"tool_name"`
			Risk             *ToolRisk
			Command          string
			Answers          []bool
			ApprovalFault    string `json:"approval_fault"`
			Masked           bool
			OrderedTrace     []permissionTrace       `json:"ordered_trace"`
			PermissionEvents []permissionObservation `json:"permission_events"`
			HandlerCalls     []permissionHandled     `json:"handler_calls"`
			ToolResults      []permissionResult      `json:"tool_results"`
			Output           *string
			Error            *struct{ Type, Text string }
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil || len(fixture.Cases) != 14 {
		t.Fatal(err, len(fixture.Cases))
	}
	for _, row := range fixture.Cases {
		t.Run(row.Name, func(t *testing.T) {
			capture := &permissionCapture{trace: []permissionTrace{}, handled: []permissionHandled{}}
			handler := permissionSafeHandler{capture, row.ToolName}
			risk := RiskUnclassified
			if row.Risk != nil {
				risk = *row.Risk
			}
			traits := ToolTraits{Risk: risk, Readonly: risk == RiskRead}
			var definition ToolDefinition
			var err error
			if row.ToolName == protocol.ToolBash {
				definition, err = NewToolDefinition(row.ToolName, traits, handler)
			} else {
				schema, e := protocol.ExtensionToolSchema(row.ToolName, "safe fixture", []byte(`{"type":"object","properties":{"command":{"type":"string"}},"required":["command"]}`))
				if e != nil {
					t.Fatal(e)
				}
				definition, err = NewTypedExtensionTool(schema, traits, permissionCommandCodec{}, handler)
			}
			if err != nil {
				t.Fatal(err)
			}
			catalog, err := NewToolCatalog(definition)
			if err != nil {
				t.Fatal(err)
			}
			encoded, _ := json.Marshal(permissionCommand{row.Command})
			input, err := protocol.DecodeModelToolInput(row.ToolName, encoded)
			if err != nil {
				t.Fatal(err)
			}
			calls, answers := 0, 0
			approve := approverFunc(func(_ context.Context, request ApprovalRequest) (bool, error) {
				calls++
				capture.add(permissionTrace{Type: "approval_callback", Tool: string(request.Call.Name()), Rule: request.Rule})
				if calls == 2 {
					switch row.ApprovalFault {
					case "error":
						return false, errors.New("fixture approval failed")
					case "cancel":
						return false, context.Canceled
					}
				}
				if answers >= len(row.Answers) {
					return false, errors.New("unexpected approval callback")
				}
				answer := row.Answers[answers]
				answers++
				return answer, nil
			})
			registry := secrets.New(secrets.Config{MaskWith: &fixture.Mask.Replacement})
			if row.Masked {
				registry.RegisterValue("FIXTURE", fixture.Mask.Value)
			}
			config := RuntimeConfig{ID: "session", Owner: "fixture-owner", Provider: resourceProvider{[]protocol.Block{protocol.NewToolUse("permission-tool", input)}}, Tools: catalog, Bash: echoExecutor{}, Workspace: t.TempDir(), Mode: row.Mode, MaxRounds: 4, Model: "fixture-model", Recovery: DirectRecovery{}, Approver: approve, Secrets: registry, EventSink: capture, Label: "main"}
			session, err := NewManagedSession(config)
			if err != nil {
				t.Fatal(err)
			}
			run, err := ExplicitHumanRunContext(HumanRunConfig{})
			if err != nil {
				t.Fatal(err)
			}
			output, err := session.RunWithContext(context.Background(), "observe permission", run)
			if row.Error == nil {
				if err != nil || row.Output == nil || output != *row.Output {
					t.Fatalf("run: %q/%v, Source %v", output, err, row.Output)
				}
			} else if row.Error.Type != "CancelledError" || !errors.Is(err, context.Canceled) {
				t.Fatalf("cancel: %v, Source %+v", err, row.Error)
			}
			if answers != len(row.Answers) {
				t.Fatal("approval answer inventory", answers, row.Answers)
			}
			observed, results := []permissionObservation{}, []permissionResult{}
			var last EventSequence
			for _, record := range capture.records {
				if record.Sequence <= last {
					t.Fatal("event sequence regressed")
				}
				last = record.Sequence
				wire, err := json.Marshal(record)
				if err != nil {
					t.Fatal(err)
				}
				switch record.Event.Kind() {
				case EventPermission:
					if record.Scope.RunContext.MessageID() != run.MessageID() || record.SessionID != "session" {
						t.Fatal("permission identity drift")
					}
					var event permissionObservation
					if err := json.Unmarshal(wire, &event); err != nil {
						t.Fatal(err)
					}
					event.MessageID = "turn"
					observed = append(observed, event)
					restored := assertStoredEventRoundTrip(t, wire)
					original, ok := record.Event.Permission()
					got, restoredOK := restored.Event.Permission()
					if !ok || !restoredOK || got != original {
						t.Fatal("stored permission changed")
					}
					got.Reason = "caller rewrite"
					unchanged, _ := restored.Event.Permission()
					if unchanged != original {
						t.Fatal("permission accessor aliases internal value")
					}
					if row.Masked && strings.Contains(string(wire), fixture.Mask.Value) {
						t.Fatal("permission leaked a secret")
					}
				case EventToolResult:
					var result permissionResult
					if err := json.Unmarshal(wire, &result); err != nil {
						t.Fatal(err)
					}
					results = append(results, result)
				}
			}
			if !reflect.DeepEqual(observed, row.PermissionEvents) {
				t.Fatalf("permission events: got %+v, Source %+v", observed, row.PermissionEvents)
			}
			if !reflect.DeepEqual(capture.trace, row.OrderedTrace) {
				t.Fatalf("decision timing: got %+v, Source %+v", capture.trace, row.OrderedTrace)
			}
			if !reflect.DeepEqual(capture.handled, row.HandlerCalls) {
				t.Fatalf("effects: got %+v, Source %+v", capture.handled, row.HandlerCalls)
			}
			if !reflect.DeepEqual(results, row.ToolResults) {
				t.Fatalf("tool results: got %+v, Source %+v", results, row.ToolResults)
			}
		})
	}
}

func TestStoredPermissionRejectsUnknownDecisions(t *testing.T) {
	for _, decision := range []string{"allow", "deny", "", "approve", "grant"} {
		encoded := `{"seq":1,"ts":1,"session":"s","transcript_epoch":1,"type":"permission","decision":"` + decision + `","rule":"r","tool":"bash","reason":"r"}`
		record, err := DecodeStoredEvent([]byte(encoded))
		if decision == "allow" || decision == "deny" {
			if err != nil {
				t.Fatal("valid permission decision refused", decision, err)
			}
			event, ok := record.Event.Permission()
			if !ok || string(event.Decision) != decision {
				t.Fatal("valid permission decision changed")
			}
		} else if err == nil || !strings.Contains(err.Error(), "invalid stored permission decision") {
			t.Fatal("unsupported permission decision not rejected at its boundary", decision, err)
		}
	}
}
