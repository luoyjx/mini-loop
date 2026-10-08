package launcher

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/config"
	"github.com/luoyjx/mini-loop/go/httpapi"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/workflows"
)

func TestWorkflowLauncherMatchesActualDefaultSourceLifespan(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-workflow-launcher.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Rows []struct {
			Name                           string
			Env                            map[string]string
			Authenticated, Enabled, Health bool
			Listing                        httpapi.WorkflowListResponse
			Tools                          []string
			Caps                           workflows.DefinitionCaps
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, row := range fixture.Rows {
		t.Run(row.Name, func(t *testing.T) {
			schemas := make(chan []protocol.ToolSchema, 1)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req struct{ Tools []protocol.ToolSchema }
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Error(err)
				}
				schemas <- req.Tools
				io.WriteString(w, `{"id":"done","type":"message","role":"assistant","model":"configured","content":[{"type":"text","text":"done"}],"stop_reason":"end_turn","usage":{"input_tokens":5,"output_tokens":3}}`)
			}))
			defer upstream.Close()
			env := map[string]string{"MINILOOP_FAKE_LLM": "0", "ANTHROPIC_API_KEY": "fixture-key", "ANTHROPIC_BASE_URL": upstream.URL, "MODEL_ID": "configured"}
			for key, value := range row.Env {
				env[key] = value
			}
			settings := settingsFor(t, env)
			var auth httpapi.Authenticator
			token := ""
			if row.Authenticated {
				auth, err = httpapi.NewTokenAuth([]httpapi.TokenBinding{{Token: "token-a", Principal: "alice"}})
				token = "token-a"
				if err != nil {
					t.Fatal(err)
				}
			}
			app, err := New(context.Background(), settings, config.ServerSettings{Host: "127.0.0.1"}, auth)
			if err != nil {
				t.Fatal(err)
			}
			defer stopApp(t, app)
			report := Inspect(settings, config.ServerSettings{}, auth)
			if report.WorkflowTools != row.Enabled || (app.manager.Workflows() != nil) != row.Enabled || len(report.Unsupported) != 0 {
				t.Fatal(report)
			}
			client := &http.Client{Timeout: 5 * time.Second}
			server := httptest.NewServer(app.Handler())
			defer server.Close()
			code, data := exchange(t, client, "POST", server.URL+"/sessions", "{}", token)
			if code != 200 {
				t.Fatal(code, string(data))
			}
			parent := decode[httpapi.SessionInfo](t, data)
			path := server.URL + "/sessions/" + string(parent.ID)
			_, data = exchange(t, client, "GET", server.URL+"/healthz", "", token)
			if decode[httpapi.HealthResponse](t, data).ExperimentalWorkflows != row.Health {
				t.Fatal(string(data))
			}
			code, data = exchange(t, client, "GET", path+"/workflows", "", token)
			if code != 200 || !reflect.DeepEqual(decode[httpapi.WorkflowListResponse](t, data), row.Listing) {
				t.Fatal(code, string(data))
			}
			code, data = exchange(t, client, "POST", path+"/messages", `{"message":"catalogue"}`, token)
			if code != 200 {
				t.Fatal(code, string(data))
			}
			names := []string{}
			for _, schema := range <-schemas {
				if strings.HasPrefix(string(schema.Name), "Workflow") {
					names = append(names, string(schema.Name))
				}
			}
			sort.Strings(names)
			if !reflect.DeepEqual(names, row.Tools) {
				t.Fatal(names, row.Tools)
			}
			if row.Enabled && strings.HasPrefix(row.Name, "caps-") {
				live, err := agent.ExplicitHumanRunContext(agent.HumanRunConfig{ApprovedCapabilities: []agent.RunCapability{agent.CapabilityWorkflowLaunch}})
				if err != nil {
					t.Fatal(err)
				}
				keys := []string{"max_concurrent_agents", "max_agents", "max_rounds", "wall_time_seconds"}
				for i, key := range keys {
					cap := row.Caps
					switch i {
					case 0:
						cap.MaxConcurrentAgents++
					case 1:
						cap.MaxAgents++
					case 2:
						cap.MaxRounds++
					case 3:
						cap.WallTimeSeconds++
					}
					budget := fmt.Sprintf(`{"max_concurrent_agents":%d,"max_agents":%d,"max_rounds":%d,"wall_time_seconds":%g}`, cap.MaxConcurrentAgents, cap.MaxAgents, cap.MaxRounds, cap.WallTimeSeconds)
					definition, err := jsonvalue.Decode(`{"name":"wf","return_from":"a","nodes":[{"id":"a","kind":"agent"}],"budget":` + budget + `}`)
					if err != nil {
						t.Fatal(err)
					}
					_, err = app.manager.Workflows().Launch(context.Background(), agent.WorkflowLaunchRequest{SessionID: parent.ID, Context: live, ActionID: agent.ActionID(key), Input: protocol.WorkflowInput{Definition: definition, Args: jsonvalue.ObjectValue(nil)}})
					if err == nil || err.Error() != "workflow "+key+" exceeds the process policy" {
						t.Fatal(key, err)
					}
				}
			}
		})
	}
}

