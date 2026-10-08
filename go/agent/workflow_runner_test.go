package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/workflows"
	workspacepkg "github.com/luoyjx/mini-loop/go/workspace"
)

func workflowLaunch(t *testing.T) RunContext {
	t.Helper()
	actor := ActorID("human")
	launch, err := ExplicitHumanRunContext(HumanRunConfig{ActorID: &actor, ApprovedCapabilities: []RunCapability{CapabilityWorkflowLaunch, CapabilityWorkflowManage}})
	if err != nil {
		t.Fatal(err)
	}
	return launch
}
func workflowExecution(t *testing.T, schema string) workflows.AttemptExecution {
	t.Helper()
	output, err := jsonvalue.Decode(schema)
	if err != nil {
		t.Fatal(err)
	}
	inputs, _ := jsonvalue.Decode(`{"args":{"text":"你好"}}`)
	return workflows.AttemptExecution{Attempt: workflows.NodeAttempt{AttemptID: "attempt", RunID: "run", NodeID: "a", AgentID: "worker", Attempt: 1}, Node: workflows.Node{ID: "a", Kind: workflows.Agent, OutputSchema: output, PromptTemplate: "inspect"}, Inputs: inputs}
}

func TestFreshWorkflowRunnerMatchesActualPython(t *testing.T) {
	var fixture struct {
		Rows []struct {
			Recipe struct {
				Name       string
				Schema     jsonvalue.Value
				Values     []jsonvalue.Value
				NodeRounds *int `json:"node_rounds"`
				Task       *string
			}
			Value         jsonvalue.Value
			Error, Detail string
			Calls, Rounds int
			Tools         []protocol.ToolName
			System, Mode  string
			Schema        jsonvalue.Value
			Context       struct {
				Authority            RunAuthority
				ActorID              ActorID         `json:"actor_id"`
				ParentMessageID      string          `json:"parent_message_id"`
				DelegatedBy          string          `json:"delegated_by"`
				ApprovedCapabilities []RunCapability `json:"approved_capabilities"`
			}
		}
	}
	wire, err := os.ReadFile("../testdata/python-workflow-runner.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(wire, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Rows) != 8 {
		t.Fatal(len(fixture.Rows))
	}
	for _, row := range fixture.Rows {
		t.Run(row.Recipe.Name, func(t *testing.T) {
			calls := 0
			provider := stateProviderFunc(func(ctx context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
				if request.System == nil || !strings.Contains(*request.System, row.System) {
					t.Fatal("worker system differs from source")
				}
				if calls == 0 {
					if len(request.Messages) != 1 {
						t.Fatal("worker inherited conversation")
					}
					if !slices.Equal([]protocol.ToolName{request.Tools[0].Name, request.Tools[1].Name, request.Tools[2].Name}, row.Tools) {
						t.Fatal("worker tools differ")
					}
					actual, _ := json.Marshal(request.Tools[2].InputSchema)
					value, _ := jsonvalue.Decode(string(actual))
					got, _ := workflows.CanonicalJSON(value)
					want, _ := workflows.CanonicalJSON(row.Schema)
					if string(got) != string(want) {
						t.Fatalf("schema %s want %s", got, want)
					}
				}
				current := calls
				calls++
				if current < len(row.Recipe.Values) {
					return fakeReply([]protocol.Block{protocol.NewToolUse("u", protocol.ReturnArtifactToolInput(row.Recipe.Values[current]))}, protocol.StopToolUse), nil
				}
				return fakeReply([]protocol.Block{protocol.NewTextBlock("done")}, protocol.StopEndTurn), nil
			})
			launch := workflowLaunch(t)
			limit := 4
			runner, err := NewFreshWorkflowRunner(WorkflowRunnerConfig{Provider: provider, Owner: "owner", Workspace: t.TempDir(), MaxRounds: &limit, ResolveContext: func(context.Context, workflows.NodeAttempt) (RunContext, error) { return launch, nil }})
			if err != nil {
				t.Fatal(err)
			}
			execution := workflowExecution(t, `{}`)
			execution.Node.OutputSchema = row.Recipe.Schema
			execution.Node.MaxRounds = row.Recipe.NodeRounds
			if row.Recipe.Task != nil {
				execution.Node.PromptTemplate = *row.Recipe.Task
			}
			submission, err := runner.Run(context.Background(), execution)
			kind, detail := "", ""
			if err != nil {
				var failure *workflows.RunnerError
				if !errors.As(err, &failure) {
					t.Fatal(err)
				}
				kind, detail = string(failure.Kind), failure.Detail
			}
			if kind != row.Error || detail != row.Detail || calls != row.Calls {
				t.Fatalf("outcome %s %s calls %d want %s %s %d", kind, detail, calls, row.Error, row.Detail, row.Calls)
			}
			if submission != nil {
				got, _ := submission.Value().MarshalJSON()
				want, _ := row.Value.MarshalJSON()
				if string(got) != string(want) {
					t.Fatalf("value %s want %s", got, want)
				}
			}
			view, ok := runner.LastWorker()
			if !ok || view.MaxRounds != row.Rounds || !slices.Equal(view.ToolNames, row.Tools) {
				t.Fatal(view)
			}
			peer := view.RunContext
			if peer.Authority != row.Context.Authority || peer.ActorID == nil || *peer.ActorID != row.Context.ActorID || peer.DelegatedBy == nil || *peer.DelegatedBy != row.Context.DelegatedBy || peer.ParentMessageID == nil || *peer.ParentMessageID != launch.MessageID() || len(peer.ApprovedCapabilities) != 0 {
				t.Fatal("peer authority", peer)
			}
			view.ToolNames[0] = protocol.ToolBash
			again, _ := runner.LastWorker()
			if again.ToolNames[0] == protocol.ToolBash {
				t.Fatal("mutable diagnostics")
			}
		})
	}
}

