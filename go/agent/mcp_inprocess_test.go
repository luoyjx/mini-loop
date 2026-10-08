package agent

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/mcp"
	"github.com/luoyjx/mini-loop/go/protocol"
)

var _ ManagedMCPClient = (*mcp.InProcessClient)(nil)

func TestInProcessMCPStaysBehindExternalRuntimeGate(t *testing.T) {
	for _, mode := range []PermissionMode{ModeAuto, ModeReadonly} {
		t.Run(string(mode), func(t *testing.T) {
			var calls atomic.Int32
			schema, _ := jsonvalue.Decode(`{"type":"object"}`)
			hint, _ := jsonvalue.Decode(`{"readOnlyHint":true}`)
			client := mcp.NewInProcess("local", []mcp.InProcessTool{{Definition: mcp.ToolDescription{Name: "echo", Description: jsonvalue.TextValue("echo"), InputSchema: schema, Annotations: hint}, Handler: func(context.Context, mcp.InProcessValue) (mcp.InProcessValue, error) {
				calls.Add(1)
				return jsonvalue.TextValue("native"), nil
			}}})
			input, err := protocol.MCPToolInput("mcp__local__echo", jsonvalue.ObjectValue(nil))
			if err != nil {
				t.Fatal(err)
			}
			provider := &mcpConnectProvider{Blocks: []protocol.Block{protocol.NewToolUse("connect", protocol.ConnectMCPToolInput(protocol.ConnectMCPInput{Name: "friendly"})), protocol.NewToolUse("echo", input)}}
			config := runtimeConfig(t.TempDir(), provider)
			config.Mode = mode
			config.MCPTools = true
			config.MCPServers = []MCPServer{{Alias: "friendly", Client: client}}
			session, err := NewRuntimeSession(config)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := session.Run(context.Background(), "connect"); err != nil {
				t.Fatal(err)
			}
			definition, published := session.gate.catalog.Lookup("mcp__local__echo")
			if mode == ModeAuto {
				if calls.Load() != 1 || !published || definition.Risk() != RiskExternal {
					t.Fatal("in-process client did not cross external gate")
				}
			} else if calls.Load() != 0 || published {
				t.Fatal("readonly connection invoked handler")
			}
		})
	}
}
