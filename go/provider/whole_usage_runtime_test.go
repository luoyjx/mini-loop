package provider_test

import (
	"context"
	"encoding/json"
	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/provider"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWholeUsageActualHTTPManagedModelEnd(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-provider-whole-usage.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name, Mode, Outcome, SSE string
			Error                    struct{ Class, Message string }
			Response                 string          `json:"response_json"`
			Expected                 jsonvalue.Value `json:"event_usage"`
			Consumer                 struct {
				Prompt *int `json:"prompt_tokens"`
			}
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 86 {
		t.Fatal("Source inventory changed")
	}
	for _, row := range fixture.Cases {
		t.Run(row.Name+"/"+row.Mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if row.Mode == "stream" {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = w.Write([]byte(row.SSE))
				} else {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(row.Response))
				}
			}))
			defer server.Close()
			cfg := provider.Config{BaseURL: server.URL, APIKey: "fixture-key"}
			var services agent.ManagerServices
			if row.Mode == "stream" {
				client, err := provider.NewStreaming(cfg)
				if err != nil {
					t.Fatal(err)
				}
				services.Provider = client
			} else {
				client, err := provider.New(cfg)
				if err != nil {
					t.Fatal(err)
				}
				services.Provider = client
			}
			manager, err := agent.NewSessionManager(agent.ManagerConfig{WorkspaceRoot: filepath.Join(t.TempDir(), "sessions"), Services: services, Defaults: agent.SessionDefaults{Model: "fixture-model"}})
			if err != nil {
				t.Fatal(err)
			}
			defer manager.Stop(context.Background())
			session, err := manager.Create(context.Background(), agent.CreateSessionRequest{Owner: "alice", PermissionMode: agent.ModeAuto})
			if err != nil {
				t.Fatal(err)
			}
			_, err = session.Run(context.Background(), "hello")
			if err != nil {
				t.Fatal("managed terminal outcome differs", err)
			}
			ends := 0
			for _, record := range session.Events() {
				if end, ok := record.Event.ModelEnd(); ok {
					ends++
					if row.Outcome == "error" {
						if end.Status != agent.ModelError || end.Error == nil || !strings.Contains(*end.Error, row.Error.Message) {
							t.Fatal("failed stream reported success")
						}
						continue
					}
					encoded, err := json.Marshal(end.Usage)
					if err != nil {
						t.Fatal(err)
					}
					actual, err := jsonvalue.Decode(string(encoded))
					if err != nil {
						t.Fatal(err)
					}
					a, _ := actual.Sorted().MarshalJSON()
					b, _ := row.Expected.Sorted().MarshalJSON()
					if string(a) != string(b) {
						t.Fatal("managed event projection differs", string(a), string(b))
					}
					if (end.PromptTokens != nil) != (row.Consumer.Prompt != nil) || (end.PromptTokens != nil && *end.PromptTokens != *row.Consumer.Prompt) {
						t.Fatal("managed prompt differs")
					}
				}
			}
			if ends != 1 {
				t.Fatal("unexpected model end count", ends)
			}
		})
	}
}