func TestWorkflowWorkerReadMaskingAndReadonlyBackstop(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte(runtimeCanary), 0600); err != nil {
		t.Fatal(err)
	}
	files, _ := workspacepkg.NewFiles(root)
	read, _ := NewToolDefinition(protocol.ToolReadFile, ToolTraits{Risk: RiskRead, Readonly: true, Capabilities: []Capability{CapabilityRepoRead}}, workspaceFileHandler{files, protocol.ToolReadFile})
	write, _ := NewToolDefinition(protocol.ToolWriteFile, ToolTraits{Risk: RiskWrite, Capabilities: []Capability{CapabilityWorkspaceWrite}}, workspaceFileHandler{files, protocol.ToolWriteFile})
	ownedSchema, _ := protocol.WorkflowArtifactSchema(jsonvalue.ObjectValue(nil))
	foreignCapture := false
	foreign, _ := NewToolDefinitionWithSchema(ownedSchema, ToolTraits{Risk: RiskRead, Readonly: true, Capabilities: []Capability{CapabilityRepoRead}}, scheduledHandlerFunc(func(context.Context, ToolAuthority, protocol.ToolInput) (string, error) {
		foreignCapture = true
		return "foreign capture", nil
	}))
	catalog, _ := NewToolCatalog(write, read, foreign)
	policy, _ := NewCapabilityRoleToolPolicy([]RoleCapabilityProfile{{Role: RoleExplore, Capabilities: []Capability{CapabilityRepoRead, CapabilityWorkspaceWrite}}})
	calls := 0
	provider := stateProviderFunc(func(ctx context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
		calls++
		if calls == 1 {
			return fakeReply([]protocol.Block{
				protocol.NewToolUse("w", protocol.WriteFileToolInput(protocol.WriteFileInput{Path: "note.txt", Content: "changed"})),
				protocol.NewToolUse("r", protocol.ReadFileToolInput(protocol.ReadFileInput{Path: "note.txt"})),
			}, protocol.StopToolUse), nil
		}
		if calls == 2 {
			blocks, _ := request.Messages[len(request.Messages)-1].Content.Blocks()
			denied, _ := blocks[0].ToolResult()
			masked, _ := blocks[1].ToolResult()
			if !strings.Contains(denied.Content, "read-only") || strings.Contains(masked.Content, runtimeCanary) {
				t.Fatal(denied, masked)
			}
			value := jsonvalue.TextValue(masked.Content)
			return fakeReply([]protocol.Block{protocol.NewToolUse("a", protocol.ReturnArtifactToolInput(value))}, protocol.StopToolUse), nil
		}
		return fakeReply([]protocol.Block{protocol.NewTextBlock("done")}, protocol.StopEndTurn), nil
	})
	launch := workflowLaunch(t)
	runner, err := NewFreshWorkflowRunner(WorkflowRunnerConfig{Provider: provider, Owner: "owner", Workspace: root, Catalog: catalog, RolePolicy: policy, Secrets: runtimeSecrets(), ResolveContext: func(context.Context, workflows.NodeAttempt) (RunContext, error) { return launch, nil }})
	if err != nil {
		t.Fatal(err)
	}
	submission, err := runner.Run(context.Background(), workflowExecution(t, `{"type":"string"}`))
	if err != nil {
		t.Fatal(err)
	}
	if foreignCapture {
		t.Fatal("operator handler replaced owned artifact capture")
	}
	wire, _ := submission.Value().MarshalJSON()
	if strings.Contains(string(wire), runtimeCanary) {
		t.Fatal("secret artifact", string(wire))
	}
	content, _ := os.ReadFile(filepath.Join(root, "note.txt"))
	if string(content) != runtimeCanary {
		t.Fatal("worker mutated workspace")
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 1 {
		t.Fatal("context management wrote files", entries)
	}
}

