package agent

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/workflows"
)

func TestBoundWorkflowToolsMatchActualPython(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-workflow-bound-tools.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Rows []struct {
			Name                  string
			Tool                  protocol.ToolName
			Context               string
			Definition, Args      jsonvalue.Value
			Output, Error, Detail string
			InputHash             InputHash `json:"input_hash"`
		}
		Traits []struct {
			Name         protocol.ToolName
			Risk         ToolRisk
			Readonly     bool
			ParallelSafe bool `json:"parallel_safe"`
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, row := range fixture.Rows {
		t.Run(row.Name, func(t *testing.T) {
			journal, _ := NewInMemoryActionJournal(DefaultResultsRetained)
			var service *WorkflowService
			worker := WorkflowWorkerFactory(func(WorkflowRunnerConfig) (workflows.WorkflowRunner, error) {
				return workflows.WorkflowRunnerFunc(func(context.Context, workflows.AttemptExecution) (*workflows.ArtifactSubmission, error) {
					submission := workflows.NewArtifactSubmission(jsonvalue.ObjectValue([]jsonvalue.Field{{Name: "answer", Value: jsonvalue.TextValue("中文")}}), "return_artifact")
					return &submission, nil
				}), nil
			})
			path := t.TempDir()
			service, err = NewWorkflowService(WorkflowServiceConfig{Caps: workflows.DefaultDefinitionCaps(), Journal: journal, ResolveParent: func(SessionID) (WorkflowParent, bool) { return WorkflowParent{Owner: "owner", Workspace: path}, true }, WorkerFactory: worker})
			if err != nil {
				t.Fatal(err)
			}
			defer service.Close(context.Background())
			config := RuntimeConfig{ID: "s", Owner: "owner", Workspace: path, Mode: ModeAuto, Provider: &FakeProvider{}, Bash: echoExecutor{}, MaxRounds: 2, WorkflowTools: true, WorkflowService: service, ActionJournal: journal}
			if strings.HasPrefix(row.Name, "no-service") {
				config.WorkflowService = nil
			}
			parent, err := NewManagedSession(config)
			if err != nil {
				t.Fatal(err)
			}
			parent.runCount = 7
			handler := parent.core.runtime
			if row.Name == "no-parent" {
				handler.workflowParent = nil
			}
			trusted := nativeWorkflowRequest(t, "seed").Context
			trusted.approved = append(trusted.approved, CapabilityWorkflowManage)
			run := trusted.clone()
			switch row.Context {
			case "launch":
				run.approved = []RunCapability{CapabilityWorkflowLaunch}
			case "manage":
				run.approved = []RunCapability{CapabilityWorkflowManage}
			case "untrusted":
				run, _ = DefaultRunContext()
				run.messageID = "m"
			case "none":
				run = RunContext{}
			}
			input := protocol.WorkflowToolInput(protocol.WorkflowInput{Definition: row.Definition, Args: row.Args})
			ids := map[string]string{}
			if row.Tool != protocol.ToolWorkflow {
				request := WorkflowLaunchRequest{SessionID: "s", Input: protocol.WorkflowInput{Definition: row.Definition, Args: row.Args}, Context: trusted, ActionID: "seed", ToolUseID: "seed", LaunchTurn: 7}
				launched, err := service.Launch(context.Background(), request)
				if err != nil {
					t.Fatal(err)
				}
				completed, err := service.Wait(context.Background(), launched.RunID)
				if err != nil {
					t.Fatal(err)
				}
				ids[string(completed.RunID)] = "<run>"
				ids[string(*completed.FinalArtifactID)] = "<artifact>"
				for _, attempt := range service.Store().ListAttempts(completed.RunID) {
					ids[string(attempt.AttemptID)] = "<attempt>"
				}
				reference := protocol.WorkflowReferenceInput{RunID: completed.RunID}
				if row.Tool == protocol.ToolWorkflowStatus {
					input = protocol.WorkflowStatusToolInput(reference)
				} else {
					input = protocol.WorkflowCancelToolInput(reference)
				}
			}
			authority := handler.binding
			authority.RunContext = run
			authority.ActionID = "action"
			authority.ToolUseID = "u"
			if row.Name == "no-action" {
				authority.ActionID = ""
			}
			if row.Name == "no-call" {
				authority.ToolUseID = ""
			}
			// Foreign scope is exercised through a separately bound native session.
			if strings.HasPrefix(row.Name, "foreign") {
				foreign, err := NewManagedSession(RuntimeConfig{ID: "foreign", Owner: "owner", Workspace: path, Mode: ModeAuto, Provider: &FakeProvider{}, Bash: echoExecutor{}, MaxRounds: 2, WorkflowService: service})
				if err != nil {
					t.Fatal(err)
				}
				handler = foreign.core.runtime
				authority = handler.binding
				authority.RunContext = run
				authority.ActionID = "action"
				authority.ToolUseID = "u"
			}
			output, err := handler.ExecuteTool(context.Background(), authority, input)
			kind, detail := workflowServiceFailure(err)
			for raw, label := range ids {
				detail = strings.ReplaceAll(detail, raw, label)
			}
			if kind != row.Error || detail != row.Detail {
				t.Fatalf("failure got %s %q want %s %q", kind, detail, row.Error, row.Detail)
			}
			if output != "" {
				value, err := jsonvalue.Decode(output)
				if err != nil {
					t.Fatal(err)
				}
				if row.Tool == protocol.ToolWorkflow {
					field, _ := value.Lookup("run_id")
					id, _ := field.Text()
					ids[id] = "<run>"
					if _, err := service.Wait(context.Background(), workflows.RunID(id)); err != nil {
						t.Fatal(err)
					}
				}
				normalized := normalizedWorkflowProjection(value, ids)
				output, err = protocol.PythonJSON(normalized.Sorted(), false, false)
				if err != nil {
					t.Fatal(err)
				}
			}
			if output != row.Output {
				t.Fatalf("output got %s want %s", output, row.Output)
			}
			record, found, err := journal.Get(context.Background(), "action")
			if err != nil {
				t.Fatal(err)
			}
			hash := InputHash("")
			if found {
				hash = record.InputHash
			}
			if hash != row.InputHash {
				t.Fatalf("journal hash got %s want %s", hash, row.InputHash)
			}
			for _, expected := range fixture.Traits {
				definition, ok := parent.core.gate.catalog.Lookup(expected.Name)
				if !ok || definition.Risk() != expected.Risk || definition.Readonly() != expected.Readonly || definition.ParallelSafe() != expected.ParallelSafe {
					t.Fatalf("traits differ for %s", expected.Name)
				}
			}
		})
	}
}

func TestWorkflowGateOwnsRawJournalIdentityAndFencesReplay(t *testing.T) {
	ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	root := t.TempDir()
	journal, _ := NewInMemoryActionJournal(DefaultResultsRetained)
	ignored, _ := NewInMemoryActionJournal(DefaultResultsRetained)
	service, err := NewWorkflowService(WorkflowServiceConfig{Caps: workflows.DefaultDefinitionCaps(), Journal: journal, ResolveParent: func(SessionID) (WorkflowParent, bool) { return WorkflowParent{Owner: "owner", Workspace: root}, true }, WorkerFactory: func(WorkflowRunnerConfig) (workflows.WorkflowRunner, error) {
		return workflows.WorkflowRunnerFunc(func(context.Context, workflows.AttemptExecution) (*workflows.ArtifactSubmission, error) {
			result := workflows.NewArtifactSubmission(jsonvalue.ObjectValue([]jsonvalue.Field{{Name: "private", Value: jsonvalue.TextValue("private-result")}}), "return_artifact")
			return &result, nil
		}), nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close(ctx)
	config := runtimeConfig(root, &FakeProvider{})
	config.ID = "s"
	config.Mode = ModeAuto
	config.WorkflowService = service
	config.ActionJournal = ignored
	parent, err := NewManagedSession(config)
	if err != nil {
		t.Fatal(err)
	}
	parent.runCount = 9
	request := nativeWorkflowRequest(t, "action")
	request.Input.Definition, err = jsonvalue.Decode(`{"name":"wf","revision":"forged","return_from":"a","nodes":[{"id":"a","kind":"agent"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	run := request.Context
	run.approved = append(run.approved, CapabilityWorkflowManage)
	authority := parent.core.runtime.binding
	authority.RunContext = run
	call := ToolCall{ID: "launch", Input: protocol.WorkflowToolInput(request.Input)}
	first, err := parent.core.gate.Dispatch(ctx, authority, call)
	if err != nil || first.IsError() {
		t.Fatal(first, err)
	}
	action, err := ToolActionID("s", run, call)
	if err != nil {
		t.Fatal(err)
	}
	record, found, err := journal.Get(ctx, action)
	if err != nil || !found || record.WorkflowRunID == nil || record.Status != ActionCompleted {
		t.Fatal(record, found, err)
	}
	if _, found, _ := ignored.Get(ctx, action); found {
		t.Fatal("runtime used a conflicting journal")
	}
	candidate, err := actionCandidate(ActionRequest{ActionID: action, SessionID: "s", MessageID: run.MessageID(), ToolUseID: call.ID, Input: call.Input})
	if err != nil || record.InputHash != candidate.InputHash {
		t.Fatal(record, err)
	}
	completed, err := service.Wait(ctx, *record.WorkflowRunID)
	if err != nil || completed.Status != workflows.RunCompleted {
		t.Fatal(completed, err)
	}
	batch, err := service.Views().PrepareNotifications("s", 9)
	if err != nil || len(batch.Notifications()) != 0 {
		t.Fatal("launch-turn notification was admitted", batch, err)
	}
	repeated, err := parent.core.gate.Dispatch(ctx, authority, call)
	if err != nil || !repeated.Replayed || repeated.Output != first.Output {
		t.Fatal(repeated, err)
	}
	statusCall := ToolCall{ID: "status", Input: protocol.WorkflowStatusToolInput(protocol.WorkflowReferenceInput{RunID: completed.RunID})}
	good, err := parent.core.gate.Dispatch(ctx, authority, statusCall)
	if err != nil || good.IsError() || !strings.Contains(good.Output, "private-result") {
		t.Fatal(good, err)
	}
	for _, change := range []string{"owner", "session", "workspace", "capability", "untrusted"} {
		foreign := authority
		switch change {
		case "owner":
			foreign.OwnerID = "foreign"
		case "session":
			foreign.SessionID = "foreign"
		case "workspace":
			foreign.Workspace = t.TempDir()
		case "capability":
			foreign.RunContext = run.clone()
			foreign.RunContext.approved = []RunCapability{CapabilityWorkflowLaunch}
		case "untrusted":
			foreign.RunContext, _ = DefaultRunContext()
			foreign.RunContext.messageID = run.MessageID()
		}
		out, err := parent.core.gate.Dispatch(ctx, foreign, statusCall)
		if err != nil || !out.Denied || out.Replayed || strings.Contains(out.Output, "private-result") {
			t.Fatal(change, out, err)
		}
	}
	if _, err := parent.ChangePermissionMode(ModeReadonly); err != nil {
		t.Fatal(err)
	}
	readonly := authority
	readonly.Mode = ModeReadonly
	out, err := parent.core.gate.Dispatch(ctx, readonly, ToolCall{ID: "cancel", Input: protocol.WorkflowCancelToolInput(protocol.WorkflowReferenceInput{RunID: completed.RunID})})
	if err != nil || !out.Denied {
		t.Fatal("readonly cancel", out, err)
	}
}

func TestWorkflowToolsDefaultSelectionAndBareLaunch(t *testing.T) {
	config := runtimeConfig(t.TempDir(), &FakeProvider{})
	core, err := NewRuntimeSession(config)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := core.gate.catalog.Lookup(protocol.ToolWorkflow); ok {
		t.Fatal("default workflow activation")
	}
	config.WorkflowTools = true
	config.ActionJournal, _ = NewInMemoryActionJournal(DefaultResultsRetained)
	config.Mode = ModeAuto
	core, err = NewRuntimeSession(config)
	if err != nil {
		t.Fatal(err)
	}
	request := nativeWorkflowRequest(t, "action")
	authority := core.runtime.binding
	authority.RunContext = request.Context
	out, err := core.gate.Dispatch(context.Background(), authority, ToolCall{ID: "u", Input: protocol.WorkflowToolInput(request.Input)})
	if err != nil || !out.Failed || !strings.Contains(out.Output, "managed AgentSession") {
		t.Fatal(out, err)
	}
	config.ToolSelection = SelectTools(protocol.ToolWorkflowStatus)
	core, err = NewRuntimeSession(config)
	if err != nil {
		t.Fatal(err)
	}
	if len(core.gate.catalog.Names()) != 1 || core.gate.catalog.Names()[0] != protocol.ToolWorkflowStatus {
		t.Fatal(core.gate.catalog.Names())
	}
}

func TestManagedModelWorkflowLaunchAndToolCancellation(t *testing.T) {
	ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	root := t.TempDir()
	journal, _ := NewInMemoryActionJournal(DefaultResultsRetained)
	started := make(chan struct{})
	service, err := NewWorkflowService(WorkflowServiceConfig{Caps: workflows.DefaultDefinitionCaps(), Journal: journal, ResolveParent: func(SessionID) (WorkflowParent, bool) { return WorkflowParent{Owner: "owner", Workspace: root}, true }, WorkerFactory: func(WorkflowRunnerConfig) (workflows.WorkflowRunner, error) {
		return workflows.WorkflowRunnerFunc(func(workerCtx context.Context, _ workflows.AttemptExecution) (*workflows.ArtifactSubmission, error) {
			close(started)
			<-workerCtx.Done()
			return nil, workerCtx.Err()
		}), nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close(ctx)
	request := nativeWorkflowRequest(t, "action")
	run := request.Context
	run.approved = append(run.approved, CapabilityWorkflowManage)
	call := ToolCall{ID: "launch", Input: protocol.WorkflowToolInput(request.Input)}
	config := runtimeConfig(root, resourceProvider{[]protocol.Block{protocol.NewToolUse(call.ID, call.Input)}})
	config.ID = "s"
	config.Mode = ModeAuto
	config.WorkflowService = service
	parent, err := NewManagedSession(config)
	if err != nil {
		t.Fatal(err)
	}
	output, err := parent.RunWithContext(ctx, "launch", run)
	if err != nil || output != "done" {
		t.Fatal(output, err)
	}
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	action, err := ToolActionID("s", run, call)
	if err != nil {
		t.Fatal(err)
	}
	record, found, err := journal.Get(ctx, action)
	if err != nil || !found || record.WorkflowRunID == nil {
		t.Fatal(record, found, err)
	}
	authority := parent.core.runtime.binding
	authority.RunContext = run
	cancelled, err := parent.core.gate.Dispatch(ctx, authority, ToolCall{ID: "cancel", Input: protocol.WorkflowCancelToolInput(protocol.WorkflowReferenceInput{RunID: *record.WorkflowRunID})})
	if err != nil || cancelled.IsError() || !strings.Contains(cancelled.Output, `"status": "CANCELLED"`) {
		t.Fatal(cancelled, err)
	}
	result, err := service.Wait(ctx, *record.WorkflowRunID)
	if err != nil || result.Status != workflows.RunCancelled || result.CancelReason == nil || *result.CancelReason != "cancelled by trusted parent" || service.HasActive("s") {
		t.Fatal(result, err)
	}
	batch, err := service.Views().PrepareNotifications("s", 1)
	if err != nil || len(batch.Notifications()) != 0 {
		t.Fatal(batch, err)
	}
	batch, err = service.Views().PrepareNotifications("s", 2)
	if err != nil || len(batch.Notifications()) != 1 {
		t.Fatal(batch, err)
	}
}
