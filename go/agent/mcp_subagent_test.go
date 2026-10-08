package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/mcp"
	"github.com/luoyjx/mini-loop/go/protocol"
)

type childMCPTrace struct {
	mu                     sync.Mutex
	created                int
	listed, called, closed []int
}
type childMCPPeer struct {
	trace    *childMCPTrace
	identity int
	call     func(context.Context) (string, error)
}

func (peer *childMCPPeer) Name() string { return "raw.name" }
func (peer *childMCPPeer) ListTools(context.Context) ([]mcp.ToolDescription, error) {
	peer.trace.mu.Lock()
	peer.trace.listed = append(peer.trace.listed, peer.identity)
	peer.trace.mu.Unlock()
	schema, _ := jsonvalue.Decode(`{"type":"object"}`)
	return []mcp.ToolDescription{{Name: "echo", Description: jsonvalue.TextValue("echo"), InputSchema: schema}}, nil
}
func (peer *childMCPPeer) CallTool(ctx context.Context, _ string, _ jsonvalue.Value) (string, error) {
	peer.trace.mu.Lock()
	peer.trace.called = append(peer.trace.called, peer.identity)
	peer.trace.mu.Unlock()
	if peer.call != nil {
		return peer.call(ctx)
	}
	return fmt.Sprintf("client%d", peer.identity), nil
}
func (peer *childMCPPeer) Close() error {
	peer.trace.mu.Lock()
	peer.trace.closed = append(peer.trace.closed, peer.identity)
	peer.trace.mu.Unlock()
	return nil
}

type childMCPPolicy struct{}

func (childMCPPolicy) Select(_ AgentRole, parent *ToolCatalog) (*ToolCatalog, error) {
	var definitions []ToolDefinition
	for _, definition := range parent.ordered {
		if definition.Name() == protocol.ToolConnectMCP || protocol.IsMCPToolName(definition.Name()) {
			definitions = append(definitions, definition)
		}
	}
	return NewToolCatalog(definitions...)
}

type childMCPProvider struct {
	custom, preconnected          bool
	role                          AgentRole
	parentRequests, childRequests int
	childNames                    [][]protocol.ToolName
	childResults                  []string
}

func childMCPCall(id string) protocol.Block {
	input, err := protocol.MCPToolInput("mcp__raw_name__echo", jsonvalue.ObjectValue(nil))
	if err != nil {
		panic(err)
	}
	return protocol.NewToolUse(id, input)
}
func childMCPConnect(id string) protocol.Block {
	return protocol.NewToolUse(id, protocol.ConnectMCPToolInput(protocol.ConnectMCPInput{Name: "friendly"}))
}
func (provider *childMCPProvider) Complete(_ context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
	if request.System != nil && strings.Contains(*request.System, " subagent in ") {
		provider.childRequests++
		names := []protocol.ToolName{}
		for _, schema := range request.Tools {
			if schema.Name == protocol.ToolConnectMCP || protocol.IsMCPToolName(schema.Name) {
				names = append(names, schema.Name)
			}
		}
		provider.childNames = append(provider.childNames, names)
		if provider.custom && provider.childRequests == 1 {
			var blocks []protocol.Block
			if provider.preconnected && provider.role != RoleExplore {
				blocks = append(blocks, childMCPCall("borrowed"))
			}
			blocks = append(blocks, childMCPConnect("child-connect"), childMCPCall("child-echo"))
			return fakeReply(blocks, protocol.StopToolUse), nil
		}
		// Only the final request contains the child's paired batch results.
		for _, message := range request.Messages {
			blocks, _ := message.Content.Blocks()
			for _, block := range blocks {
				if result, ok := block.ToolResult(); ok {
					provider.childResults = append(provider.childResults, result.Content)
				}
			}
		}
		return fakeReply([]protocol.Block{protocol.NewTextBlock("child done")}, protocol.StopEndTurn), nil
	}
	provider.parentRequests++
	if provider.parentRequests == 1 {
		var blocks []protocol.Block
		if provider.preconnected {
			blocks = append(blocks, childMCPConnect("parent-connect"))
		}
		role := protocol.AgentType(provider.role)
		blocks = append(blocks, protocol.NewToolUse("task", protocol.TaskToolInput(protocol.TaskInput{Prompt: "delegated", AgentType: &role})))
		if provider.preconnected {
			blocks = append(blocks, childMCPCall("parent-echo"))
		}
		return fakeReply(blocks, protocol.StopToolUse), nil
	}
	return fakeReply([]protocol.Block{protocol.NewTextBlock("parent done")}, protocol.StopEndTurn), nil
}

