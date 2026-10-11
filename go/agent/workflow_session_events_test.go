package agent

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
	secretpkg "github.com/luoyjx/mini-loop/go/secrets"
	"github.com/luoyjx/mini-loop/go/workflows"
)

type workflowCaptureSink struct{ rows []SessionEventRecord }

func (sink *workflowCaptureSink) OnEvent(_ context.Context, row SessionEventRecord) error {
	sink.rows = append(sink.rows, row.clone())
	return nil
}

func TestWorkflowSessionEventsMatchActualPythonCapture(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-workflow-session-events.json")
	if err != nil {
		t.Fatal(err)
	}
	var source struct {
		Raw      []workflows.Value      `json:"raw"`
		Live     []SessionEventRecord   `json:"live"`
		Backlog  []SessionEventRecord   `json:"backlog"`
		Sink     []SessionEventRecord   `json:"sink"`
		Stored   []SessionEventRecord   `json:"stored"`
		Disabled []workflows.RunSummary `json:"disabled_summaries"`
	}
	if err := json.Unmarshal(data, &source); err != nil {
		t.Fatal(err)
	}
	store := newRuntimeStateStore()
	observer := &workflowCaptureSink{}
	registry := secretpkg.New(secretpkg.Config{})
	registry.RegisterValue("WORKFLOW_TOKEN", "workflow-secret-canary")
	parent, err := NewManagedSession(RuntimeConfig{ID: "s", Owner: "owner", Workspace: t.TempDir(), Provider: &FakeProvider{}, Bash: echoExecutor{}, Mode: ModeAuto, MaxRounds: 2,
		StateStore: store, StateLeaseOwner: "native", Secrets: registry, EventSink: observer})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { parent.core.persistence.release() })
	sub := parent.Subscribe(false)
	defer sub.Close()
	for _, raw := range source.Raw {
		bytes, err := json.Marshal(raw)
		if err != nil {
			t.Fatal(err)
		}
		event, err := decodeWorkflowEvent(bytes)
		if err != nil {
			t.Fatal(err)
		}
		if err := (managedWorkflowEvents{parent}).EmitWorkflowEvent(context.Background(), event); err != nil {
			t.Fatal(err)
		}
	}
	if len(source.Raw) != 12 {
		t.Fatal(len(source.Raw))
	}
	live := []SessionEventRecord{}
	for range source.Raw {
		select {
		case row := <-sub.Events():
			live = append(live, row)
		case <-time.After(time.Second):
			t.Fatal("missing live event")
		}
	}
	stored, err := store.LoadEvents(context.Background(), parent.ID(), 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertRows := func(label string, got, want []SessionEventRecord) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("%s length %d != %d", label, len(got), len(want))
		}
		for i := range got {
			got[i].Timestamp = 0
			bytes, err := json.Marshal(got[i])
			if err != nil {
				t.Fatal(err)
			}
			expected, err := json.Marshal(want[i])
			if err != nil {
				t.Fatal(err)
			}
			if string(bytes) != string(expected) {
				t.Fatalf("%s row %d\ngot %s\nwant %s", label, i, bytes, expected)
			}
			if strings.Contains(string(bytes), "workflow-secret-canary") {
				t.Fatal("secret reached observer")
			}
			roundtrip, err := DecodeStoredEvent(bytes)
			if err != nil {
				t.Fatal(err)
			}
			if roundtrip.Scope.RunContext.Authority() != AuthorityUntrusted {
				t.Fatal("archive acquired authority")
			}
		}
	}
	assertRows("live", live, source.Live)
	assertRows("backlog", parent.Events(), source.Backlog)
	assertRows("observer", observer.rows, source.Sink)
	assertRows("store", stored, source.Stored)
	if !reflect.DeepEqual(parent.Info().Workflows, source.Disabled) {
		t.Fatal(parent.Info().Workflows)
	}
	first, ok := live[3].Event.Workflow()
	if !ok || first.AttemptID != nil {
		t.Fatal("source node claim needs only node identity")
	}
	*first.NodeID = "changed"
	again, _ := live[3].Event.Workflow()
	if *again.NodeID == "changed" {
		t.Fatal("workflow accessor aliases retained event")
	}
}

