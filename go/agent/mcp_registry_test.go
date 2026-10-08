package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/mcp"
	"github.com/luoyjx/mini-loop/go/protocol"
)

type mcpFixtureTool struct {
	Name        string          `json:"name"`
	Description jsonvalue.Value `json:"description"`
	InputSchema jsonvalue.Value `json:"input_schema"`
	Annotations jsonvalue.Value `json:"annotations"`
}
type mcpFixtureStep struct {
	Server string           `json:"server"`
	Tools  []mcpFixtureTool `json:"tools"`
}
type mcpFixtureDefinition struct {
	Name         protocol.ToolName `json:"name"`
	Description  string            `json:"description"`
	Schema       jsonvalue.Value   `json:"schema"`
	Risk         ToolRisk          `json:"risk"`
	Readonly     bool              `json:"readonly"`
	ParallelSafe bool              `json:"parallel_safe"`
}
type mcpFixturePublication struct {
	Added       []protocol.ToolName          `json:"added"`
	Definitions []mcpFixtureDefinition       `json:"definitions"`
	Problems    []string                     `json:"problems"`
	Owners      map[protocol.ToolName]string `json:"owners"`
}
type mcpRegistrationFixture struct {
	Rows []struct {
		Steps        []mcpFixtureStep        `json:"steps"`
		Publications []mcpFixturePublication `json:"publications"`
	} `json:"rows"`
	Outputs        []string `json:"outputs"`
	Normalizations []struct {
		Input  string `json:"input"`
		Output string `json:"output"`
	} `json:"normalizations"`
}