func TestTaskMCPSelectionAndChildPublicationMatchPython(t *testing.T) {
	var fixture struct {
		Rows []struct {
			Custom, Preconnected bool
			Role                 AgentRole
			Output               string
			Created              int
			Listed, Called       []int
			ChildNames           [][]protocol.ToolName `json:"child_names"`
			ChildResults         []string              `json:"child_results"`
			ParentResults        []string              `json:"parent_results"`
			ParentConnected      map[string]string     `json:"parent_connected"`
			ParentNames          []protocol.ToolName   `json:"parent_names"`
		} `json:"rows"`
	}
	data, err := os.ReadFile("../testdata/python-mcp-subagent.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for index, row := range fixture.Rows {
		t.Run(fmt.Sprint(index), func(t *testing.T) {
			trace := &childMCPTrace{listed: []int{}, called: []int{}, closed: []int{}}
			provider := &childMCPProvider{custom: row.Custom, preconnected: row.Preconnected, role: row.Role, childResults: []string{}}
			config := managerTestConfig(t.TempDir(), provider)
			config.Services.MCPTools = true
			config.Services.MCPServers = []ManagedMCPServer{{Alias: "friendly", Factory: func(context.Context) (*MCPConnection, error) {
				trace.mu.Lock()
				peer := &childMCPPeer{trace: trace, identity: trace.created}
				trace.created++
				trace.mu.Unlock()
				return NewMCPConnection(peer)
			}}}
			if row.Custom {
				config.Services.RoleToolPolicy = childMCPPolicy{}
			}
			manager := makeManager(t, config)
			parent := createManaged(t, manager, CreateSessionRequest{Owner: "owner"})
			// Match the Source fixture's explicit operator policy, not production defaults.
			// Both policy implementations still impose monotonic readonly restrictions.
			parent.core.gate.policy, err = NewPermissionPolicy(nil, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			output, err := parent.Run(context.Background(), "delegate")
			if err != nil || output != row.Output {
				t.Fatal(output, err)
			}
			if !reflect.DeepEqual(provider.childNames, row.ChildNames) || !reflect.DeepEqual(provider.childResults, row.ChildResults) {
				t.Fatalf("child names/results: %v %v", provider.childNames, provider.childResults)
			}
			parentResults := []string{}
			for _, message := range parent.Messages() {
				blocks, _ := message.Content.Blocks()
				for _, block := range blocks {
					if result, ok := block.ToolResult(); ok {
						parentResults = append(parentResults, result.Content)
					}
				}
			}
			names := []protocol.ToolName{}
			for _, name := range parent.core.gate.catalog.Names() {
				if name == protocol.ToolConnectMCP || protocol.IsMCPToolName(name) {
					names = append(names, name)
				}
			}
			if !reflect.DeepEqual(parentResults, row.ParentResults) || !reflect.DeepEqual(names, row.ParentNames) || !reflect.DeepEqual(parent.core.mcp.connected, row.ParentConnected) {
				t.Fatal("child publication changed parent state", parentResults, names, parent.core.mcp.connected)
			}
			if trace.created != row.Created || !reflect.DeepEqual(trace.listed, row.Listed) || !reflect.DeepEqual(trace.called, row.Called) {
				t.Fatalf("factory ownership: %+v", trace)
			}
			if len(trace.closed) != 0 {
				t.Fatal("task completion prematurely closed retained client")
			}
			waitMCPDeletion(t, manager, parent)
			expected := []int{}
			for index := 0; index < row.Created; index++ {
				expected = append(expected, index)
			}
			if !reflect.DeepEqual(trace.closed, expected) {
				t.Fatalf("parent deletion lost child factory ownership: %v want %v", trace.closed, expected)
			}
		})
	}
}

func TestManagedParentRetainsMCPUntilCancelledTaskChildDrains(t *testing.T) {
	trace := &childMCPTrace{listed: []int{}, called: []int{}, closed: []int{}}
	entered, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	provider := &childMCPProvider{custom: true, role: RoleWorker, childResults: []string{}}
	config := managerTestConfig(t.TempDir(), provider)
	config.Services.MCPTools = true
	config.Services.RoleToolPolicy = childMCPPolicy{}
	config.Services.MCPServers = []ManagedMCPServer{{Alias: "friendly", Factory: func(context.Context) (*MCPConnection, error) {
		peer := &childMCPPeer{trace: trace, identity: 0, call: func(ctx context.Context) (string, error) {
			close(entered)
			<-ctx.Done()
			close(cancelled)
			<-release
			return "", ctx.Err()
		}}
		return NewMCPConnection(peer)
	}}}
	manager := makeManager(t, config)
	parent := createManaged(t, manager, CreateSessionRequest{Owner: "owner"})
	var err error
	parent.core.gate.policy, err = NewPermissionPolicy(nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := parent.Run(context.Background(), "delegate"); done <- err }()
	<-entered
	if ok, err := manager.Delete("owner", parent.ID(), DeleteSessionOptions{}); !ok || err != nil {
		t.Fatal(ok, err)
	}
	<-cancelled
	trace.mu.Lock()
	closed := len(trace.closed)
	trace.mu.Unlock()
	if closed != 0 {
		t.Fatal("parent closed client before child returned")
	}
	if _, err := os.Stat(parent.core.workspace); err != nil {
		t.Fatal("parent reclaimed workspace before child returned")
	}
	close(release)
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation did not propagate", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := manager.WaitCleanup(ctx); err != nil {
		t.Fatal(err)
	}
	trace.mu.Lock()
	defer trace.mu.Unlock()
	if !reflect.DeepEqual(trace.closed, []int{0}) {
		t.Fatal("child-only connection leaked", trace.closed)
	}
}
