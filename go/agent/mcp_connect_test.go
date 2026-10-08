package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/mcp"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/secrets"
)

type mcpConnectSpec struct {
	Alias    string         `json:"alias"`
	Name     string         `json:"name"`
	Factory  bool           `json:"factory"`
	Withheld []secrets.Name `json:"withheld"`
	NoTools  bool           `json:"no_tools"`
}
type mcpConnectOperation struct {
	Connect   *string           `json:"connect"`
	Call      protocol.ToolName `json:"call"`
	Arguments jsonvalue.Value   `json:"arguments"`
}
type mcpConnectCall struct {
	Alias     string          `json:"alias"`
	Name      string          `json:"name"`
	Arguments jsonvalue.Value `json:"arguments"`
}
type mcpConnectTrace struct {
	Factory []string
	Listed  []string
	Called  []mcpConnectCall
}
type mcpConnectPeer struct {
	Spec  mcpConnectSpec
	Trace *mcpConnectTrace
}

func (peer *mcpConnectPeer) Name() string { return peer.Spec.Name }
func (peer *mcpConnectPeer) Withheld() []secrets.Name {
	return append([]secrets.Name(nil), peer.Spec.Withheld...)
}
func (peer *mcpConnectPeer) ListTools(context.Context) ([]mcp.ToolDescription, error) {
	peer.Trace.Listed = append(peer.Trace.Listed, peer.Spec.Alias)
	if peer.Spec.NoTools {
		return nil, nil
	}
	schema, _ := jsonvalue.Decode(`{"type":"object","properties":{}}`)
	annotations, _ := jsonvalue.Decode(`{"readOnlyHint":true}`)
	return []mcp.ToolDescription{{Name: "echo", Description: jsonvalue.TextValue("echo"), InputSchema: schema, Annotations: annotations}}, nil
}
func (peer *mcpConnectPeer) CallTool(_ context.Context, name string, args jsonvalue.Value) (string, error) {
	call := mcpConnectCall{Alias: peer.Spec.Alias, Name: name, Arguments: args}
	peer.Trace.Called = append(peer.Trace.Called, call)
	return protocol.PythonJSON(struct {
		Alias     string          `json:"alias"`
		Arguments jsonvalue.Value `json:"arguments"`
		Name      string          `json:"name"`
	}{call.Alias, args.Sorted(), name}, false, false)
}

type mcpConnectProvider struct {
	Blocks []protocol.Block
	Names  [][]protocol.ToolName
}

func (provider *mcpConnectProvider) Complete(_ context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
	names := make([]protocol.ToolName, 0, len(request.Tools))
	for _, schema := range request.Tools {
		names = append(names, schema.Name)
	}
	provider.Names = append(provider.Names, names)
	if len(provider.Names) == 1 {
		return fakeReply(provider.Blocks, protocol.StopToolUse), nil
	}
	return fakeReply([]protocol.Block{protocol.NewTextBlock("done")}, protocol.StopEndTurn), nil
}

