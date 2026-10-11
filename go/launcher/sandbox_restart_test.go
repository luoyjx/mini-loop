package launcher

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/config"
	"github.com/luoyjx/mini-loop/go/httpapi"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/sandbox"
)

func TestLauncherSQLiteRestoreRebindsCurrentSeatbeltPolicy(t *testing.T) {
	if !sandbox.Available() {
		t.Skip(*sandbox.UnavailableReason())
	}
	ctx := context.Background()
	root := t.TempDir()
	oldExtra, newExtra := filepath.Join(root, "old-extra"), filepath.Join(root, "new-extra")
	credential := filepath.Join(root, ".ssh", "canary")
	for _, path := range []string{oldExtra, newExtra, filepath.Dir(credential)} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(credential, []byte("PRIVATE-RESTORE-CANARY"), 0600); err != nil {
		t.Fatal(err)
	}
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
	var tool atomic.Pointer[protocol.Block]
	var calls atomic.Int32
	requests := make(chan []string, 4)
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var input launcherMCPRequest
		if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
			t.Error(err)
			http.Error(writer, "bad input", 400)
			return
		}
		requests <- launcherMCPResults(input.Messages)
		reply := protocol.ModelReply{ID: "reply", Type: protocol.ReplyMessage, Role: protocol.RoleAssistant, Model: "configured", Content: []protocol.Block{protocol.NewTextBlock("done")}, StopReason: protocol.StopEndTurn, Usage: protocol.TokenUsage{InputTokens: protocol.IntegerUsageCount(5), OutputTokens: protocol.IntegerUsageCount(3)}}
		if calls.Add(1)%2 == 1 {
			reply.Content = []protocol.Block{*tool.Load()}
			reply.StopReason = protocol.StopToolUse
		}
		writer.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(writer).Encode(reply); err != nil {
			t.Error(err)
		}
	}))
	defer upstream.Close()
	settings := settingsFor(t, map[string]string{"MINILOOP_FAKE_LLM": "0", "ANTHROPIC_API_KEY": "fixture-key", "ANTHROPIC_BASE_URL": upstream.URL, "MODEL_ID": "configured", "MINILOOP_WORKSPACE_ROOT": filepath.Join(root, "workspaces")})
	auth, err := httpapi.AuthFromEnvironment(map[string]string{"MINILOOP_API_TOKENS": "alice:alice-token,bob:bob-token"})
	if err != nil {
		t.Fatal(err)
	}
	server := config.ServerSettings{Host: "127.0.0.1"}
	client := &http.Client{Timeout: 5 * time.Second}
	defer client.CloseIdleConnections()
	database := filepath.Join(root, "state.db")
	var info httpapi.SessionInfo
	var previous agent.TranscriptSnapshot
	for generation, extra := range []string{oldExtra, newExtra} {
		store := launcherState(t, database)
		backend, err := sandbox.NewSeatbelt(sandbox.Config{WritableRoots: []string{filepath.Join(root, "template")}, UnreadableRoots: []string{filepath.Dir(credential)}, ExtraWritable: []string{extra}})
		if err != nil {
			t.Fatal(err)
		}
		app, err := NewWithOptions(ctx, settings, server, auth, Options{StateStore: store, Sandbox: backend})
		if err != nil {
			t.Fatal(err)
		}
		// Register ownership cleanup even if a mid-scenario assertion fails.
		defer stopApp(t, app)
		base, cancel, done := startApp(t, app)
		defer cancel()
		if generation == 0 {
			code, body := exchange(t, client, "POST", base+"/sessions", `{"mode":"auto"}`, "alice-token")
			if code != 200 {
				t.Fatal(code, string(body))
			}
			info = decode[httpapi.SessionInfo](t, body)
		} else {
			endpoint := base + "/sessions/" + string(info.ID)
			if code, _ := exchange(t, client, "GET", endpoint, "", "alice-token"); code != 404 {
				t.Fatal("constructor restored implicitly", code)
			}
			restored, err := app.RestoreSessions(ctx)
			if err != nil || len(restored) != 1 || restored[0].ID() != info.ID || restored[0].Info().Workspace != info.Workspace {
				t.Fatal("restore binding", restored, err)
			}
			if code, _ := exchange(t, client, "GET", endpoint, "", "bob-token"); code != 404 {
				t.Fatal("restore widened ownership", code)
			}
			code, body := exchange(t, client, "GET", endpoint+"/transcript", "", "alice-token")
			if code != 200 || !reflect.DeepEqual(decode[agent.TranscriptSnapshot](t, body).Messages, previous.Messages) {
				t.Fatal("restore changed transcript", code, string(body))
			}
			if code, body := exchange(t, client, "POST", endpoint+"/mode", `{"mode":"auto"}`, "alice-token"); code != 200 {
				t.Fatal(code, string(body))
			}
		}
		name := "first-proof"
		if generation == 1 {
			name = "restored-proof"
		}
		outside := filepath.Join(root, "outside-"+name)
		command := "printf inside > " + name + " && printf current > " + quote(filepath.Join(extra, name)) + "; if cat " + quote(credential) + "; then printf LEAKED; else printf READ-BLOCKED; fi; if printf escaped > " + quote(outside) + "; then printf WIDENED; else printf WRITE-BLOCKED; fi"
		if generation == 1 {
			command += "; if printf stale > " + quote(filepath.Join(oldExtra, "stale")) + "; then printf STALE-ALLOWED; else printf STALE-BLOCKED; fi"
		}
		block := protocol.NewBashUse(name, command)
		tool.Store(&block)
		endpoint := base + "/sessions/" + string(info.ID)
		code, body := exchange(t, client, "POST", endpoint+"/messages", `{"message":"verify current sandbox"}`, "alice-token")
		if code != 200 || decode[httpapi.MessageResponse](t, body).Final != "done" {
			t.Fatal(code, string(body))
		}
		<-requests
		results := <-requests
		if len(results) == 0 {
			t.Fatal("model received no effect result")
		}
		result := results[len(results)-1]
		if !strings.Contains(result, "READ-BLOCKED") || !strings.Contains(result, "WRITE-BLOCKED") || strings.Contains(result, "PRIVATE-RESTORE-CANARY") {
			t.Fatal("restored sandbox not enforced", result)
		}
		if generation == 1 && !strings.Contains(result, "STALE-BLOCKED") {
			t.Fatal("old process policy was retained", result)
		}
		for _, denied := range []string{outside, filepath.Join(oldExtra, "stale")} {
			if _, err := os.Stat(denied); !os.IsNotExist(err) {
				t.Fatal("forbidden effect exists", denied, err)
			}
		}
		for _, allowed := range []string{filepath.Join(info.Workspace, name), filepath.Join(extra, name)} {
			if _, err := os.Stat(allowed); err != nil {
				t.Fatal("allowed write missing", allowed, err)
			}
		}
		code, body = exchange(t, client, "GET", base+"/healthz", "", "")
		var health struct {
			Posture agent.RuntimePosture `json:"posture"`
		}
		if err := json.Unmarshal(body, &health); err != nil {
			t.Fatal(err)
		}
		if code != 200 || health.Posture.Sandbox != "SeatbeltSandbox" || health.Posture.StateStoreError != nil {
			t.Fatal("restored posture", code, string(body))
		}
		code, body = exchange(t, client, "GET", endpoint+"/transcript", "", "alice-token")
		if code != 200 {
			t.Fatal(code, string(body))
		}
		previous = decode[agent.TranscriptSnapshot](t, body)
		cancel()
		stopped(t, done)
		if holder, held, err := store.LeaseHolder(ctx, info.ID); err != nil || held || holder != "" {
			t.Fatal("shutdown retained lease", holder, held, err)
		}
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
	}
	// A third actual database open retains both admitted turns and their identity.
	store := launcherState(t, database)
	rows, err := store.LoadSessions(ctx)
	if err != nil || len(rows) != 1 || rows[0].SessionID != info.ID || rows[0].RunCount != 2 {
		t.Fatal("reopen lost resumed turn", rows, err)
	}
}
