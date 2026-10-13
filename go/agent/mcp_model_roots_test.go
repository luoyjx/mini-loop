package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/mcp"
	"github.com/luoyjx/mini-loop/go/protocol"
)

type mcpRootProvider struct {
	input    string
	batch    bool
	requests int
}

func (provider *mcpRootProvider) Complete(_ context.Context, _ protocol.ModelRequest) (protocol.ModelReply, error) {
	provider.requests++
	if provider.requests > 1 {
		return fakeReply([]protocol.Block{protocol.NewTextBlock("done")}, protocol.StopEndTurn), nil
	}
	blocks := []protocol.Block{}
	if provider.batch {
		first, err := protocol.DecodeModelToolInput("mcp__peer__echo", []byte(`{"first":1}`))
		if err != nil {
			return protocol.ModelReply{}, err
		}
		blocks = append(blocks, protocol.NewToolUse("first", first))
	}
	input, err := protocol.DecodeModelToolInput("mcp__peer__echo", []byte(provider.input))
	if err != nil {
		return protocol.ModelReply{}, err
	}
	blocks = append(blocks, protocol.NewToolUse("model", input))
	return fakeReply(blocks, protocol.StopToolUse), nil
}

func TestMCPModelRootsMatchActualSourceBatchAndGate(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-mcp-model-roots.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name    string
			Mode    PermissionMode
			Input   string `json:"input_json"`
			Batch   bool
			Output  *string
			Error   *struct{ Kind, Message string }
			Trace   []string
			Calls   []jsonvalue.Value
			Results []struct {
				Output         string
				Denied, Failed bool
			}
			Requests            int
			ModelInputUnchanged bool `json:"model_input_unchanged"`
		}
	}
	if err = json.Unmarshal(data, &fixture); err != nil || len(fixture.Cases) != 52 {
		t.Fatal(err, len(fixture.Cases))
	}
	for _, row := range fixture.Cases {
		t.Run(row.Name, func(t *testing.T) {
			trace := []string{}
			calls := []jsonvalue.Value{}
			results := []ToolOutcome{}
			peer := simpleMCPPeer("peer", "")
			peer.callHook = func(_ context.Context, value jsonvalue.Value) {
				trace = append(trace, "handler")
				calls = append(calls, value)
			}
			// A concrete peer returns each prepared object in Source compact form.
			client := mcpModelRootPeer{peer: peer}
			registry := &MCPRegistry{}
			published, err := registry.Register(context.Background(), client, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			provider := &mcpRootProvider{input: row.Input, batch: row.Batch}
			config := runtimeConfig(t.TempDir(), provider)
			config.MCPCatalog = published.Catalog
			config.Mode = row.Mode
			config.Hooks = GateHooks{Before: []BeforeHook{beforeHookFunc(func(context.Context, ToolAuthority, ToolCall) (BeforeDecision, error) {
				trace = append(trace, "before")
				return KeepToolCall(), nil
			})}, After: []AfterHook{afterHookFunc(func(_ context.Context, _ ToolAuthority, _ ToolCall, output string) (string, error) {
				trace = append(trace, "after")
				return output, nil
			})}, Observers: []ResultObserver{observerFunc(func(_ context.Context, _ ToolAuthority, _ ToolCall, result ToolOutcome) error {
				trace = append(trace, "result")
				results = append(results, result)
				return nil
			})}}
			session, err := NewRuntimeSession(config)
			if err != nil {
				t.Fatal(err)
			}
			output, err := session.Run(context.Background(), "call MCP")
			if row.Error != nil {
				var failure protocol.ModelBatchInputFailure
				if !errors.As(err, &failure) || failure.SourceClass() != row.Error.Kind || failure.Error() != row.Error.Message {
					t.Fatal("Source preparation failure differs", err, row.Error)
				}
			} else if err != nil || row.Output == nil || output != *row.Output {
				t.Fatal(output, row.Output, err)
			}
			if !reflect.DeepEqual(trace, row.Trace) || !reflect.DeepEqual(calls, row.Calls) || len(results) != len(row.Results) || provider.requests != row.Requests {
				t.Fatal("Source effects/hook stages differ", trace, row.Trace, calls, row.Calls, results, provider.requests, row.Requests)
			}
			for i, result := range results {
				if result.Output != row.Results[i].Output || result.Denied != row.Results[i].Denied || result.Failed != row.Results[i].Failed {
					t.Fatal(result, row.Results[i])
				}
			}
			messages := session.Messages()
			blocks, _ := messages[1].Content.Blocks()
			use, _ := blocks[len(blocks)-1].ToolUse()
			original, err := use.Input.MarshalJSON()
			if err != nil {
				t.Fatal(err)
			}
			value, err := jsonvalue.Decode(row.Input)
			if err != nil {
				t.Fatal(err)
			}
			expected, _ := value.MarshalJSON()
			if string(original) != string(expected) || !row.ModelInputUnchanged {
				t.Fatal("prepared object overwrote model history", string(original), string(expected))
			}
		})
	}
}

type mcpModelRootPeer struct{ peer *mcpPeer }

func (client mcpModelRootPeer) Name() string { return client.peer.Name() }
func (client mcpModelRootPeer) ListTools(ctx context.Context) ([]mcp.ToolDescription, error) {
	return client.peer.ListTools(ctx)
}
func (client mcpModelRootPeer) CallTool(ctx context.Context, name string, value jsonvalue.Value) (string, error) {
	if client.peer.callHook != nil {
		client.peer.callHook(ctx, value)
	}
	return protocol.PythonJSON(value.Sorted(), false, true)
}