func TestWorkflowLauncherServesArtifactsAndJoinsWorkers(t *testing.T) {
	var blocking atomic.Bool
	var parentCalls atomic.Int32
	entered, cancelled := make(chan struct{}), make(chan struct{})
	var enterOnce, cancelOnce sync.Once
	catalog := make(chan []protocol.ToolSchema, 8)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Tools    []protocol.ToolSchema
			Messages []struct{ Content jsonvalue.Value }
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		worker := false
		for _, schema := range req.Tools {
			if schema.Name == protocol.ToolReturnArtifact {
				worker = true
			}
		}
		if worker && blocking.Load() {
			enterOnce.Do(func() { close(entered) })
			<-r.Context().Done()
			cancelOnce.Do(func() { close(cancelled) })
			return
		}
		catalog <- req.Tools
		w.Header().Set("Content-Type", "application/json")
		if !worker && parentCalls.Add(1) == 1 {
			io.WriteString(w, `{"id":"untrusted","type":"message","role":"assistant","model":"configured","content":[{"type":"tool_use","id":"attempt","name":"Workflow","input":{"definition":{"name":"unauthorized","return_from":"a","nodes":[{"id":"a","kind":"agent"}]},"args":{}}}],"stop_reason":"tool_use","usage":{"input_tokens":5,"output_tokens":3}}`)
		} else if worker && len(req.Messages) == 1 {
			io.WriteString(w, `{"id":"artifact","type":"message","role":"assistant","model":"configured","content":[{"type":"tool_use","id":"submit","name":"return_artifact","input":{"value":{"answer":"done"}}}],"stop_reason":"tool_use","usage":{"input_tokens":5,"output_tokens":3}}`)
		} else {
			io.WriteString(w, `{"id":"done","type":"message","role":"assistant","model":"configured","content":[{"type":"text","text":"done"}],"stop_reason":"end_turn","usage":{"input_tokens":5,"output_tokens":3}}`)
		}
	}))
	defer upstream.Close()
	settings := settingsFor(t, map[string]string{"MINILOOP_FAKE_LLM": "0", "ANTHROPIC_API_KEY": "fixture-key", "ANTHROPIC_BASE_URL": upstream.URL, "MODEL_ID": "configured"})
	auth, err := httpapi.NewTokenAuth([]httpapi.TokenBinding{{Token: "token-a", Principal: "alice"}, {Token: "token-b", Principal: "bob"}})
	if err != nil {
		t.Fatal(err)
	}
	app, err := NewWithOptions(context.Background(), settings, config.ServerSettings{Host: "127.0.0.1"}, auth, Options{WorkflowTools: true})
	if err != nil {
		t.Fatal(err)
	}
	base, stop, done := startApp(t, app)
	defer stop()
	client := &http.Client{Timeout: 5 * time.Second}
	defer client.CloseIdleConnections()
	code, data := exchange(t, client, "POST", base+"/sessions", "{}", "token-a")
	if code != 200 {
		t.Fatal(code, string(data))
	}
	parent := decode[httpapi.SessionInfo](t, data)
	path := base + "/sessions/" + string(parent.ID)
	body := `{"definition":{"name":"wf","return_from":"a","nodes":[{"id":"a","kind":"agent"}]},"action_id":"one"}`
	code, data = exchange(t, client, "POST", path+"/workflows", body, "token-a")
	if code != 200 {
		t.Fatal(code, string(data))
	}
	launch := decode[httpapi.WorkflowLaunchResponse](t, data)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	run, err := app.manager.Workflows().Wait(ctx, launch.RunID)
	if err != nil || run.Status != workflows.RunCompleted {
		t.Fatal(run, err)
	}
	code, data = exchange(t, client, "GET", path+"/workflows/"+string(launch.RunID), "", "token-a")
	view := decode[workflows.RunStatusView](t, data)
	value, _ := view.Result.Lookup("answer")
	text, _ := value.Text()
	if code != 200 || text != "done" {
		t.Fatal(code, string(data))
	}
	if code, _ = exchange(t, client, "GET", path+"/workflows", "", "token-b"); code != 404 {
		t.Fatal(code)
	}
	for _, schema := range <-catalog {
		if schema.Name == protocol.ToolWorkflow || schema.Name == protocol.ToolWorkflowCancel {
			t.Fatal("worker got parent workflow authority", schema.Name)
		}
	}
	code, data = exchange(t, client, "POST", path+"/mode", `{"mode":"auto"}`, "token-a")
	if code != 200 {
		t.Fatal(code, string(data))
	}
	code, data = exchange(t, client, "POST", path+"/messages", `{"message":"model tries workflow"}`, "token-a")
	if code != 200 || parentCalls.Load() != 2 {
		t.Fatal(code, string(data), parentCalls.Load())
	}
	scope := workflows.SessionID(parent.ID)
	if runs := app.manager.Workflows().Store().ListRuns(&scope); len(runs) != 1 {
		t.Fatal("model gained launch authority", runs)
	}
	blocking.Store(true)
	body = strings.Replace(body, `"one"`, `"two"`, 1)
	code, data = exchange(t, client, "POST", path+"/workflows", body, "token-a")
	if code != 200 {
		t.Fatal(code, string(data))
	}
	second := decode[httpapi.WorkflowLaunchResponse](t, data)
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	stop()
	stopped(t, done)
	select {
	case <-cancelled:
	case <-ctx.Done():
		t.Fatal("provider request not cancelled", ctx.Err())
	}
	if app.manager.State() != agent.ManagerStopped || app.manager.Workflows().HasActive(workflows.SessionID(parent.ID)) {
		t.Fatal("workflow shutdown not joined")
	}
	run, err = app.manager.Workflows().Get(second.RunID)
	if err != nil || run.Status != workflows.RunCancelled {
		t.Fatal(run, err)
	}
}
