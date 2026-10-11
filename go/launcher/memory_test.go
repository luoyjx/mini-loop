package launcher

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/config"
	"github.com/luoyjx/mini-loop/go/memory"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/userresources"
)

func TestLauncherMemoryRootsMatchActualSourceConstruction(t *testing.T) {
	raw, err := os.ReadFile("../testdata/python-launcher-memory.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name             string
			ConfiguredShared bool `json:"configured_shared"`
			OwnerLocal       bool `json:"owner_local"`
			Blocked          string
			Started          bool
			Backend          MemoryBackend
			SharedPath       string   `json:"shared_path"`
			MemoryTools      []string `json:"memory_tools"`
			SharedDirectory  bool     `json:"shared_directory"`
			UsersDirectory   bool     `json:"users_directory"`
			UsersMode        *uint32  `json:"users_mode"`
		}
	}
	if err := json.Unmarshal(raw, &fixture); err != nil || len(fixture.Cases) != 7 {
		t.Fatal("incomplete source constructor fixture", err)
	}
	for _, row := range fixture.Cases {
		t.Run(row.Name, func(t *testing.T) {
			base := t.TempDir()
			memoryRoot := filepath.Join(base, "workspaces", ".memory")
			env := map[string]string{"MINILOOP_WORKSPACE_ROOT": filepath.Join(base, "workspaces")}
			if row.ConfiguredShared {
				memoryRoot = filepath.Join(base, "memory")
				env["MINILOOP_MEMORY_ROOT"] = memoryRoot
			}
			users := filepath.Join(base, "users")
			if row.OwnerLocal {
				env["MINILOOP_USER_RESOURCES_ROOT"] = users
			}
			if row.Blocked != "" {
				blocked := users
				if row.Blocked == "memory" {
					blocked = memoryRoot
				}
				if err := os.WriteFile(blocked, []byte("regular file"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			settings := settingsFor(t, env)
			app, err := New(context.Background(), settings, config.ServerSettings{Host: "127.0.0.1"}, nil)
			if (err == nil) != row.Started {
				t.Fatal("source constructor outcome differs", err)
			}
			if app != nil {
				defer stopApp(t, app)
				if report := Inspect(settings, config.ServerSettings{Host: "127.0.0.1"}, nil); report.MemoryBackend != row.Backend || report.MemoryTools || !report.MemoryAuto || len(row.MemoryTools) != 0 {
					t.Fatal("source defaults differ", report)
				}
				if memoryRoot != filepath.Join(base, filepath.FromSlash(row.SharedPath)) {
					t.Fatal("source shared path differs", memoryRoot, row.SharedPath)
				}
			}
			for path, expected := range map[string]bool{memoryRoot: row.SharedDirectory, users: row.UsersDirectory} {
				info, err := os.Stat(path)
				if (err == nil && info.IsDir()) != expected {
					t.Fatal("source root effects differ", path, expected, err)
				}
			}
			if row.UsersMode != nil {
				info, err := os.Stat(users)
				if err != nil || uint32(info.Mode().Perm()) != *row.UsersMode {
					t.Fatal("owner root mode differs", err)
				}
			}
		})
	}
}

func TestLauncherMemoryWireUsesSelectedRootsAndExactOwners(t *testing.T) {
	for _, mode := range []string{"default-shared", "configured-shared", "owner-local"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			base := t.TempDir()
			env := map[string]string{"MINILOOP_WORKSPACE_ROOT": filepath.Join(base, "workspaces")}
			sharedRoot := filepath.Join(base, "workspaces", ".memory")
			if mode == "configured-shared" {
				sharedRoot = filepath.Join(base, "configured-memory")
				env["MINILOOP_MEMORY_ROOT"] = sharedRoot
			}
			usersRoot := filepath.Join(base, "users")
			if mode == "owner-local" {
				env["MINILOOP_USER_RESOURCES_ROOT"] = usersRoot
			}
			for _, owner := range []string{"alice", "bob", "anonymous"} {
				seedLauncherMemory(t, sharedRoot, owner, owner+" legacy fact")
				if mode == "owner-local" {
					key, err := userresources.OwnerDirectoryKey(owner)
					if err != nil {
						t.Fatal(err)
					}
					seedLauncherMemory(t, filepath.Join(usersRoot, string(key), "memory"), owner, owner+" local fact")
				}
			}
			var calls atomic.Int32
			captured := make(chan json.RawMessage, 6)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var wire struct{ Messages json.RawMessage }
				if err := json.NewDecoder(r.Body).Decode(&wire); err != nil {
					t.Error(err)
				}
				select {
				case captured <- wire.Messages:
				default:
					t.Error("unexpected extra model requests")
				}
				w.Header().Set("Content-Type", "application/json")
				if calls.Add(1)%2 == 1 {
					io.WriteString(w, `{"id":"tool","type":"message","role":"assistant","model":"test","content":[{"type":"tool_use","id":"recall","name":"recall","input":{}}],"stop_reason":"tool_use","usage":{"input_tokens":1,"output_tokens":1}}`)
				} else {
					io.WriteString(w, `{"id":"final","type":"message","role":"assistant","model":"test","content":[{"type":"text","text":"recalled"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
				}
			}))
			defer upstream.Close()
			env["MINILOOP_FAKE_LLM"], env["ANTHROPIC_API_KEY"], env["ANTHROPIC_BASE_URL"] = "0", "fixture-key", upstream.URL
			settings := settingsFor(t, env)
			auto := false
			app, err := NewWithOptions(ctx, settings, config.ServerSettings{Host: "127.0.0.1"}, nil, Options{MemoryTools: true, MemoryAuto: &auto})
			if err != nil {
				t.Fatal(err)
			}
			defer stopApp(t, app)
			auto = true // Startup snapshots the explicit override before session admission.
			for _, owner := range []string{"alice", "bob", "anonymous"} {
				session, err := app.manager.Create(ctx, agent.CreateSessionRequest{Owner: agent.OwnerID(owner), PermissionMode: agent.ModeAuto})
				if err != nil {
					t.Fatal(err)
				}
				turnCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
				result, err := session.Run(turnCtx, "recall my stored facts")
				cancel()
				if err != nil || result != "recalled" {
					t.Fatal(result, err)
				}
				<-captured
				messages := string(<-captured)
				suffix := " legacy fact"
				if mode == "owner-local" {
					suffix = " local fact"
				}
				if !strings.Contains(messages, owner+suffix) {
					t.Fatal("selected owner memory missing", messages)
				}
				for _, foreign := range []string{"alice", "bob", "anonymous"} {
					if foreign != owner && strings.Contains(messages, foreign+suffix) {
						t.Fatal("foreign memory served", messages)
					}
				}
				if mode == "owner-local" && strings.Contains(messages, "legacy fact") {
					t.Fatal("owner resolver lost precedence", messages)
				}
			}
			if calls.Load() != 6 {
				t.Fatal("auto override was not retained", calls.Load())
			}
		})
	}
}

func seedLauncherMemory(t *testing.T, root, owner, body string) {
	t.Helper()
	store, err := memory.NewStore(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	bound, err := memory.Bind(store, memory.OwnerID(owner))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bound.Write(context.Background(), memory.Input{Name: "private", Body: body, Type: memory.Project, Origin: memory.Explicit}); err != nil {
		t.Fatal(err)
	}
}

func TestLauncherMemoryDefaultToolsStayOffAndInspectionIsPure(t *testing.T) {
	base := t.TempDir()
	settings := settingsFor(t, map[string]string{"MINILOOP_WORKSPACE_ROOT": filepath.Join(base, "workspaces"), "MINILOOP_MEMORY_ROOT": filepath.Join(base, "memory"), "MINILOOP_USER_RESOURCES_ROOT": filepath.Join(base, "users")})
	auto := false
	report := InspectWithOptions(settings, config.ServerSettings{Host: "127.0.0.1"}, nil, Options{MemoryTools: true, MemoryAuto: &auto})
	if report.MemoryBackend != OwnerMemory || !report.MemoryTools || report.MemoryAuto || len(report.Unsupported) != 0 {
		t.Fatal(report)
	}
	for _, root := range []string{settings.WorkspaceRoot, *settings.MemoryRoot, *settings.UserResourcesRoot} {
		if _, err := os.Stat(root); !os.IsNotExist(err) {
			t.Fatal("inspection created root", root, err)
		}
	}
	if _, err := NewWithOptions(context.Background(), settings, config.ServerSettings{Host: "0.0.0.0"}, nil, Options{MemoryTools: true}); err == nil {
		t.Fatal("open bind admitted")
	}
	for _, root := range []string{settings.WorkspaceRoot, *settings.MemoryRoot, *settings.UserResourcesRoot} {
		if _, err := os.Stat(root); !os.IsNotExist(err) {
			t.Fatal("bind refusal created root", root, err)
		}
	}
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Tools []struct{ Name protocol.ToolName }
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		for _, tool := range request.Tools {
			if tool.Name == protocol.ToolRemember || tool.Name == protocol.ToolRecall {
				t.Error("memory tool enabled by default", tool.Name)
			}
		}
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"final","type":"message","role":"assistant","model":"test","content":[{"type":"text","text":"default"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	defer upstream.Close()
	settings = settingsFor(t, map[string]string{"MINILOOP_FAKE_LLM": "0", "ANTHROPIC_API_KEY": "fixture-key", "ANTHROPIC_BASE_URL": upstream.URL})
	app, err := New(context.Background(), settings, config.ServerSettings{Host: "127.0.0.1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer stopApp(t, app)
	session, err := app.manager.Create(context.Background(), agent.CreateSessionRequest{Owner: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	if result, err := session.Run(context.Background(), "default probe"); err != nil || result != "default" || calls.Load() != 1 {
		t.Fatal(result, err, calls.Load())
	}
}
