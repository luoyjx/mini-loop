package httpapi

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/workflows"
)

type httpClosureCase struct {
	Name, Group, Method, Path, Body, Token string
	Status                                 int
	Response                               jsonvalue.Value
	Headers                                map[string]string
	CronJobs                               int `json:"cron_jobs"`
	ModelCalls                             int `json:"model_calls"`
}
type httpClosureFixture struct {
	Cases                []httpClosureCase
	WorkflowDefinition   jsonvalue.Value   `json:"workflow_definition"`
	WorkflowActionID     string            `json:"workflow_action_id"`
	WorkflowCancelReason string            `json:"workflow_cancel_reason"`
	SourceSHA256         map[string]string `json:"source_sha256"`
	Scope                []string
}

func TestActualHTTPClosureCronAndWorkflowContracts(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-http-closure.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture httpClosureFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 24 {
		t.Fatalf("Source inventory %d; want 22 cron + 2 workflow", len(fixture.Cases))
	}
	entered := make(chan struct{})
	var manager *agent.SessionManager
	journal, err := agent.NewInMemoryActionJournal(1024)
	if err != nil {
		t.Fatal(err)
	}
	service, err := agent.NewWorkflowService(agent.WorkflowServiceConfig{
		Caps: workflows.DefaultDefinitionCaps(), Journal: journal,
		ResolveParent: func(id agent.SessionID) (agent.WorkflowParent, bool) {
			parent, err := manager.Get("alice", id)
			if err != nil {
				return agent.WorkflowParent{}, false
			}
			return agent.WorkflowParent{Owner: parent.Owner(), Workspace: parent.Info().Workspace}, true
		},
		WorkerFactory: func(agent.WorkflowRunnerConfig) (workflows.WorkflowRunner, error) {
			return workflows.WorkflowRunnerFunc(func(ctx context.Context, _ workflows.AttemptExecution) (*workflows.ArtifactSubmission, error) {
				close(entered)
				<-ctx.Done()
				return nil, ctx.Err()
			}), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	model := &countedProvider{}
	manager, err = agent.NewSessionManager(agent.ManagerConfig{WorkspaceRoot: t.TempDir(), Services: agent.ManagerServices{Provider: model, WorkflowService: service}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := manager.Stop(context.Background()); err != nil {
			t.Error(err)
		}
	})
	server := testServer(t, Config{Manager: manager, Auth: tokenAuth(t)})
	parent := create(t, server, "token-a")
	aliases := map[string]string{string(parent.ID): "session-fixture"}
	var runID workflows.RunID
	var cronCases, workflowCases int
	for _, row := range fixture.Cases {
		t.Run(row.Name, func(t *testing.T) {
			if row.Group == "workflow" {
				workflowCases++
				if runID == "" {
					payload, err := json.Marshal(struct {
						Definition jsonvalue.Value `json:"definition"`
						ActionID   string          `json:"action_id"`
					}{fixture.WorkflowDefinition, fixture.WorkflowActionID})
					if err != nil {
						t.Fatal(err)
					}
					launched := request(server, "POST", "/sessions/"+string(parent.ID)+"/workflows", string(payload), "token-a")
					if launched.Code != 200 {
						t.Fatalf("launch: %d %s", launched.Code, launched.Body.String())
					}
					result := decode[WorkflowLaunchResponse](t, launched.Body.Bytes())
					runID = result.RunID
					aliases[string(runID)] = "run-fixture"
					receive(t, entered)
				} else {
					payload, _ := json.Marshal(struct {
						Reason string `json:"reason"`
					}{fixture.WorkflowCancelReason})
					cancelled := request(server, "POST", "/sessions/"+string(parent.ID)+"/workflows/"+string(runID)+"/cancel", string(payload), "token-a")
					if cancelled.Code != 200 {
						t.Fatalf("cancel: %d %s", cancelled.Code, cancelled.Body.String())
					}
				}
			} else if row.Group == "cron" {
				cronCases++
			} else {
				t.Fatalf("unknown Source group %q", row.Group)
			}
			path := row.Path
			for actual, symbol := range aliases {
				path = strings.ReplaceAll(path, symbol, actual)
			}
			response := request(server, row.Method, path, row.Body, row.Token)
			if response.Code != row.Status {
				t.Fatalf("status %d; Source %d: %s", response.Code, row.Status, response.Body.String())
			}
			for _, job := range manager.CronScheduler().Jobs() {
				if _, found := aliases[string(job.ID)]; !found {
					count := 1
					for _, symbol := range aliases {
						if strings.HasPrefix(symbol, "job-") {
							count++
						}
					}
					aliases[string(job.ID)] = "job-" + strconv.Itoa(count)
				}
			}
			actual, err := jsonvalue.Decode(response.Body.String())
			if err != nil {
				t.Fatal(err)
			}
			if row.Group == "workflow" {
				var view workflows.RunStatusView
				if err := json.Unmarshal(response.Body.Bytes(), &view); err != nil {
					t.Fatal(err)
				}
				for _, node := range view.Nodes {
					for _, attempt := range node.AttemptIDs {
						aliases[string(attempt)] = "attempt-1"
					}
				}
			}
			actual = actual.MapStrings(func(value string) string {
				for raw, symbol := range aliases {
					value = strings.ReplaceAll(value, raw, symbol)
				}
				return value
			})
			if !reflect.DeepEqual(actual.Sorted(), row.Response.Sorted()) {
				t.Errorf("complete response\ngot %s\nSource %s", actual.PythonString(), row.Response.PythonString())
			}
			for _, key := range []string{"content-type", "x-content-type-options", "x-frame-options", "referrer-policy"} {
				if got := response.Header().Get(key); got != row.Headers[key] {
					t.Errorf("header %s=%q; Source %q", key, got, row.Headers[key])
				}
			}
			if len(manager.CronScheduler().Jobs()) != row.CronJobs || int(model.calls.Load()) != row.ModelCalls {
				t.Errorf("effects: cron %d/model %d; Source %d/%d", len(manager.CronScheduler().Jobs()), model.calls.Load(), row.CronJobs, row.ModelCalls)
			}
		})
	}
	if cronCases != 22 || workflowCases != 2 {
		t.Fatalf("consumed %d cron/%d workflow", cronCases, workflowCases)
	}
}