func readMCPRegistrationFixture(t *testing.T) mcpRegistrationFixture {
	t.Helper()
	data, err := os.ReadFile("../testdata/python-mcp-registration.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture mcpRegistrationFixture
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	return fixture
}

type mcpPeer struct {
	server   string
	tools    []mcp.ToolDescription
	result   string
	calls    atomic.Int64
	callHook func(context.Context, jsonvalue.Value)
}

func (peer *mcpPeer) Name() string { return peer.server }
func (peer *mcpPeer) ListTools(context.Context) ([]mcp.ToolDescription, error) {
	return append([]mcp.ToolDescription(nil), peer.tools...), nil
}
func (peer *mcpPeer) CallTool(ctx context.Context, name string, args jsonvalue.Value) (string, error) {
	peer.calls.Add(1)
	if peer.callHook != nil {
		peer.callHook(ctx, args)
	}
	if name == "slow" {
		<-ctx.Done()
		return "", ctx.Err()
	}
	if peer.result != "" {
		return peer.result, nil
	}
	return protocol.PythonJSON(struct {
		Arguments jsonvalue.Value `json:"arguments"`
		Original  string          `json:"original"`
	}{args.Sorted(), name}, false, false)
}
func fixturePeer(step mcpFixtureStep) *mcpPeer {
	peer := &mcpPeer{server: step.Server}
	for _, tool := range step.Tools {
		peer.tools = append(peer.tools, mcp.ToolDescription{Name: tool.Name, Description: tool.Description, InputSchema: tool.InputSchema, Annotations: tool.Annotations})
	}
	return peer
}
func simpleMCPPeer(name, result string) *mcpPeer {
	schema, _ := jsonvalue.Decode(`{"type":"object","properties":{}}`)
	hint, _ := jsonvalue.Decode(`{"readOnlyHint":true}`)
	return &mcpPeer{server: name, result: result, tools: []mcp.ToolDescription{{Name: "echo", Description: jsonvalue.TextValue("d"), InputSchema: schema, Annotations: hint}}}
}

func TestMCPRegistrationMatchesPython(t *testing.T) {
	fixture := readMCPRegistrationFixture(t)
	for _, item := range fixture.Normalizations {
		if actual := protocol.NormalizeMCPName(item.Input); actual != item.Output {
			t.Fatalf("normalization %q: %q", item.Input, actual)
		}
	}
	for index, row := range fixture.Rows {
		t.Run(fmt.Sprint(index), func(t *testing.T) {
			var registry MCPRegistry
			for i, step := range row.Steps {
				publication, err := registry.Register(context.Background(), fixturePeer(step), 10*time.Millisecond)
				if err != nil {
					t.Fatal(err)
				}
				expected := row.Publications[i]
				if !reflect.DeepEqual(publication.Added, expected.Added) {
					t.Fatalf("added: %v != %v", publication.Added, expected.Added)
				}
				if !reflect.DeepEqual(publication.Catalog.Names(), namesOfMCPFixture(expected.Definitions)) {
					t.Fatal("catalog order changed")
				}
				for _, item := range expected.Definitions {
					definition, _ := publication.Catalog.Lookup(item.Name)
					if definition.Risk() != item.Risk || definition.Readonly() != item.Readonly || definition.ParallelSafe() != item.ParallelSafe || definition.schema.Description != item.Description {
						t.Fatalf("metadata differs for %s", item.Name)
					}
					actual, err := definition.schema.InputSchema.MarshalJSON()
					if err != nil {
						t.Fatal(err)
					}
					value, err := jsonvalue.Decode(string(actual))
					if err != nil {
						t.Fatal(err)
					}
					a, _ := value.Sorted().MarshalJSON()
					b, _ := item.Schema.Sorted().MarshalJSON()
					if string(a) != string(b) {
						t.Fatalf("schema fields lost: %s != %s", a, b)
					}
				}
				var messages []string
				for _, entry := range registry.Problems().Entries {
					messages = append(messages, entry.Text)
				}
				if len(messages) != len(expected.Problems) || len(messages) > 0 && !reflect.DeepEqual(messages, expected.Problems) {
					t.Fatalf("problems: %v != %v", messages, expected.Problems)
				}
				if !reflect.DeepEqual(registry.owners, expected.Owners) {
					t.Fatal("collision ownership changed")
				}
			}
		})
	}
}
func namesOfMCPFixture(items []mcpFixtureDefinition) []protocol.ToolName {
	out := make([]protocol.ToolName, 0, len(items))
	for _, item := range items {
		out = append(out, item.Name)
	}
	return out
}

func TestMCPSnapshotKeepsOldHandlerAndExternalPermission(t *testing.T) {
	var registry MCPRegistry
	old := simpleMCPPeer("same", "old")
	fresh := simpleMCPPeer("same", "new")
	first, err := registry.Register(context.Background(), old, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	second, err := registry.Register(context.Background(), fresh, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	name := first.Added[0]
	args, _ := jsonvalue.Decode(`{"value":"raw"}`)
	input, err := protocol.MCPToolInput(name, args)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		catalog *ToolCatalog
		result  string
	}{{first.Catalog, "old"}, {second.Catalog, "new"}} {
		gate, err := NewToolGate(item.catalog, DefaultPermissionPolicy(nil), GateHooks{})
		if err != nil {
			t.Fatal(err)
		}
		for _, mode := range []PermissionMode{ModeReadonly, ModeInteractive} {
			denied, err := gate.Dispatch(context.Background(), gateTestAuthority(mode), ToolCall{ID: "denied", Input: input})
			if err != nil || !denied.Denied {
				t.Fatalf("external readonly hint bypass: %v %+v", err, denied)
			}
		}
		outcome, err := gate.Dispatch(context.Background(), gateTestAuthority(ModeAuto), ToolCall{ID: "allowed", Input: input})
		if err != nil || outcome.Output != item.result {
			t.Fatalf("snapshot execution: %+v %v", outcome, err)
		}
	}
	if old.calls.Load() != 1 || fresh.calls.Load() != 1 {
		t.Fatal("denied or replaced handler executed")
	}
}

func TestMCPGateHooksRewriteAndTimeout(t *testing.T) {
	fixture := readMCPRegistrationFixture(t)
	schema, _ := jsonvalue.Decode(`{"type":"object","properties":{}}`)
	peer := &mcpPeer{server: "calls", tools: []mcp.ToolDescription{{Name: "original name", Description: jsonvalue.TextValue("d"), InputSchema: schema}, {Name: "slow", Description: jsonvalue.TextValue("d"), InputSchema: schema}}}
	var registry MCPRegistry
	published, err := registry.Register(context.Background(), peer, 10*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	args, _ := jsonvalue.Decode(`{"value":"中文","nested":{"x":[1,true,null]}}`)
	for i, name := range published.Added {
		input, err := protocol.MCPToolInput(name, args)
		if err != nil {
			t.Fatal(err)
		}
		gate, _ := NewToolGate(published.Catalog, DefaultPermissionPolicy(nil), GateHooks{})
		outcome, err := gate.Dispatch(context.Background(), gateTestAuthority(ModeAuto), ToolCall{ID: "source", Input: input})
		if err != nil || outcome.Output != fixture.Outputs[i] || outcome.Failed {
			t.Fatalf("source handler: %+v %v want %s", outcome, err, fixture.Outputs[i])
		}
	}
	var order []string
	name := published.Added[0]
	rewritten, _ := jsonvalue.Decode(`{"changed":true}`)
	input, _ := protocol.MCPToolInput(name, args)
	replacement, _ := protocol.MCPToolInput(name, rewritten)
	peer.callHook = func(_ context.Context, actual jsonvalue.Value) {
		order = append(order, "execute")
		wire, _ := actual.MarshalJSON()
		if string(wire) != `{"changed":true}` {
			t.Error("execution lost rewrite")
		}
	}
	hooks := GateHooks{
		Before: []BeforeHook{beforeHookFunc(func(context.Context, ToolAuthority, ToolCall) (BeforeDecision, error) {
			order = append(order, "before")
			return RewriteToolCall(replacement), nil
		})},
		Guards: []GuardHook{guardHookFunc(func(_ context.Context, _ ToolAuthority, call ToolCall) (string, bool, error) {
			order = append(order, "guard")
			if value, _ := call.Input.CanonicalJSON(); value != `{"changed":true}` {
				t.Error("guard saw old input")
			}
			return "", false, nil
		})},
		After: []AfterHook{afterHookFunc(func(_ context.Context, _ ToolAuthority, _ ToolCall, result string) (string, error) {
			order = append(order, "after")
			return result, nil
		})},
		Observers: []ResultObserver{observerFunc(func(context.Context, ToolAuthority, ToolCall, ToolOutcome) error {
			order = append(order, "observe")
			return nil
		})},
	}
	approver := approverFunc(func(context.Context, ApprovalRequest) (bool, error) {
		order = append(order, "permission")
		return true, nil
	})
	gate, _ := NewToolGate(published.Catalog, DefaultPermissionPolicy(approver), hooks)
	if _, err := gate.Dispatch(context.Background(), gateTestAuthority(ModeInteractive), ToolCall{ID: "rewrite", Input: input}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(order, []string{"before", "guard", "permission", "execute", "after", "observe"}) {
		t.Fatal(order)
	}
}

func TestRuntimeExplicitMCPCatalog(t *testing.T) {
	peer := simpleMCPPeer("runtime", "external result")
	var registry MCPRegistry
	publication, err := registry.Register(context.Background(), peer, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	args, _ := jsonvalue.Decode(`{}`)
	input, _ := protocol.MCPToolInput(publication.Added[0], args)
	config := runtimeConfig(t.TempDir(), resourceProvider{tools: []protocol.Block{protocol.NewToolUse("external", input)}})
	config.Mode = ModeAuto
	config.MCPCatalog = publication.Catalog
	session, err := NewRuntimeSession(config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Run(context.Background(), "call external"); err != nil {
		t.Fatal(err)
	}
	if peer.calls.Load() != 1 {
		t.Fatal("model-request MCP tool not executed")
	}
	config.MCPCatalog = session.gate.catalog
	if _, err := NewRuntimeSession(config); err == nil {
		t.Fatal("non-MCP definitions accepted as MCP catalog")
	}
}

func TestMCPJournalReplayKeepsExternalPermission(t *testing.T) {
	peer := simpleMCPPeer("replay", "once")
	var registry MCPRegistry
	publication, err := registry.Register(context.Background(), peer, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	args, _ := jsonvalue.Decode(`{"nested":{"unicode":"中文","items":[1,true,null]}}`)
	input, err := protocol.MCPToolInput(publication.Added[0], args)
	if err != nil {
		t.Fatal(err)
	}
	journal, err := NewInMemoryActionJournal(4)
	if err != nil {
		t.Fatal(err)
	}
	gate, err := NewJournaledToolGate(publication.Catalog, DefaultPermissionPolicy(nil), GateHooks{}, journal)
	if err != nil {
		t.Fatal(err)
	}
	authority := gateTestAuthority(ModeAuto)
	authority.RunContext = actionContext(t)
	call := ToolCall{ID: "mcp-replay", Input: input}
	first, err := gate.Dispatch(context.Background(), authority, call)
	if err != nil || first.Output != "once" || first.Replayed {
		t.Fatalf("first: %+v %v", first, err)
	}
	second, err := gate.Dispatch(context.Background(), authority, call)
	if err != nil || !second.Replayed || second.Output != "once" || peer.calls.Load() != 1 {
		t.Fatalf("replay: %+v %v", second, err)
	}
	authority.Mode = ModeReadonly
	denied, err := gate.Dispatch(context.Background(), authority, call)
	if err != nil || !denied.Denied || denied.Replayed || peer.calls.Load() != 1 {
		t.Fatalf("replay bypassed current policy: %+v %v", denied, err)
	}
}
