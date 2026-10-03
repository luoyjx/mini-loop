package provider_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/provider"
)

func TestRealHTTPAdapterRunsToolsThroughExistingGateAndRecordsServedAlias(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tenant/v1/messages" || r.Header.Get("x-api-key") != "fixture-key" || r.Header.Get("anthropic-version") != provider.APIVersion {
			t.Error("wrong HTTP boundary", r.URL, r.Header)
		}
		var input struct {
			Model    string
			Messages []struct{ Content json.RawMessage }
			Tools    []protocol.ToolSchema
			System   json.RawMessage
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Error(err)
			return
		}
		if input.Model != "requested-alias" || len(input.Tools) != 10 || len(input.System) == 0 {
			t.Error("typed request missing model/catalogue/system")
		}
		reply := protocol.ModelReply{ID: "response", Type: protocol.ReplyMessage, Role: protocol.RoleAssistant, Model: "actually-served", Usage: protocol.TokenUsage{InputTokens: 17, OutputTokens: 3}}
		if calls.Add(1) == 1 {
			reply.StopReason = protocol.StopToolUse
			reply.Content = []protocol.Block{protocol.NewToolUseWithoutCaller("w1", protocol.WriteFileToolInput(protocol.WriteFileInput{Path: "artifact.txt", Content: "landed"}))}
		} else {
			paired := false
			for _, message := range input.Messages {
				var blocks []struct {
					Type    protocol.BlockKind
					ID      string `json:"tool_use_id"`
					Content string
				}
				if json.Unmarshal(message.Content, &blocks) == nil {
					for _, block := range blocks {
						if block.Type == protocol.BlockToolResult && block.ID == "w1" && block.Content != "" {
							paired = true
						}
					}
				}
			}
			if !paired {
				t.Error("second model request has no paired tool result")
			}
			reply.StopReason = protocol.StopEndTurn
			reply.Content = []protocol.Block{protocol.NewTextBlock("done")}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(reply)
	}))
	defer server.Close()
	client, err := provider.New(provider.Config{BaseURL: server.URL + "/tenant/", APIKey: "fixture-key"})
	if err != nil {
		t.Fatal(err)
	}
	manager, err := agent.NewSessionManager(agent.ManagerConfig{WorkspaceRoot: filepath.Join(t.TempDir(), "root"), Services: agent.ManagerServices{Provider: client}, Defaults: agent.SessionDefaults{Model: "requested-alias"}})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Stop(context.Background())
	session, err := manager.Create(context.Background(), agent.CreateSessionRequest{Owner: "alice", PermissionMode: agent.ModeAuto})
	if err != nil {
		t.Fatal(err)
	}
	final, err := session.Run(context.Background(), "write artifact")
	if err != nil || final != "done" {
		t.Fatal(final, err)
	}
	data, err := os.ReadFile(filepath.Join(session.Info().Workspace, "artifact.txt"))
	if err != nil || string(data) != "landed" {
		t.Fatal("effect did not land", err)
	}
	if calls.Load() != 2 {
		t.Fatal(calls.Load())
	}
	count := 0
	for _, record := range session.Events() {
		if end, ok := record.Event.ModelEnd(); ok {
			count++
			if end.ServedModel == nil || *end.ServedModel != "actually-served" || end.Usage == nil || end.Usage.InputTokens != 17 {
				t.Fatal("served identity/usage lost", end)
			}
		}
	}
	if count != 2 {
		t.Fatal("model end events missing")
	}
	if err := protocol.ValidateTranscript(session.Messages()); err != nil {
		t.Fatal(err)
	}
}
