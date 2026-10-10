package launcher

import (
	"context"
	"encoding/json"
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
	"github.com/luoyjx/mini-loop/go/httpapi"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/sandbox"
	"github.com/luoyjx/mini-loop/go/shell"
)

func TestLauncherSandboxHTTPExecutionAndConfiguredDeadline(t *testing.T) {
	for _, confined := range []bool{false, true} {
		name := "unavailable"
		if confined {
			name = "seatbelt"
		}
		t.Run(name, func(t *testing.T) {
			if confined && !sandbox.Available() {
				t.Skip(*sandbox.UnavailableReason())
			}
			root := t.TempDir()
			t.Setenv("HOME", root)
			credential := filepath.Join(root, ".ssh", "canary")
			if err := os.Mkdir(filepath.Dir(credential), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(credential, []byte("PRIVATE-CANARY"), 0600); err != nil {
				t.Fatal(err)
			}
			var backend shell.Sandbox = sandbox.NewUnavailable("fixture platform unavailable")
			if confined {
				var err error
				backend, err = sandbox.NewDefault(filepath.Join(root, "template"), sandbox.DefaultOptions{Require: true})
				if err != nil {
					t.Fatal(err)
				}
			}
			outside := filepath.Join(root, "outside")
			quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
			command := "printf inside > proof; if cat " + quote(credential) + "; then printf READ-ALLOWED; else printf READ-BLOCKED; fi; if printf outside > " + quote(outside) + "; then printf WRITE-ALLOWED; else printf WRITE-BLOCKED; fi; sleep 10"
			captured := make(chan []string, 2)
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				var input launcherMCPRequest
				if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
					t.Error(err)
					http.Error(writer, "decode failed", 400)
					return
				}
				captured <- launcherMCPResults(input.Messages)
				reply := protocol.ModelReply{ID: "reply", Type: protocol.ReplyMessage, Role: protocol.RoleAssistant, Model: "configured", Content: []protocol.Block{protocol.NewTextBlock("done")}, StopReason: protocol.StopEndTurn, Usage: protocol.TokenUsage{InputTokens: protocol.IntegerUsageCount(5), OutputTokens: protocol.IntegerUsageCount(3)}}
				if calls.Add(1) == 1 {
					reply.Content = []protocol.Block{protocol.NewBashUse("sandbox", command)}
					reply.StopReason = protocol.StopToolUse
				}
				writer.Header().Set("Content-Type", "application/json")
				if err := json.NewEncoder(writer).Encode(reply); err != nil {
					t.Error(err)
				}
			}))
			defer upstream.Close()
			settings := settingsFor(t, map[string]string{"MINILOOP_FAKE_LLM": "0", "ANTHROPIC_API_KEY": "fixture-key", "ANTHROPIC_BASE_URL": upstream.URL, "MODEL_ID": "configured", "MINILOOP_BASH_TIMEOUT": "1", "MINILOOP_WORKSPACE_ROOT": filepath.Join(root, "workspaces")})
			options := Options{Sandbox: backend}
			report := InspectWithOptions(settings, config.ServerSettings{Host: "127.0.0.1"}, nil, options)
			expected := "UnavailableSandbox"
			if confined {
				expected = "SeatbeltSandbox"
			}
			if report.Sandbox != expected || (report.SandboxReason != nil) == confined {
				t.Fatal(report)
			}
			if _, err := os.Stat(settings.WorkspaceRoot); !os.IsNotExist(err) {
				t.Fatal("inspection allocated workspace", err)
			}
			app, err := NewWithOptions(context.Background(), settings, config.ServerSettings{Host: "127.0.0.1"}, nil, options)
			if err != nil {
				t.Fatal(err)
			}
			defer stopApp(t, app)
			base, cancel, done := startApp(t, app)
			defer cancel()
			client := &http.Client{Timeout: 5 * time.Second}
			defer client.CloseIdleConnections()
			checkHealth := func() {
				t.Helper()
				code, body := exchange(t, client, "GET", base+"/healthz", "", "")
				var health struct {
					Posture agent.RuntimePosture `json:"posture"`
				}
				if err := json.Unmarshal(body, &health); err != nil {
					t.Fatal(err)
				}
				if code != 200 || health.Posture.Sandbox != expected || (health.Posture.SandboxReason != nil) == confined {
					t.Fatal(code, string(body))
				}
			}
			checkHealth()
			code, body := exchange(t, client, "POST", base+"/sessions", `{"mode":"auto"}`, "")
			if code != 200 {
				t.Fatal(code, string(body))
			}
			info := decode[httpapi.SessionInfo](t, body)
			code, body = exchange(t, client, "POST", base+"/sessions/"+string(info.ID)+"/messages", `{"message":"verify sandbox"}`, "")
			if code != 200 || decode[httpapi.MessageResponse](t, body).Final != "done" {
				t.Fatal(code, string(body))
			}
			<-captured
			results := <-captured
			if len(results) != 1 || !strings.Contains(results[0], "Error: Timeout (1s)") {
				t.Fatal("configured deadline lost", results)
			}
			if confined {
				if !strings.Contains(results[0], "READ-BLOCKED") || !strings.Contains(results[0], "WRITE-BLOCKED") || strings.Contains(results[0], "PRIVATE-CANARY") {
					t.Fatal(results)
				}
				if _, err := os.Stat(outside); !os.IsNotExist(err) {
					t.Fatal("outside write escaped", err)
				}
			} else {
				if !strings.Contains(results[0], "READ-ALLOWED") || !strings.Contains(results[0], "WRITE-ALLOWED") {
					t.Fatal("unavailable backend ceased host execution", results)
				}
				if data, err := os.ReadFile(outside); err != nil || string(data) != "outside" {
					t.Fatal(err)
				}
			}
			if data, err := os.ReadFile(filepath.Join(info.Workspace, "proof")); err != nil || string(data) != "inside" {
				t.Fatal("inside write failed", err)
			}
			checkHealth()
			cancel()
			stopped(t, done)
		})
	}
}

func TestLauncherSandboxInspectionKeepsHostDefaultAndDetachedReasons(t *testing.T) {
	settings := settingsFor(t, nil)
	server := config.ServerSettings{Host: "127.0.0.1"}
	if report := Inspect(settings, server, nil); report.Sandbox != "none" || report.SandboxReason != nil {
		t.Fatal(report)
	}
	options := Options{Sandbox: sandbox.NewUnavailable("reason")}
	report := InspectWithOptions(settings, server, nil, options)
	*report.SandboxReason = "changed"
	if *InspectWithOptions(settings, server, nil, options).SandboxReason != "reason" {
		t.Fatal("inspection aliases reason")
	}
}
