package launcher

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/config"
	"github.com/luoyjx/mini-loop/go/httpapi"
	"github.com/luoyjx/mini-loop/go/protocol"
)

func TestSelfAuditLauncherSelectsVisibilityFromAuthentication(t *testing.T) {
	for _, authenticated := range []bool{false, true} {
		t.Run(fmt.Sprint(authenticated), func(t *testing.T) {
			var count atomic.Int32
			results := make(chan string, 1)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request struct {
					Tools    []struct{ Name protocol.ToolName }
					Messages []struct{ Content json.RawMessage }
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
				}
				found := false
				for _, tool := range request.Tools {
					if tool.Name == protocol.ToolSelfAudit {
						found = true
					}
				}
				if !found {
					t.Error("optional schema missing")
				}
				content, reason := `[{"type":"text","text":"done"}]`, "end_turn"
				if count.Add(1) == 1 {
					content, reason = `[{"type":"tool_use","id":"audit","name":"self_audit","input":{}}]`, "tool_use"
				} else {
					for _, message := range request.Messages {
						var blocks []struct {
							Type    string
							Content string
						}
						if err := json.Unmarshal(message.Content, &blocks); err != nil {
							continue
						}
						for _, block := range blocks {
							if block.Type == "tool_result" {
								results <- block.Content
							}
						}
					}
				}
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{"id":"audit","type":"message","role":"assistant","model":"configured","content":%s,"stop_reason":%q,"usage":{"input_tokens":5,"output_tokens":3}}`, content, reason)
			}))
			defer upstream.Close()
			var auth httpapi.Authenticator = httpapi.NullAuth{}
			token := ""
			if authenticated {
				var err error
				auth, err = httpapi.NewTokenAuth([]httpapi.TokenBinding{{Token: "alice-token", Principal: "alice"}})
				if err != nil {
					t.Fatal(err)
				}
				token = "alice-token"
			}
			settings := settingsFor(t, map[string]string{"MINILOOP_FAKE_LLM": "0", "ANTHROPIC_API_KEY": "test-key", "ANTHROPIC_BASE_URL": upstream.URL, "MINILOOP_WORKSPACE_ROOT": t.TempDir()})
			app, err := NewWithOptions(context.Background(), settings, config.ServerSettings{Host: "127.0.0.1"}, auth, Options{SelfAuditTools: true})
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := app.Stop(context.Background()); err != nil {
					t.Error(err)
				}
			}()
			if _, err := app.manager.Create(context.Background(), agent.CreateSessionRequest{Owner: "bob"}); err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(app.Handler())
			defer server.Close()
			client := &http.Client{Timeout: 5 * time.Second}
			defer client.CloseIdleConnections()
			code, body := exchange(t, client, "POST", server.URL+"/sessions", `{"permission_mode":"readonly"}`, token)
			if code != 200 {
				t.Fatal(code, string(body))
			}
			info := decode[httpapi.SessionInfo](t, body)
			code, body = exchange(t, client, "POST", server.URL+"/sessions/"+string(info.ID)+"/messages", `{"message":"audit"}`, token)
			if code != 200 || decode[httpapi.MessageResponse](t, body).Final != "done" {
				t.Fatal(code, string(body))
			}
			select {
			case report := <-results:
				want := "## sessions (2)"
				if authenticated {
					want = "## sessions (1)"
				}
				if !strings.Contains(report, want) || strings.Contains(report, "## cron") == authenticated {
					t.Fatal("launcher scope differs", report)
				}
			case <-time.After(time.Second):
				t.Fatal("tool result missing")
			}
			if Inspect(settings, config.ServerSettings{}, auth).SelfAuditTools {
				t.Fatal("default selection enabled")
			}
		})
	}
}