func TestManagerWorkflowEventsAndSummariesUseLiveParent(t *testing.T) {
	provider := stateProviderFunc(func(_ context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
		if len(request.Messages) == 1 {
			return fakeReply([]protocol.Block{protocol.NewToolUse("a", protocol.ReturnArtifactToolInput(jsonvalue.ObjectValue(nil)))}, protocol.StopToolUse), nil
		}
		return fakeReply([]protocol.Block{protocol.NewTextBlock("done")}, protocol.StopEndTurn), nil
	})
	config := managerTestConfig(t.TempDir(), provider)
	config.Services.WorkflowTools = true
	config.Services.StateStore = newRuntimeStateStore()
	manager := makeManager(t, config)
	parent := createManaged(t, manager, CreateSessionRequest{Owner: "owner"})
	request := nativeWorkflowRequest(t, "events")
	request.SessionID = parent.ID()
	request.parent = parent
	ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	launch, err := manager.workflows.Launch(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.workflows.Wait(ctx, launch.RunID); err != nil {
		t.Fatal(err)
	}
	rows := parent.Events()
	seen := map[WorkflowEventKind]bool{}
	for _, row := range rows {
		event, ok := row.Event.Workflow()
		if !ok {
			continue
		}
		if event.SessionID != parent.ID() || event.RunID != launch.RunID || row.Scope.Label != "" || row.Event.Ephemeral() {
			t.Fatal(row)
		}
		seen[event.Kind] = true
	}
	for _, kind := range []WorkflowEventKind{WorkflowPlanned, WorkflowDecisionRecorded, WorkflowStarted, WorkflowNodeClaimed, WorkflowAgentStarted, WorkflowAgentProgress, WorkflowAgentCompleted, WorkflowCompleted, WorkflowResultEnqueued} {
		if !seen[kind] {
			t.Fatal("missing managed event", kind)
		}
	}
	data, err := os.ReadFile("../testdata/python-workflow-session-events.json")
	if err != nil {
		t.Fatal(err)
	}
	var source struct {
		Completed []workflows.RunSummary `json:"completed_summaries"`
		Fork      []workflows.RunSummary `json:"fork_summaries"`
	}
	if err := json.Unmarshal(data, &source); err != nil {
		t.Fatal(err)
	}
	summaries := parent.Info().Workflows
	if len(summaries) != 1 || summaries[0].RunID != launch.RunID || summaries[0].Status != workflows.RunCompleted {
		t.Fatal(summaries)
	}
	normalized := append([]workflows.RunSummary{}, summaries...)
	normalized[0].RunID = "run"
	if !reflect.DeepEqual(normalized, source.Completed) {
		t.Fatal(normalized, source.Completed)
	}
	child, err := manager.Fork(ctx, "owner", parent.ID())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(child.Info().Workflows, source.Fork) {
		t.Fatal("fork inherited parent's workflow summaries")
	}
	if _, err := manager.Delete("owner", parent.ID(), DeleteSessionOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, exists := manager.workflowParent(parent.ID()); exists {
		t.Fatal("deleted parent remains resolvable")
	}
}

func TestStoredWorkflowEventsRejectConflictingIdentityAndVersion(t *testing.T) {
	event := WorkflowEvent{Kind: WorkflowFailed, EventID: "e", OccurredAt: 1, SessionID: "s", RunID: "run", Name: "wf", Revision: "rev"}
	bytes, err := json.Marshal(SessionEventRecord{Sequence: 1, Timestamp: 1, SessionID: "s", TranscriptEpoch: 1, Event: SessionEvent{kind: SessionEventKind(event.Kind), workflow: event}})
	if err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]string{{`"payload_version":1`, `"payload_version":2`}, {`"kind":"workflow_failed"`, `"kind":"workflow_started"`}, {`"workflow_run_id":"run"`, `"workflow_run_id":"other"`}} {
		if _, err := DecodeStoredEvent([]byte(strings.Replace(string(bytes), pair[0], pair[1], 1))); err == nil {
			t.Fatal(pair)
		}
	}
	foreign := event
	foreign.SessionID = "other"
	parent, err := NewManagedSession(RuntimeConfig{ID: "s", Owner: "owner", Workspace: t.TempDir(), Provider: &FakeProvider{}, Bash: echoExecutor{}, Mode: ModeAuto, MaxRounds: 2})
	if err != nil {
		t.Fatal(err)
	}
	if err := (managedWorkflowEvents{parent}).EmitWorkflowEvent(context.Background(), foreign); err == nil || len(parent.Events()) != 0 {
		t.Fatal("foreign event published", err)
	}
}

func TestWorkflowShutdownRetainsTerminalObservabilityWithoutLaunchAdmission(t *testing.T) {
	ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	config := managerTestConfig(t.TempDir(), &FakeProvider{})
	config.Services.WorkflowTools = true
	manager := makeManager(t, config)
	parent := createManaged(t, manager, CreateSessionRequest{Owner: "owner"})
	entered := make(chan struct{})
	manager.workflows.config.WorkerFactory = func(WorkflowRunnerConfig) (workflows.WorkflowRunner, error) {
		return workflows.WorkflowRunnerFunc(func(ctx context.Context, _ workflows.AttemptExecution) (*workflows.ArtifactSubmission, error) {
			close(entered)
			<-ctx.Done()
			return nil, ctx.Err()
		}), nil
	}
	request := nativeWorkflowRequest(t, "stop")
	request.SessionID = parent.ID()
	request.parent = parent
	if _, err := manager.workflows.Launch(ctx, request); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := manager.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if _, ok := manager.workflowParent(parent.ID()); ok {
		t.Fatal("stopped manager admits launch")
	}
	data, err := os.ReadFile("../testdata/python-workflow-session-events.json")
	if err != nil {
		t.Fatal(err)
	}
	var source struct {
		Kinds []WorkflowEventKind `json:"shutdown_terminal_kinds"`
	}
	if err := json.Unmarshal(data, &source); err != nil {
		t.Fatal(err)
	}
	kinds := []WorkflowEventKind{}
	for _, row := range parent.Events() {
		event, ok := row.Event.Workflow()
		if ok && (event.Kind == WorkflowCancelled || event.Kind == WorkflowResultEnqueued) {
			kinds = append(kinds, event.Kind)
		}
	}
	if !reflect.DeepEqual(kinds, source.Kinds) {
		t.Fatal(kinds, source.Kinds)
	}
}
