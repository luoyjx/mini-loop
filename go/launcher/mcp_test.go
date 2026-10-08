package launcher

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/config"
	"github.com/luoyjx/mini-loop/go/httpapi"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/mcp"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/secrets"
)

type launcherMCPCounts struct{ Listed, Called, Closed atomic.Int32 }
type launcherMCPPeer struct{ counts *launcherMCPCounts }

func (peer *launcherMCPPeer) Name() string { return "raw.name" }
func (peer *launcherMCPPeer) ListTools(context.Context) ([]mcp.ToolDescription, error) {
	peer.counts.Listed.Add(1)
	schema, _ := jsonvalue.Decode(`{"type":"object"}`)
	return []mcp.ToolDescription{{Name: "echo", Description: jsonvalue.TextValue("echo"), InputSchema: schema}}, nil
}
func (peer *launcherMCPPeer) CallTool(context.Context, string, jsonvalue.Value) (string, error) {
	peer.counts.Called.Add(1)
	return "echo", nil
}
func (peer *launcherMCPPeer) Close() error { peer.counts.Closed.Add(1); return nil }

type launcherMCPRequest struct {
	Tools    []protocol.ToolSchema
	Messages []launcherMCPMessage
}

func launcherMCPNames(tools []protocol.ToolSchema) []protocol.ToolName {
	names := []protocol.ToolName{}
	for _, tool := range tools {
		if tool.Name == protocol.ToolConnectMCP || protocol.IsMCPToolName(tool.Name) {
			names = append(names, tool.Name)
		}
	}
	return names
}

type launcherMCPMessage struct{ Content jsonvalue.Value }

func launcherMCPResults(messages []launcherMCPMessage) []string {
	results := []string{}
	for _, message := range messages {
		blocks, _ := message.Content.Array()
		for _, block := range blocks {
			kind, _ := block.Lookup("type")
			label, _ := kind.Text()
			if label == "tool_result" {
				content, _ := block.Lookup("content")
				text, _ := content.Text()
				results = append(results, text)
			}
		}
	}
	return results
}

func writeMCPModelReply(w http.ResponseWriter, toolCall, echo bool) {
	w.Header().Set("Content-Type", "application/json")
	if toolCall {
		blocks := `{"type":"tool_use","id":"connect","name":"connect_mcp","input":{"name":"friendly"}}`
		if echo {
			blocks += `,{"type":"tool_use","id":"echo","name":"mcp__raw_name__echo","input":{}}`
		}
		fmt.Fprintf(w, `{"id":"msg","type":"message","role":"assistant","model":"configured","content":[%s],"stop_reason":"tool_use","usage":{"input_tokens":1,"output_tokens":1}}`, blocks)
	} else {
		io.WriteString(w, `{"id":"final","type":"message","role":"assistant","model":"configured","content":[{"type":"text","text":"done"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
	}
}

func TestMCPLauncherMatchesSourceHTTPComposition(t *testing.T) {
	var fixture struct {
		Rows []struct {
			Enabled      bool                                   `json:"enabled"`
			Configured   bool                                   `json:"configured"`
			Output       string                                 `json:"output"`
			RequestNames [][]protocol.ToolName                  `json:"request_mcp_names"`
			Results      []string                               `json:"results"`
			Counters     struct{ Listed, Called, Closed int32 } `json:"counters"`
		} `json:"rows"`
	}
	data, err := os.ReadFile("../testdata/python-mcp-launcher.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for index, row := range fixture.Rows {
		t.Run(fmt.Sprint(index), func(t *testing.T) {
			var calls atomic.Int32
			requests := make(chan launcherMCPRequest, 2)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request launcherMCPRequest
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
				}
				requests <- request
				writeMCPModelReply(w, calls.Add(1) == 1 && row.Enabled, row.Configured)
			}))
			defer upstream.Close()
			counts := &launcherMCPCounts{}
			options := Options{MCPTools: row.Enabled}
			if row.Configured {
				connection, err := agent.NewMCPConnection(&launcherMCPPeer{counts})
				if err != nil {
					t.Fatal(err)
				}
				options.MCPServers = []agent.ManagedMCPServer{{Alias: "friendly", Connection: connection}}
			}
			settings := settingsFor(t, map[string]string{"MINILOOP_FAKE_LLM": "0", "ANTHROPIC_API_KEY": "fixture-key", "ANTHROPIC_BASE_URL": upstream.URL, "MODEL_ID": "configured"})
			app, err := NewWithOptions(context.Background(), settings, config.ServerSettings{Host: "127.0.0.1"}, nil, options)
			if err != nil {
				t.Fatal(err)
			}
			defer stopApp(t, app)
			// The manager detaches trusted option entries before the listener is served.
			if len(options.MCPServers) > 0 {
				options.MCPServers[0].Alias = "changed"
			}
			base, cancel, done := startApp(t, app)
			client := &http.Client{Timeout: 5 * time.Second}
			defer client.CloseIdleConnections()
			status, data := exchange(t, client, "POST", base+"/sessions", `{"mode":"auto"}`, "")
			if status != 200 {
				t.Fatal(status, string(data))
			}
			info := decode[httpapi.SessionInfo](t, data)
			status, data = exchange(t, client, "POST", base+"/sessions/"+string(info.ID)+"/messages", `{"message":"connect"}`, "")
			if answer := decode[httpapi.MessageResponse](t, data); status != 200 || answer.Final != row.Output {
				t.Fatal(status, string(data))
			}
			names := [][]protocol.ToolName{}
			results := []string{}
			for range row.RequestNames {
				request := <-requests
				names = append(names, launcherMCPNames(request.Tools))
				results = append(results, launcherMCPResults(request.Messages)...)
			}
			if !reflect.DeepEqual(names, row.RequestNames) || !reflect.DeepEqual(results, row.Results) {
				t.Fatalf("requests/results: %v %v", names, results)
			}
			cancel()
			stopped(t, done)
			if counts.Listed.Load() != row.Counters.Listed || counts.Called.Load() != row.Counters.Called || counts.Closed.Load() != row.Counters.Closed {
				t.Fatalf("counts: %d %d %d", counts.Listed.Load(), counts.Called.Load(), counts.Closed.Load())
			}
		})
	}
}

// A real native subprocess exercises launcher -> manager -> connect_mcp -> stdio
// discovery/call and last-holder shutdown, without invoking Python.
func TestMCPLauncherChild(t *testing.T) {
	marker := ""
	for index, arg := range os.Args {
		if arg == "--miniloop-mcp-child" && index+1 < len(os.Args) {
			marker = os.Args[index+1]
		}
	}
	if marker == "" {
		return
	}
	if err := os.WriteFile(marker, []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
		os.Exit(2)
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var request struct {
			ID     jsonvalue.Value `json:"id"`
			Method string          `json:"method"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &request); err != nil {
			os.Exit(3)
		}
		result := "{}"
		switch request.Method {
		case "notifications/initialized":
			continue
		case "tools/list":
			result = `{"tools":[{"name":"echo","description":"echo","inputSchema":{"type":"object"}}]}`
		case "tools/call":
			content, err := json.Marshal(struct {
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			}{Content: []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}{{"text", fmt.Sprintf("pid=%d; scrubbed=%t", os.Getpid(), os.Getenv("ANTHROPIC_API_KEY") == "")}}})
			if err != nil {
				os.Exit(4)
			}
			result = string(content)
		}
		value, err := jsonvalue.Decode(result)
		if err != nil {
			os.Exit(5)
		}
		if err := json.NewEncoder(os.Stdout).Encode(struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      jsonvalue.Value `json:"id"`
			Result  jsonvalue.Value `json:"result"`
		}{"2.0", request.ID, value}); err != nil {
			os.Exit(6)
		}
	}
	os.Exit(0)
}

