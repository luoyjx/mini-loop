package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/mcp"
	"github.com/luoyjx/mini-loop/go/protocol"
)

var _ ManagedMCPClient = (*mcp.StdioClient)(nil)

type mcpCloseTrace struct {
	mu      sync.Mutex
	created int
	closed  []int
}

func (trace *mcpCloseTrace) snapshot() []int {
	trace.mu.Lock()
	defer trace.mu.Unlock()
	result := append([]int{}, trace.closed...)
	sort.Ints(result)
	return result
}

type lifecycleMCPPeer struct {
	trace    *mcpCloseTrace
	identity int
	list     func(context.Context) ([]mcp.ToolDescription, error)
	close    func() error
}

func (peer *lifecycleMCPPeer) Name() string { return "shared" }
func (peer *lifecycleMCPPeer) ListTools(ctx context.Context) ([]mcp.ToolDescription, error) {
	if peer.list != nil {
		return peer.list(ctx)
	}
	schema, _ := jsonvalue.Decode(`{"type":"object"}`)
	return []mcp.ToolDescription{{Name: "echo", Description: jsonvalue.TextValue("echo"), InputSchema: schema}}, nil
}
func (peer *lifecycleMCPPeer) CallTool(context.Context, string, jsonvalue.Value) (string, error) {
	return "echo", nil
}
func (peer *lifecycleMCPPeer) Close() error {
	peer.trace.mu.Lock()
	peer.trace.closed = append(peer.trace.closed, peer.identity)
	peer.trace.mu.Unlock()
	if peer.close != nil {
		return peer.close()
	}
	return nil
}
func newLifecycleMCP(t *testing.T, trace *mcpCloseTrace) (*MCPConnection, *lifecycleMCPPeer) {
	t.Helper()
	trace.mu.Lock()
	peer := &lifecycleMCPPeer{trace: trace, identity: trace.created}
	trace.created++
	trace.mu.Unlock()
	connection, err := NewMCPConnection(peer)
	if err != nil {
		t.Fatal(err)
	}
	return connection, peer
}

type lifecycleMCPProvider struct {
	mu    sync.Mutex
	count int
}

