package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/protocol"
)

type permissionHTTPEvent struct {
	Type      agent.SessionEventKind        `json:"type"`
	Sequence  agent.EventSequence           `json:"seq"`
	Session   agent.SessionID               `json:"session"`
	Agent     string                        `json:"agent"`
	Depth     int                           `json:"depth"`
	MessageID agent.MessageID               `json:"message_id"`
	Decision  agent.PermissionEventDecision `json:"decision"`
	Rule      string                        `json:"rule"`
	Tool      protocol.ToolName             `json:"tool"`
	Reason    string                        `json:"reason"`
	Denied    bool                          `json:"denied"`
}

func TestReadonlyPermissionEventCrossesActualHTTPSSE(t *testing.T) {
	model := &agent.FakeProvider{}
	bash := &countedBash{}
	manager, err := agent.NewSessionManager(agent.ManagerConfig{
		WorkspaceRoot: filepath.Join(t.TempDir(), "workspaces"),
		Services:      agent.ManagerServices{Provider: model, BashFactory: fixedBashFactory{bash}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := manager.Stop(ctx); err != nil {
			t.Error(err)
		}
	})
	server := httptest.NewServer(testServer(t, Config{Manager: manager, Auth: tokenAuth(t)}))
	defer server.Close()
	client := &http.Client{Timeout: 5 * time.Second}
	defer client.CloseIdleConnections()
	exchange := func(path, body, token string, status int) (http.Header, []byte) {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, server.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		if err != nil || response.StatusCode != status {
			t.Fatalf("HTTP %s: status=%d body=%s error=%v", path, response.StatusCode, data, err)
		}
		return response.Header, data
	}
	_, data := exchange("/sessions", `{"mode":"readonly"}`, "token-a", http.StatusOK)
	created := decode[SessionInfo](t, data)
	route := "/sessions/" + string(created.ID) + "/messages/stream"
	exchange(route, `{"message":"foreign"}`, "token-b", http.StatusNotFound)
	if model.Calls() != 0 || bash.calls.Load() != 0 {
		t.Fatal("foreign request reached the model or handler")
	}
	headers, data := exchange(route, `{"message":"safe readonly probe"}`, "token-a", http.StatusOK)
	if !strings.HasPrefix(headers.Get("Content-Type"), "text/event-stream") {
		t.Fatal("missing SSE content type", headers)
	}
	var toolUse, permission, toolResult permissionHTTPEvent
	counts := make(map[agent.SessionEventKind]int)
	for _, block := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n\n") {
		var id, eventName, payload string
		for _, line := range strings.Split(block, "\n") {
			key, value, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			value = strings.TrimPrefix(value, " ")
			switch key {
			case "id":
				id = value
			case "event":
				eventName = value
			case "data":
				payload = value
			}
		}
		if payload == "" {
			continue
		}
		var event permissionHTTPEvent
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			t.Fatal(err)
		}
		counts[event.Type]++
		if event.Type == agent.EventPermission {
			if eventName != "permission" || id != strconv.FormatUint(uint64(event.Sequence), 10) {
				t.Fatal("permission SSE envelope changed", eventName, id, event)
			}
			permission = event
		} else if event.Type == agent.EventToolUse {
			toolUse = event
		} else if event.Type == agent.EventToolResult {
			toolResult = event
		}
	}
	for _, kind := range []agent.SessionEventKind{agent.EventToolUse, agent.EventPermission, agent.EventToolResult, agent.EventDone} {
		if counts[kind] != 1 {
			t.Fatalf("expected one %s, got %d: %s", kind, counts[kind], data)
		}
	}
	if permission.Decision != agent.PermissionDeny || permission.Rule != "readonly-mode" || permission.Tool != protocol.ToolBash || permission.Reason != "risk=exec" {
		t.Fatal("permission payload changed", permission)
	}
	if permission.Session != created.ID || permission.Agent != "main" || permission.Depth != 0 || permission.MessageID == "" || permission.MessageID != toolUse.MessageID || permission.MessageID != toolResult.MessageID {
		t.Fatal("permission lost current turn identity", toolUse, permission, toolResult)
	}
	if !(toolUse.Sequence < permission.Sequence && permission.Sequence < toolResult.Sequence) || !toolResult.Denied {
		t.Fatal("permission was absent or published after its tool result", toolUse, permission, toolResult)
	}
	if model.Calls() != 2 || bash.calls.Load() != 0 {
		t.Fatal("readonly request executed a handler or did not complete", model.Calls(), bash.calls.Load())
	}
}