func TestFreshWorkflowRunnerConcurrentIsolationAndInvalidContext(t *testing.T) {
	provider := stateProviderFunc(func(ctx context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
		if len(request.Messages) == 1 {
			return fakeReply([]protocol.Block{protocol.NewToolUse("a", protocol.ReturnArtifactToolInput(jsonvalue.ObjectValue(nil)))}, protocol.StopToolUse), nil
		}
		if len(request.Messages) != 3 {
			t.Fatal("shared history", len(request.Messages))
		}
		return fakeReply([]protocol.Block{protocol.NewTextBlock("done")}, protocol.StopEndTurn), nil
	})
	launch := workflowLaunch(t)
	runner, err := NewFreshWorkflowRunner(WorkflowRunnerConfig{Provider: provider, Owner: "owner", Workspace: t.TempDir(), ResolveContext: func(context.Context, workflows.NodeAttempt) (RunContext, error) { return launch, nil }})
	if err != nil {
		t.Fatal(err)
	}
	execution := workflowExecution(t, `{"type":"object"}`)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := runner.Run(context.Background(), execution); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	called := false
	runner, err = NewFreshWorkflowRunner(WorkflowRunnerConfig{Provider: stateProviderFunc(func(context.Context, protocol.ModelRequest) (protocol.ModelReply, error) {
		called = true
		return protocol.ModelReply{}, nil
	}), Owner: "owner", Workspace: t.TempDir(), ResolveContext: func(context.Context, workflows.NodeAttempt) (RunContext, error) { return RunContext{}, nil }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Run(context.Background(), execution); err == nil || called {
		t.Fatal("invalid live context reached model")
	}
}

func TestWorkflowEngineUsesFreshNativeWorkers(t *testing.T) {
	definition, err := workflows.DecodeDefinition([]byte(`{"name":"native","revision":"base","return_from":"b","nodes":[{"id":"a","kind":"agent"},{"id":"b","kind":"reduce","needs":["a"]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	store := workflows.NewInMemoryStore()
	if _, err = store.RegisterDefinition(definition); err != nil {
		t.Fatal(err)
	}
	launch := workflowLaunch(t)
	run, err := store.CreateRun(workflows.CreateRunInput{DefinitionRevision: "base", SessionID: "parent", RunContext: launch.Snapshot(), IdempotencyKey: "key", Args: jsonvalue.ObjectValue(nil)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.TransitionRun(run.RunID, run.Version, workflows.RunQueued, nil); err != nil {
		t.Fatal(err)
	}
	provider := stateProviderFunc(func(ctx context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
		if len(request.Messages) == 1 {
			return fakeReply([]protocol.Block{protocol.NewToolUse("a", protocol.ReturnArtifactToolInput(jsonvalue.ObjectValue(nil)))}, protocol.StopToolUse), nil
		}
		return fakeReply([]protocol.Block{protocol.NewTextBlock("done")}, protocol.StopEndTurn), nil
	})
	runner, err := NewFreshWorkflowRunner(WorkflowRunnerConfig{Provider: provider, Owner: "owner", Workspace: t.TempDir(), ResolveContext: func(ctx context.Context, attempt workflows.NodeAttempt) (RunContext, error) {
		if attempt.RunID != run.RunID {
			return RunContext{}, errors.New("unbound run")
		}
		return launch, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	engine, err := workflows.NewWorkflowEngine(store, runner, workflows.EngineOptions{})
	if err != nil {
		t.Fatal(err)
	}
	completed, err := engine.Execute(context.Background(), run.RunID)
	if err != nil || completed.Status != workflows.RunCompleted || completed.AttemptsUsed != 2 || completed.FinalArtifactID == nil {
		t.Fatal(completed, err)
	}
	for _, attempt := range store.ListAttempts(run.RunID) {
		if attempt.Status != workflows.AttemptSucceeded {
			t.Fatal(attempt)
		}
	}
}
