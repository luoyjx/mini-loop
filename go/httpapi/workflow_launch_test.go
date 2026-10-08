package httpapi

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/workflows"
)

type workflowHTTPFixture struct {
	Rows []struct {
		Mode    string
		Context *agent.RunContextSnapshot
		Cases   []struct {
			Name, Method, Path, Body, Token string
			Status                          int
			Response                        jsonvalue.Value
			AwaitWorker                     bool `json:"await_worker"`
		}
	}
}

func TestActualPythonWorkflowHTTPLaunchContracts(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-workflow-http.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture workflowHTTPFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, row := range fixture.Rows {
		t.Run(row.Mode, func(t *testing.T) {
			provider := &blockedProvider{entered: make(chan struct{})}
			manager, err := agent.NewSessionManager(agent.ManagerConfig{WorkspaceRoot: t.TempDir(), Services: agent.ManagerServices{Provider: provider, WorkflowTools: row.Mode != "disabled"}})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := manager.Stop(context.Background()); err != nil {
					t.Error(err)
				}
			})
			var auth Authenticator = NullAuth{}
			token := ""
			if row.Mode != "anonymous" {
				auth, token = tokenAuth(t), "token-a"
			}
			s := testServer(t, Config{Manager: manager, Auth: auth})
			parent, other := create(t, s, token), create(t, s, token)
			ids := map[string]string{string(parent.ID): "s", string(other.ID): "other"}
			path := func(value string) string {
				value = strings.ReplaceAll(value, "/sessions/s/", "/sessions/"+string(parent.ID)+"/")
				value = strings.ReplaceAll(value, "/sessions/other/", "/sessions/"+string(other.ID)+"/")
				for actual, symbol := range ids {
					if strings.HasPrefix(symbol, "run-") {
						value = strings.ReplaceAll(value, symbol, actual)
					}
				}
				return value
			}
			var runID workflows.RunID
			for _, tc := range row.Cases {
				got := request(s, tc.Method, path(tc.Path), tc.Body, tc.Token)
				if got.Code != tc.Status {
					t.Fatalf("%s: %d %s; expected %d", tc.Name, got.Code, got.Body.String(), tc.Status)
				}
				actual, err := jsonvalue.Decode(got.Body.String())
				if err != nil {
					t.Fatal(tc.Name, err)
				}
				if id, found := actual.Lookup("run_id"); found && got.Code == 200 {
					raw, _ := id.Text()
					if _, known := ids[raw]; !known {
						ids[raw] = "run-1"
					}
					runID = workflows.RunID(raw)
				}
				if tc.AwaitWorker {
					receive(t, provider.entered)
				}
				actual = actual.MapStrings(func(text string) string {
					for raw, symbol := range ids {
						text = strings.ReplaceAll(text, raw, symbol)
					}
					return text
				}).Sorted()
				if !reflect.DeepEqual(actual, tc.Response.Sorted()) {
					want, _ := tc.Response.MarshalJSON()
					t.Fatalf("%s: %s; expected %s", tc.Name, got.Body.String(), want)
				}
			}
			if row.Context != nil {
				stored, err := manager.Workflows().Store().GetRun(runID)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(stored.RunContext, *row.Context) {
					t.Fatalf("context: %#v; expected %#v", stored.RunContext, *row.Context)
				}
			}
		})
	}
}

func TestWorkflowHTTPGeneratedActionIdentityAndFreshWorker(t *testing.T) {
	manager, err := agent.NewSessionManager(agent.ManagerConfig{WorkspaceRoot: t.TempDir(), Services: agent.ManagerServices{Provider: workflowArtifactProvider{}, WorkflowTools: true}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := manager.Stop(context.Background()); err != nil {
			t.Error(err)
		}
	})
	s := testServer(t, Config{Manager: manager, Auth: tokenAuth(t)})
	parent := create(t, s, "token-a")
	ids := map[agent.ActionID]bool{}
	for _, action := range []string{"", `,"action_id":null`, `,"action_id":""`} {
		body := `{"definition":{"name":"wf","return_from":"a","nodes":[{"id":"a","kind":"agent"}]}` + action + `}`
		got := request(s, "POST", "/sessions/"+string(parent.ID)+"/workflows", body, "token-a")
		if got.Code != 200 {
			t.Fatal(got.Code, got.Body.String())
		}
		launch := decode[WorkflowLaunchResponse](t, got.Body.Bytes())
		id := string(launch.ActionID)
		if len(id) != 23 || !strings.HasPrefix(id, "wfhttp_") || id[19] != '4' || ids[launch.ActionID] {
			t.Fatal(id)
		}
		ids[launch.ActionID] = true
		run, err := manager.Workflows().Wait(context.Background(), launch.RunID)
		if err != nil {
			t.Fatal(err)
		}
		if run.Status != workflows.RunCompleted || run.RunContext.MessageID != agent.MessageID("msg_"+id) || run.RunContext.Authority != agent.AuthorityExplicitHuman || run.RunContext.ActorID == nil || *run.RunContext.ActorID != "alice" {
			t.Fatal(run)
		}
		if !reflect.DeepEqual(run.RunContext.ApprovedCapabilities, []agent.RunCapability{agent.CapabilityWorkflowLaunch}) {
			t.Fatal(run.RunContext)
		}
	}
}
