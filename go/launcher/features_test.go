package launcher

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/config"
	"github.com/luoyjx/mini-loop/go/httpapi"
	"github.com/luoyjx/mini-loop/go/protocol"
)

func sourceFullFeatureNames(t *testing.T) []protocol.ToolName {
	t.Helper()
	data, err := os.ReadFile("../testdata/python-feature-bundle.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Definitions []struct {
			ID   string            `json:"definition_id"`
			Name protocol.ToolName `json:"name"`
		} `json:"definitions"`
		Registries []struct {
			Name  string   `json:"name"`
			Tools []string `json:"tools"`
		} `json:"registries"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	definitions := make(map[string]protocol.ToolName)
	for _, d := range fixture.Definitions {
		definitions[d.ID] = d.Name
	}
	for _, row := range fixture.Registries {
		if row.Name == "full" {
			var names []protocol.ToolName
			for _, id := range row.Tools {
				names = append(names, definitions[id])
			}
			if len(names) != 48 {
				t.Fatal("incomplete Source inventory")
			}
			return names
		}
	}
	t.Fatal("missing full registry")
	return nil
}

func TestFullFeatureInspectionResolvesExplicitBackendWithoutEffects(t *testing.T) {
	settings := settingsFor(t, map[string]string{"MINILOOP_FEATURES": "1"})
	options := Options{DiagnosticsProvider: inspectionDiagnostics{}}
	report := InspectWithOptions(settings, config.ServerSettings{}, nil, options)
	if !report.FullFeatures || !report.TaskTools || !report.WorktreeTools || !report.TeamTools || !report.BackgroundTools || !report.CronTools || !report.MemoryTools || !report.GoalTools || !report.PlanModeTools || !report.SelfAuditTools || !report.MCPTools || !report.SessionQueryTools || !report.DiagnosticsTools || len(report.Unsupported) != 0 {
		t.Fatal("inspection disagrees with full selection", report)
	}
	if report.WorkflowTools || report.ASTTools || !report.Settings.Settings.EnableFeatures {
		t.Fatal("independent selections/caller settings changed")
	}
	if len(Inspect(settings, config.ServerSettings{}, nil).Unsupported) != 1 || len(settings.Unsupported()) != 1 {
		t.Fatal("missing parser/caller settings refusal lost")
	}
	unavailable := Inspect(settings, config.ServerSettings{}, nil)
	if unavailable.FullFeatures || unavailable.TaskTools || unavailable.WorktreeTools || unavailable.BackgroundTools || unavailable.TeamTools {
		t.Fatal("missing backend advertised available full feature groups")
	}
	if options.BackgroundTools || options.TeamTools || options.MemoryTools {
		t.Fatal("inspection mutated caller options")
	}
	if _, err := os.Stat(settings.WorkspaceRoot); !os.IsNotExist(err) {
		t.Fatal("inspection allocated workspace", err)
	}
}

func TestFullFeatureLauncherHTTPPublishesSourceCatalogueAndRepositoryBinding(t *testing.T) {
	for _, repository := range []bool{false, true} {
		t.Run(fmt.Sprint(repository), func(t *testing.T) {
			requests := make(chan launcherMCPRequest, 2)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request launcherMCPRequest
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
				}
				requests <- request
				content, reason := `[{"type":"text","text":"done"}]`, "end_turn"
				if len(launcherMCPResults(request.Messages)) == 0 {
					content = `[{"type":"tool_use","id":"worktrees","name":"list_worktrees","input":{}}]`
					reason = "tool_use"
				}
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{"id":"full-reply","type":"message","role":"assistant","model":"configured","content":%s,"stop_reason":%q,"usage":{"input_tokens":5,"output_tokens":3}}`, content, reason)
			}))
			defer upstream.Close()
			settings := settingsFor(t, map[string]string{"MINILOOP_FEATURES": "1", "MINILOOP_FAKE_LLM": "0", "ANTHROPIC_API_KEY": "fixture-key", "ANTHROPIC_BASE_URL": upstream.URL, "MODEL_ID": "configured"})
			if repository {
				root := t.TempDir()
				settings.RepoRoot = &root
			}
			provider := &launcherDiagnostics{}
			app, err := NewWithOptions(context.Background(), settings, config.ServerSettings{Host: "127.0.0.1"}, nil, Options{DiagnosticsProvider: provider})
			if err != nil {
				t.Fatal(err)
			}
			defer stopApp(t, app)
			server := httptest.NewServer(app.Handler())
			defer server.Close()
			code, health := exchange(t, server.Client(), "GET", server.URL+"/healthz", "", "")
			if code != 200 || !decode[httpapi.HealthResponse](t, health).Features {
				t.Fatal("full settings hidden by health response", code, string(health))
			}
			code, body := exchange(t, server.Client(), "POST", server.URL+"/sessions", `{"mode":"readonly"}`, "")
			if code != 200 {
				t.Fatal(code, string(body))
			}
			info := decode[httpapi.SessionInfo](t, body)
			code, body = exchange(t, server.Client(), "POST", server.URL+"/sessions/"+string(info.ID)+"/messages", `{"message":"list worktrees"}`, "")
			if code != 200 || decode[httpapi.MessageResponse](t, body).Final != "done" {
				t.Fatal(code, string(body))
			}
			want := sourceFullFeatureNames(t)
			for i := 0; i < 2; i++ {
				select {
				case request := <-requests:
					var names []protocol.ToolName
					for _, tool := range request.Tools {
						names = append(names, tool.Name)
					}
					if !slices.Equal(names, want) {
						t.Fatalf("HTTP catalogue=%v Source=%v", names, want)
					}
					if i == 1 {
						results := launcherMCPResults(request.Messages)
						if len(results) != 1 {
							t.Fatal(results)
						}
						if repository && results[0] != "(not a git repo)" {
							t.Fatal("RepoRoot did not retain Source list behavior", results)
						}
						if !repository && results[0] != "Error: worktree repository is not configured" {
							t.Fatal("missing repository behavior changed", results)
						}
					}
				case <-time.After(5 * time.Second):
					t.Fatal("missing full model request")
				}
			}
			if len(provider.calls) != 0 {
				t.Fatal("unselected diagnostic effect")
			}
		})
	}
}