func TestMCPLauncherOwnsNativeStdioChildUntilShutdown(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "fixture-parent-secret")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	marker := t.TempDir() + "/child-pid"
	registry := secrets.New(secrets.Config{})
	registry.RegisterValue("ANTHROPIC_API_KEY", "fixture-parent-secret")
	native, err := mcp.NewStdio(mcp.StdioConfig{Name: "raw.name", Secrets: registry, Command: []string{executable, "-test.run=^TestMCPLauncherChild$", "--", "--miniloop-mcp-child", marker}})
	if err != nil {
		t.Fatal(err)
	}
	defer native.Close()
	connection, err := agent.NewMCPConnection(native)
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	captured := make(chan launcherMCPRequest, 2)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request launcherMCPRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		captured <- request
		writeMCPModelReply(w, calls.Add(1) == 1, true)
	}))
	defer upstream.Close()
	settings := settingsFor(t, map[string]string{"MINILOOP_FAKE_LLM": "0", "ANTHROPIC_API_KEY": "fixture-key", "ANTHROPIC_BASE_URL": upstream.URL, "MODEL_ID": "configured"})
	app, err := NewWithOptions(context.Background(), settings, config.ServerSettings{Host: "127.0.0.1"}, nil, Options{MCPTools: true, MCPServers: []agent.ManagedMCPServer{{Alias: "friendly", Connection: connection}}})
	if err != nil {
		t.Fatal(err)
	}
	defer stopApp(t, app)
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("MCP process started before authorized connection", err)
	}
	session, err := app.manager.Create(context.Background(), agent.CreateSessionRequest{Owner: "test", PermissionMode: agent.ModeAuto})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := session.Run(ctx, "connect")
	if err != nil || result != "done" {
		t.Fatal(result, err)
	}
	<-captured
	request := <-captured
	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("pid=%d; scrubbed=true", pid)
	results := launcherMCPResults(request.Messages)
	if len(results) != 2 || results[1] != want {
		t.Fatal("native tool result/credential scrub mismatch", results)
	}
	if err := syscall.Kill(pid, 0); err != nil {
		t.Fatal("child did not remain live before app stop", err)
	}
	if err := app.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatal("native child survived app shutdown", err)
	}
}

func TestMCPLauncherInspectionStaysPureAndFullBundleUnsupported(t *testing.T) {
	settings := settingsFor(t, nil)
	var factoryCalls atomic.Int32
	options := Options{MCPTools: true, MCPServers: []agent.ManagedMCPServer{{Alias: "do-not-expose", Factory: func(context.Context) (*agent.MCPConnection, error) {
		factoryCalls.Add(1)
		return nil, errors.New("unused")
	}}}}
	report := InspectWithOptions(settings, config.ServerSettings{Host: "127.0.0.1"}, nil, options)
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if !report.MCPTools || factoryCalls.Load() != 0 || strings.Contains(string(data), "do-not-expose") {
		t.Fatal("inspection exposed configuration or invoked factory")
	}
	if _, err := os.Stat(settings.WorkspaceRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("inspect allocated workspace")
	}
	settings.EnableFeatures = true
	if _, err := NewWithOptions(context.Background(), settings, config.ServerSettings{Host: "127.0.0.1"}, nil, options); err == nil || !strings.Contains(err.Error(), "MINILOOP_FEATURES") {
		t.Fatal("MCP flag bypassed full bundle refusal", err)
	}
	if factoryCalls.Load() != 0 {
		t.Fatal("unsupported startup invoked factory")
	}
}
