package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"

	"github.com/luoyjx/mini-loop/go/protocol"
)

type extensionChildHandler struct{ calls *[]ToolAuthority }

func (handler extensionChildHandler) ExecuteExtensionTool(_ context.Context, authority ToolAuthority, input greetingExtensionInput) (string, error) {
	*handler.calls = append(*handler.calls, authority)
	return fmt.Sprintf("%s/%d/%s:Hello, %s!", authority.Scope.Label, authority.Scope.Depth, authority.Mode, input.Name), nil
}

type extensionChildProvider struct{ requests []protocol.ModelRequest }

func (provider *extensionChildProvider) Complete(_ context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
	provider.requests = append(provider.requests, request.Clone())
	if len(provider.requests) > 1 {
		return fakeReply([]protocol.Block{protocol.NewTextBlock("child done")}, protocol.StopEndTurn), nil
	}
	input, err := protocol.DecodeModelToolInput("greet", []byte(`{"name":"World"}`))
	return fakeReply([]protocol.Block{protocol.NewToolUse("toolu_x", input)}, protocol.StopToolUse), err
}

func TestTypedExtensionFreshChildrenMatchSourceCapabilitiesAndScope(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-extension-subagents.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		SchemaJSON string `json:"schema_json"`
		Cases      []struct {
			Name                   string
			Role                   AgentRole
			Capabilities           []Capability
			Risk                   ToolRisk
			ToolsJSON              string `json:"tools_json"`
			Summary                string
			FirstMessages          []protocol.Message `json:"first_messages"`
			ParentHistoryUnchanged bool               `json:"parent_history_unchanged"`
			ParentMarkerUnchanged  bool               `json:"parent_marker_unchanged"`
			Outputs                []struct {
				Output         string
				Denied, Failed bool
			}
			Observations []struct {
				Label               string
				Depth               int
				Mode                PermissionMode
				SameWorkspace       bool `json:"same_workspace"`
				Context             RunContextSnapshot
				ParentMarkerPresent bool `json:"parent_marker_present"`
			}
			Received []struct {
				Label               string
				Depth               int
				Mode                PermissionMode
				SameWorkspace       bool `json:"same_workspace"`
				Context             RunContextSnapshot
				ParentMarkerPresent bool `json:"parent_marker_present"`
			}
		}
	}
	if err = json.Unmarshal(data, &fixture); err != nil || len(fixture.Cases) != 18 {
		t.Fatal(err)
	}
	for _, row := range fixture.Cases {
		t.Run(row.Name, func(t *testing.T) {
			root := t.TempDir()
			trace := []string{}
			calls, before := []ToolAuthority{}, []ToolAuthority{}
			var observed ToolOutcome
			schema, err := protocol.ExtensionToolSchema("greet", "Typed child tool", []byte(fixture.SchemaJSON))
			if err != nil {
				t.Fatal(err)
			}
			definition, err := NewTypedExtensionTool(schema, ToolTraits{Risk: row.Risk, Readonly: row.Risk == RiskRead, Capabilities: row.Capabilities}, greetingExtensionCodec{&trace}, extensionChildHandler{&calls})
			if err != nil {
				t.Fatal(err)
			}
			catalog, err := NewToolCatalog(definition)
			if err != nil {
				t.Fatal(err)
			}
			provider := &extensionChildProvider{}
			config := runtimeConfig(root, provider)
			config.Tools = catalog
			config.Label = "main"
			config.SubagentMaxRounds = 2
			config.Hooks.Before = []BeforeHook{beforeHookFunc(func(_ context.Context, authority ToolAuthority, _ ToolCall) (BeforeDecision, error) {
				before = append(before, authority)
				return KeepToolCall(), nil
			})}
			config.Hooks.Observers = []ResultObserver{observerFunc(func(_ context.Context, _ ToolAuthority, _ ToolCall, outcome ToolOutcome) error {
				observed = outcome
				return nil
			})}
			parent, err := NewRuntimeSession(config)
			if err != nil {
				t.Fatal(err)
			}
			parent.messages = append(parent.messages, protocol.Message{Role: protocol.RoleUser, Content: protocol.PlainContent("private parent history")})
			original := parent.Messages()
			humanID := ActorID("human")
			human, err := ExplicitHumanRunContext(HumanRunConfig{ActorID: &humanID, ApprovedCapabilities: []RunCapability{CapabilityWorkflowLaunch}})
			if err != nil {
				t.Fatal(err)
			}
			output, err := parent.DelegateWithContext(context.Background(), "child", row.Role, human)
			if err != nil || output != row.Summary || len(provider.requests) != 2 {
				t.Fatal(output, provider.requests, err)
			}
			canonical, err := protocol.PythonJSON(provider.requests[0].Tools, false, true)
			if err != nil || canonical != row.ToolsJSON {
				t.Fatal("capability-selected custom schema differs", canonical, row.ToolsJSON, err)
			}
			if !reflect.DeepEqual(provider.requests[0].Messages, row.FirstMessages) {
				t.Fatal("parent history entered child", provider.requests[0].Messages, row.FirstMessages)
			}
			if len(row.Outputs) != 1 || observed.Output != row.Outputs[0].Output || observed.Denied != row.Outputs[0].Denied || observed.Failed != row.Outputs[0].Failed {
				t.Fatal("Source result/denial differs", observed, row.Outputs)
			}
			if len(before) != 1 || len(row.Observations) != 1 {
				t.Fatal(before, row.Observations)
			}
			want := row.Observations[0]
			actual := before[0]
			snapshot := actual.RunContext.Snapshot()
			snapshot.MessageID = "child"
			id := MessageID("parent")
			snapshot.ParentMessageID = &id
			if actual.Scope.Label != want.Label || actual.Scope.Depth != want.Depth || actual.Mode != want.Mode || actual.Workspace != parent.executionRoot() || !want.SameWorkspace || !reflect.DeepEqual(snapshot, want.Context) {
				t.Fatal("child callback scope/context differs", actual, snapshot, want)
			}
			if actual.OwnerID != config.Owner || actual.SessionID == parent.ID() || snapshot.ActorID != nil || len(snapshot.ApprovedCapabilities) != 0 {
				t.Fatal("peer inherited human authority or parent identity", actual, snapshot)
			}
			if len(calls) != len(row.Received) {
				t.Fatal("handler ran for filtered/readonly input", calls, row.Received)
			}
			for _, call := range calls {
				if call.Scope != actual.Scope || call.RunContext.Authority() != AuthorityPeerAgent {
					t.Fatal(call)
				}
			}
			if !reflect.DeepEqual(parent.Messages(), original) || !row.ParentHistoryUnchanged || !row.ParentMarkerUnchanged || want.ParentMarkerPresent {
				t.Fatal("child borrowed mutable parent state")
			}
		})
	}
}

func TestToolExecutionScopeIsInformational(t *testing.T) {
	authority := gateTestAuthority(ModeReadonly)
	authority.Scope = ToolExecutionScope{Label: "human-looking label", Depth: 999}
	handler := &gateHandler{output: "must not execute"}
	definition, err := NewToolDefinition(protocol.ToolBash, ToolTraits{Risk: RiskExec}, handler)
	if err != nil {
		t.Fatal(err)
	}
	catalog, _ := NewToolCatalog(definition)
	gate, _ := NewToolGate(catalog, DefaultPermissionPolicy(nil), GateHooks{})
	out, err := gate.Dispatch(context.Background(), authority, gateTestCall("echo metadata"))
	if err != nil || !out.Denied || handler.calls != 0 {
		t.Fatal("scope metadata granted authority", out, err)
	}
}