func TestDynamicMCPConnectMatchesRealPythonAgent(t *testing.T) {
	var fixture struct {
		Rows []struct {
			Recipe struct {
				Servers    []mcpConnectSpec      `json:"servers"`
				Operations []mcpConnectOperation `json:"operations"`
			} `json:"recipe"`
			Output        string                `json:"output"`
			Results       []string              `json:"results"`
			RequestNames  [][]protocol.ToolName `json:"request_names"`
			FactoryCalls  []string              `json:"factory_calls"`
			ListCalls     []string              `json:"list_calls"`
			ToolCalls     []mcpConnectCall      `json:"tool_calls"`
			Connected     map[string]string     `json:"connected"`
			Problems      []string              `json:"problems"`
			ConnectSchema jsonvalue.Value       `json:"connect_schema"`
		} `json:"rows"`
	}
	data, err := os.ReadFile("../testdata/python-mcp-connect.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for index, row := range fixture.Rows {
		t.Run(fmt.Sprint(index), func(t *testing.T) {
			trace := &mcpConnectTrace{Factory: []string{}, Listed: []string{}, Called: []mcpConnectCall{}}
			servers := make([]MCPServer, 0, len(row.Recipe.Servers))
			for _, spec := range row.Recipe.Servers {
				entry := MCPServer{Alias: spec.Alias}
				if spec.Factory {
					entry.Factory = func(context.Context) (MCPClient, error) {
						trace.Factory = append(trace.Factory, spec.Alias)
						return &mcpConnectPeer{Spec: spec, Trace: trace}, nil
					}
				} else {
					entry.Client = &mcpConnectPeer{Spec: spec, Trace: trace}
				}
				servers = append(servers, entry)
			}
			provider := &mcpConnectProvider{}
			for i, operation := range row.Recipe.Operations {
				var input protocol.ToolInput
				if operation.Connect != nil {
					input = protocol.ConnectMCPToolInput(protocol.ConnectMCPInput{Name: *operation.Connect})
				} else {
					var err error
					input, err = protocol.MCPToolInput(operation.Call, operation.Arguments)
					if err != nil {
						t.Fatal(err)
					}
				}
				provider.Blocks = append(provider.Blocks, protocol.NewToolUse(fmt.Sprintf("call%d", i), input))
			}
			config := runtimeConfig(t.TempDir(), provider)
			config.Mode = ModeAuto
			config.MaxRounds = 3
			config.MCPTools = true
			config.MCPServers = servers
			config.ToolSelection = SelectTools(protocol.ToolConnectMCP)
			session, err := NewRuntimeSession(config)
			if err != nil {
				t.Fatal(err)
			}
			initial := session.gate.catalog
			schema, _ := initial.Lookup(protocol.ToolConnectMCP)
			actual, _ := json.Marshal(schema.schema)
			value, _ := jsonvalue.Decode(string(actual))
			a, _ := value.Sorted().MarshalJSON()
			b, _ := row.ConnectSchema.Sorted().MarshalJSON()
			if string(a) != string(b) {
				t.Fatalf("connect schema differs: %s != %s", a, b)
			}
			// Configuration detaches aliases and factories before model execution.
			if len(servers) > 0 {
				servers[0].Alias = "changed"
				servers[0].Factory = nil
				servers[0].Client = nil
			}
			output, err := session.Run(context.Background(), "connect")
			if err != nil || output != row.Output {
				t.Fatalf("run: %s %v", output, err)
			}
			var results []string
			for _, message := range session.Messages() {
				blocks, _ := message.Content.Blocks()
				for _, block := range blocks {
					if result, ok := block.ToolResult(); ok {
						results = append(results, result.Content)
					}
				}
			}
			if !reflect.DeepEqual(results, row.Results) {
				t.Fatalf("results: %v != %v", results, row.Results)
			}
			if !reflect.DeepEqual(provider.Names, row.RequestNames) {
				t.Fatalf("request names: %v != %v", provider.Names, row.RequestNames)
			}
			if !reflect.DeepEqual(trace.Factory, row.FactoryCalls) || !reflect.DeepEqual(trace.Listed, row.ListCalls) {
				t.Fatalf("factories/list: %+v", trace)
			}
			actualCalls, _ := json.Marshal(trace.Called)
			expectedCalls, _ := json.Marshal(row.ToolCalls)
			if string(actualCalls) != string(expectedCalls) {
				t.Fatalf("call ownership: %s != %s", actualCalls, expectedCalls)
			}
			if !reflect.DeepEqual(session.mcp.connected, row.Connected) {
				t.Fatal("alias connection state changed")
			}
			problems := make([]string, 0)
			for _, entry := range session.MCPProblems().Entries {
				problems = append(problems, entry.Text)
			}
			if !reflect.DeepEqual(problems, row.Problems) {
				t.Fatalf("diagnostics: %v != %v", problems, row.Problems)
			}
			if !reflect.DeepEqual(initial.Names(), []protocol.ToolName{protocol.ToolConnectMCP}) {
				t.Fatal("old published snapshot changed")
			}
		})
	}
}

func TestMCPConnectionRequiresExternalPermissionBeforeFactory(t *testing.T) {
	for _, mode := range []PermissionMode{ModeReadonly, ModeInteractive} {
		t.Run(string(mode), func(t *testing.T) {
			var factories atomic.Int64
			input := protocol.ConnectMCPToolInput(protocol.ConnectMCPInput{Name: "server"})
			provider := &mcpConnectProvider{Blocks: []protocol.Block{protocol.NewToolUse("connect", input)}}
			config := runtimeConfig(t.TempDir(), provider)
			config.Mode = mode
			config.MCPTools = true
			config.MCPServers = []MCPServer{{Alias: "server", Factory: func(context.Context) (MCPClient, error) {
				factories.Add(1)
				return simpleMCPPeer("server", "result"), nil
			}}}
			session, err := NewRuntimeSession(config)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = session.Run(context.Background(), "connect"); err != nil {
				t.Fatal(err)
			}
			if factories.Load() != 0 || len(session.mcp.connected) != 0 {
				t.Fatal("denied connection reached factory")
			}
		})
	}
}

func TestMCPFactoryCancellationDoesNotPublish(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	config := runtimeConfig(t.TempDir(), &mcpConnectProvider{Blocks: []protocol.Block{protocol.NewToolUse("connect", protocol.ConnectMCPToolInput(protocol.ConnectMCPInput{Name: "server"}))}})
	config.Mode = ModeAuto
	config.MCPTools = true
	config.MCPServers = []MCPServer{{Alias: "server", Factory: func(ctx context.Context) (MCPClient, error) { cancel(); return nil, ctx.Err() }}}
	session, err := NewRuntimeSession(config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = session.Run(ctx, "connect"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	if len(session.mcp.connected) != 0 || len(session.gate.catalog.Names()) != 11 {
		t.Fatal("cancelled factory published tools")
	}
}