func (provider *lifecycleMCPProvider) Complete(context.Context, protocol.ModelRequest) (protocol.ModelReply, error) {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	provider.count++
	if provider.count%2 == 1 {
		return fakeReply([]protocol.Block{protocol.NewToolUse(fmt.Sprintf("connect%d", provider.count), protocol.ConnectMCPToolInput(protocol.ConnectMCPInput{Name: "friendly"}))}, protocol.StopToolUse), nil
	}
	return fakeReply([]protocol.Block{protocol.NewTextBlock("done")}, protocol.StopEndTurn), nil
}
func runLifecycleMCP(t *testing.T, session *ManagedSession) {
	t.Helper()
	result, err := session.Run(context.Background(), "connect")
	if err != nil || result != "done" {
		t.Fatalf("connect: %s %v", result, err)
	}
}
func waitMCPDeletion(t *testing.T, manager *SessionManager, session *ManagedSession) {
	t.Helper()
	ok, err := manager.Delete(session.Owner(), session.ID(), DeleteSessionOptions{})
	if err != nil || !ok {
		t.Fatalf("delete: %v %v", ok, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := manager.WaitCleanup(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestManagedMCPLifetimesMatchPythonManager(t *testing.T) {
	var fixture struct {
		Rows []struct {
			Factory       bool `json:"factory"`
			Fork          bool `json:"fork"`
			StopOnly      bool `json:"stop_only"`
			Created       int  `json:"created"`
			SecondInitial struct {
				Names     []protocol.ToolName `json:"names"`
				Connected map[string]string   `json:"connected"`
			} `json:"second_initial"`
			Steps []struct {
				Point  string `json:"point"`
				Closed []int  `json:"closed"`
			} `json:"steps"`
		} `json:"rows"`
	}
	data, err := os.ReadFile("../testdata/python-mcp-lifecycle.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for index, row := range fixture.Rows {
		t.Run(fmt.Sprint(index), func(t *testing.T) {
			trace := &mcpCloseTrace{}
			server := ManagedMCPServer{Alias: "friendly"}
			if row.Factory {
				server.Factory = func(context.Context) (*MCPConnection, error) {
					connection, _ := newLifecycleMCP(t, trace)
					return connection, nil
				}
			} else {
				server.Connection, _ = newLifecycleMCP(t, trace)
			}
			config := managerTestConfig(t.TempDir(), &lifecycleMCPProvider{})
			config.Services.MCPTools = true
			config.Services.MCPServers = []ManagedMCPServer{server}
			manager := makeManager(t, config)
			config.Services.MCPServers[0] = ManagedMCPServer{Alias: "changed"}
			first := createManaged(t, manager, CreateSessionRequest{Owner: "owner", ToolSelection: SelectTools(protocol.ToolConnectMCP)})
			runLifecycleMCP(t, first)
			var second *ManagedSession
			if row.Fork {
				second, err = manager.Fork(context.Background(), "owner", first.ID())
				if err != nil {
					t.Fatal(err)
				}
			} else {
				second = createManaged(t, manager, CreateSessionRequest{Owner: "owner", ToolSelection: SelectTools(protocol.ToolConnectMCP)})
			}
			// Source used a one-tool custom base registry; compare the MCP portion when
			// native fork construction includes its manager's default workspace tools.
			names := []protocol.ToolName{}
			for _, name := range second.core.gate.catalog.Names() {
				if name == protocol.ToolConnectMCP || protocol.IsMCPToolName(name) {
					names = append(names, name)
				}
			}
			if !reflect.DeepEqual(names, row.SecondInitial.Names) || !reflect.DeepEqual(second.core.mcp.connected, row.SecondInitial.Connected) {
				t.Fatal("fork inherited connected tools/state")
			}
			runLifecycleMCP(t, second)
			for _, step := range row.Steps {
				switch step.Point {
				case "first_deleted":
					waitMCPDeletion(t, manager, first)
				case "second_deleted":
					waitMCPDeletion(t, manager, second)
				case "stopped":
					if err := manager.Stop(context.Background()); err != nil {
						t.Fatal(err)
					}
					if err := manager.Stop(context.Background()); err != nil {
						t.Fatal(err)
					}
				}
				if got := trace.snapshot(); !reflect.DeepEqual(got, step.Closed) {
					t.Fatalf("%s closes=%v want=%v", step.Point, got, step.Closed)
				}
			}
			if trace.created != row.Created {
				t.Fatalf("created=%d want=%d", trace.created, row.Created)
			}
		})
	}
}

func TestManagedMCPDiscoveryDrainsBeforeLastClose(t *testing.T) {
	trace := &mcpCloseTrace{}
	connection, peer := newLifecycleMCP(t, trace)
	entered, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	peer.list = func(ctx context.Context) ([]mcp.ToolDescription, error) {
		close(entered)
		<-ctx.Done()
		close(cancelled)
		<-release
		return nil, ctx.Err()
	}
	config := managerTestConfig(t.TempDir(), &lifecycleMCPProvider{})
	config.Services.MCPTools = true
	config.Services.MCPServers = []ManagedMCPServer{{Alias: "friendly", Connection: connection}}
	manager := makeManager(t, config)
	session := createManaged(t, manager, CreateSessionRequest{Owner: "owner"})
	done := make(chan error, 1)
	go func() { _, err := session.Run(context.Background(), "connect"); done <- err }()
	<-entered
	if ok, err := manager.Delete("owner", session.ID(), DeleteSessionOptions{}); !ok || err != nil {
		t.Fatal(ok, err)
	}
	<-cancelled
	if got := trace.snapshot(); len(got) != 0 {
		t.Fatal("client closed before discovery returned")
	}
	if _, err := os.Stat(session.core.workspace); err != nil {
		t.Fatal("workspace reclaimed before discovery drained")
	}
	close(release)
	<-done
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := manager.WaitCleanup(ctx); err != nil {
		t.Fatal(err)
	}
	if got := trace.snapshot(); !reflect.DeepEqual(got, []int{0}) {
		t.Fatal(got)
	}
}

func TestManagedMCPFailedFactoryStillHasCleanupOwnership(t *testing.T) {
	for _, factoryError := range []bool{false, true} {
		t.Run(fmt.Sprint(factoryError), func(t *testing.T) {
			trace := &mcpCloseTrace{}
			connection, peer := newLifecycleMCP(t, trace)
			peer.list = func(context.Context) ([]mcp.ToolDescription, error) { return nil, errors.New("discovery failed") }
			peer.close = func() error { return errors.New("close failed") }
			config := managerTestConfig(t.TempDir(), &lifecycleMCPProvider{})
			config.Services.MCPTools = true
			config.Services.MCPServers = []ManagedMCPServer{{Alias: "friendly", Factory: func(context.Context) (*MCPConnection, error) {
				if factoryError {
					return connection, errors.New("factory failed")
				}
				return connection, nil
			}}}
			manager := makeManager(t, config)
			session := createManaged(t, manager, CreateSessionRequest{Owner: "owner"})
			runLifecycleMCP(t, session)
			if len(session.core.mcp.connected) != 0 {
				t.Fatal("failed discovery marked connected")
			}
			waitMCPDeletion(t, manager, session)
			if got := trace.snapshot(); !reflect.DeepEqual(got, []int{0}) {
				t.Fatal(got)
			}
			diagnostics := manager.CleanupErrors()
			if len(diagnostics) != 1 || diagnostics[0].Workspace != "mcp" || diagnostics[0].Error != "close failed" {
				t.Fatalf("diagnostics: %+v", diagnostics)
			}
		})
	}
}

func TestMCPConnectionWaitsForCloseBeforeReuse(t *testing.T) {
	trace := &mcpCloseTrace{}
	connection, peer := newLifecycleMCP(t, trace)
	entered, release := make(chan struct{}), make(chan struct{})
	peer.close = func() error { close(entered); <-release; return nil }
	first := &mcpLifetime{}
	if err := first.retain(context.Background(), connection); err != nil {
		t.Fatal(err)
	}
	closed := make(chan error, 1)
	go func() { closed <- first.close() }()
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	second := &mcpLifetime{}
	if err := second.retain(ctx, connection); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	close(release)
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
	peer.close = nil
	if err := second.retain(context.Background(), connection); err != nil {
		t.Fatal(err)
	}
	if err := second.close(); err != nil {
		t.Fatal(err)
	}
	if got := trace.snapshot(); !reflect.DeepEqual(got, []int{0, 0}) {
		t.Fatal(got)
	}
}

func TestManagedMCPReplacementsRetainDistinctClientsAndDeduplicateAliases(t *testing.T) {
	trace := &mcpCloseTrace{}
	first, peer := newLifecycleMCP(t, trace)
	second, _ := newLifecycleMCP(t, trace)
	peer.close = func() error { panic("close fault") }
	provider := &mcpConnectProvider{}
	for index, alias := range []string{"friendly", "duplicate", "second"} {
		provider.Blocks = append(provider.Blocks, protocol.NewToolUse(fmt.Sprint(index), protocol.ConnectMCPToolInput(protocol.ConnectMCPInput{Name: alias})))
	}
	config := managerTestConfig(t.TempDir(), provider)
	config.Services.MCPTools = true
	config.Services.MCPServers = []ManagedMCPServer{{Alias: "friendly", Connection: first}, {Alias: "duplicate", Connection: first}, {Alias: "second", Connection: second}}
	manager := makeManager(t, config)
	session := createManaged(t, manager, CreateSessionRequest{Owner: "owner"})
	runLifecycleMCP(t, session)
	waitMCPDeletion(t, manager, session)
	if got := trace.snapshot(); !reflect.DeepEqual(got, []int{0, 1}) {
		t.Fatal("raw-name replacement lost ownership or duplicate alias closed twice", got)
	}
	diagnostics := manager.CleanupErrors()
	if len(diagnostics) != 1 || diagnostics[0].Error != "MCP close panicked (string)" {
		t.Fatalf("close failure did not remain diagnostic: %+v", diagnostics)
	}
}

func TestManagerMCPValidationAndUnusedConnections(t *testing.T) {
	trace := &mcpCloseTrace{}
	connection, _ := newLifecycleMCP(t, trace)
	for _, servers := range [][]ManagedMCPServer{
		{{Alias: "same", Connection: connection}, {Alias: "same", Connection: connection}},
		{{Alias: "none"}},
		{{Alias: "both", Connection: connection, Factory: func(context.Context) (*MCPConnection, error) { return connection, nil }}},
		{{Alias: "uninitialized", Connection: &MCPConnection{}}},
	} {
		root := t.TempDir() + "/not-created"
		config := managerTestConfig(root, &FakeProvider{})
		config.Services.MCPTools = true
		config.Services.MCPServers = servers
		if _, err := NewSessionManager(config); err == nil {
			t.Fatal("invalid managed server accepted")
		}
		if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("invalid configuration allocated workspace")
		}
	}
	config := managerTestConfig(t.TempDir(), &FakeProvider{})
	config.Services.MCPTools = true
	config.Services.MCPServers = []ManagedMCPServer{{Alias: "friendly", Connection: connection}}
	manager := makeManager(t, config)
	session := createManaged(t, manager, CreateSessionRequest{Owner: "owner"})
	waitMCPDeletion(t, manager, session)
	if err := manager.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := trace.snapshot(); len(got) != 0 {
		t.Fatal("configured but never acquired client was closed", got)
	}
}
