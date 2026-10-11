package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/workflows"
)

type workflowServiceCall struct {
	NodeID    workflows.NodeID     `json:"node_id"`
	Spawn     workflows.SpawnIndex `json:"spawn_index"`
	Inputs    workflows.Value      `json:"inputs"`
	Context   RunContextSnapshot   `json:"context"`
	MaxRounds int                  `json:"max_rounds"`
	Workspace string               `json:"workspace"`
}

func workflowServiceFixture(t *testing.T) []struct {
	Recipe          struct{ Name string }
	Definition      string `json:"definition_json"`
	Error, Detail   string
	OperationError  string `json:"operation_error"`
	OperationDetail string `json:"operation_detail"`
	Output          string `json:"output_json"`
} {
	t.Helper()
	var fixture struct {
		Rows []struct {
			Recipe          struct{ Name string }
			Definition      string `json:"definition_json"`
			Error, Detail   string
			OperationError  string `json:"operation_error"`
			OperationDetail string `json:"operation_detail"`
			Output          string `json:"output_json"`
		}
	}
	data, err := os.ReadFile(filepath.Join("..", "testdata", "python-workflow-service.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	return fixture.Rows
}
func workflowServiceFailure(err error) (string, string) {
	if err == nil {
		return "", ""
	}
	var service *WorkflowServiceError
	var validation *workflows.ValidationError
	var store *workflows.StoreError
	var conflict *ActionJournalConflict
	switch {
	case errors.As(err, &service):
		return string(service.Kind), service.Detail
	case errors.As(err, &validation):
		return string(validation.Kind), validation.Detail
	case errors.As(err, &store):
		return string(store.Kind), store.Detail
	case errors.As(err, &conflict):
		return "ActionJournalConflict", conflict.Error()
	default:
		return "NativeRunnerError", err.Error()
	}
}
func normalizedWorkflowProjection(value jsonvalue.Value, ids map[string]string) jsonvalue.Value {
	switch value.Kind() {
	case jsonvalue.Object:
		fields := []jsonvalue.Field{}
		for _, key := range value.Keys() {
			child, _ := value.Lookup(key)
			switch key {
			case "created_at", "started_at", "ended_at", "heartbeat_at", "completed_at", "occurred_at":
				if child.Kind() != jsonvalue.Null {
					child, _ = jsonvalue.Decode("0")
				}
			case "event_id":
				child = jsonvalue.TextValue("<event>")
			default:
				child = normalizedWorkflowProjection(child, ids)
			}
			fields = append(fields, jsonvalue.Field{Name: key, Value: child})
		}
		return jsonvalue.ObjectValue(fields)
	case jsonvalue.Array:
		children, _ := value.Array()
		for i, child := range children {
			children[i] = normalizedWorkflowProjection(child, ids)
		}
		return jsonvalue.ArrayValue(children)
	case jsonvalue.Text:
		// JSON value access remains closed; no open maps or interface payloads.
		var text string
		wire, _ := value.MarshalJSON()
		_ = json.Unmarshal(wire, &text)
		for raw, label := range ids {
			text = strings.ReplaceAll(text, raw, label)
		}
		return jsonvalue.TextValue(text)
	default:
		return value
	}
}
func TestWorkflowServiceMatchesActualPython(t *testing.T) {
	for _, row := range workflowServiceFixture(t) {
		t.Run(row.Recipe.Name, func(t *testing.T) {
			mode := row.Recipe.Name
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			started, release := make(chan struct{}), make(chan struct{})
			var startOnce, releaseOnce sync.Once
			defer releaseOnce.Do(func() { close(release) })
			var mu sync.Mutex
			events := []WorkflowEvent{}
			calls := []workflowServiceCall{}
			parent := WorkflowParent{Owner: "owner", Workspace: t.TempDir(), Events: WorkflowEventSinkFunc(func(_ context.Context, event WorkflowEvent) error {
				if mode == "observer-fault" {
					return workflowServiceError(WorkflowRuntimeError, "observer failed")
				}
				mu.Lock()
				events = append(events, event.Clone())
				mu.Unlock()
				return nil
			})}
			journal, _ := NewInMemoryActionJournal(DefaultResultsRetained)
			factory := func(config WorkflowRunnerConfig) (workflows.WorkflowRunner, error) {
				if mode == "factory-fault" {
					return nil, &workflows.RunnerError{Kind: workflows.RunnerRuntimeError, Detail: "factory fault"}
				}
				return workflows.WorkflowRunnerFunc(func(ctx context.Context, execution workflows.AttemptExecution) (*workflows.ArtifactSubmission, error) {
					live, err := config.ResolveContext(ctx, execution.Attempt)
					if err != nil {
						return nil, err
					}
					mu.Lock()
					calls = append(calls, workflowServiceCall{execution.Node.ID, execution.Attempt.SpawnIndex, execution.Inputs, live.Snapshot(), *config.MaxRounds, "<workspace>"})
					mu.Unlock()
					if err := config.EventSink.OnEvent(ctx, SessionEventRecord{Event: SessionEvent{kind: EventToolUse, toolUse: ToolUseEvent{Name: protocol.ToolCompress, ID: "u", Input: protocol.CompressToolInput()}}}); err != nil {
						return nil, err
					}
					startOnce.Do(func() { close(started) })
					if mode == "timeout" || mode == "cancel" || mode == "close" || mode == "wait-shield" {
						select {
						case <-ctx.Done():
							return nil, ctx.Err()
						case <-release:
						}
					}
					if mode == "worker-fault" {
						return nil, &workflows.RunnerError{Kind: workflows.RunnerRuntimeError, Detail: "worker fault"}
					}
					if mode == "missing-result" {
						return nil, nil
					}
					raw := fmt.Sprintf(`{"node":%q}`, execution.Node.ID)
					if execution.Node.Kind == workflows.Verify {
						raw = `{"status":"verified"}`
					}
					if mode == "invalid-result" {
						raw = `["bad"]`
					}
					value, err := jsonvalue.Decode(raw)
					if err != nil {
						return nil, err
					}
					submission := workflows.NewArtifactSubmission(value, "return_artifact")
					return &submission, nil
				}), nil
			}
			service, err := NewWorkflowService(WorkflowServiceConfig{Caps: workflows.DefaultDefinitionCaps(), Journal: journal, ResolveParent: func(SessionID) (WorkflowParent, bool) { return parent, mode != "no-parent" }, WorkerFactory: factory})
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				releaseOnce.Do(func() { close(release) })
				if err := service.Close(ctx); err != nil {
					t.Error(err)
				}
			}()
			input, err := jsonvalue.Decode(row.Definition)
			if err != nil {
				t.Fatal(err)
			}
			live := workflowLaunch(t)
			live.messageID = "m"
			if mode == "untrusted" || mode == "closed-first" {
				live, err = DefaultRunContext()
				if err != nil {
					t.Fatal(err)
				}
				live.messageID = "m"
			}
			if mode == "no-capability" {
				live.approved = nil
			}
			request := WorkflowLaunchRequest{SessionID: "s", Input: protocol.WorkflowInput{Definition: input, Args: jsonvalue.ObjectValue(nil)}, Context: live, ActionID: "action", ToolUseID: "u", LaunchTurn: 2}
			if mode == "no-action" {
				request.ActionID = ""
			}
			if mode == "raw-action" {
				original := request.Input
				request.ActionInput = &original
			}
			if mode == "closed-first" {
				if err := service.Close(ctx); err != nil {
					t.Fatal(err)
				}
			}
			launches := []WorkflowLaunchResult{}
			result, err := service.Launch(ctx, request)
			kind, detail := workflowServiceFailure(err)
			if kind != row.Error || detail != row.Detail {
				t.Fatalf("launch refusal got %s %s want %s %s", kind, detail, row.Error, row.Detail)
			}
			var run *workflows.WorkflowRun
			operationError, operationDetail := "", ""
			if err == nil {
				launches = append(launches, result)
				if mode == "timeout" || mode == "cancel" || mode == "close" || mode == "wait-shield" {
					select {
					case <-started:
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					}
				}
				if mode == "cancel" {
					if _, err := service.Cancel(ctx, result.RunID, nil, "requested"); err != nil {
						t.Fatal(err)
					}
				}
				if mode == "close" {
					if err := service.Close(ctx); err != nil {
						t.Fatal(err)
					}
				}
				if mode == "wait-shield" {
					waiting, cancelWait := context.WithCancel(ctx)
					cancelWait()
					if _, err := service.Wait(waiting, result.RunID); !errors.Is(err, context.Canceled) {
						t.Fatal("wait not cancelled", err)
					}
					releaseOnce.Do(func() { close(release) })
				}
				finished, err := service.Wait(ctx, result.RunID)
				if err != nil {
					t.Fatal(err)
				}
				run = &finished
				if mode == "terminal-replay" || mode == "payload-conflict" {
					replay := request
					if mode == "payload-conflict" {
						replay.Input.Args, _ = jsonvalue.Decode(`{"changed":true}`)
					}
					repeated, err := service.Launch(ctx, replay)
					operationError, operationDetail = workflowServiceFailure(err)
					if err == nil {
						launches = append(launches, repeated)
					}
				}
				if mode == "foreign-cancel" {
					foreign := workflows.SessionID("other")
					_, err := service.Cancel(ctx, result.RunID, &foreign, "requested")
					operationError, operationDetail = workflowServiceFailure(err)
				}
			}
			ids := map[string]string{}
			nodes := []workflows.NodeState{}
			attempts := []workflows.NodeAttempt{}
			artifacts := []workflows.ArtifactSnapshot{}
			if run != nil {
				ids[string(run.RunID)] = "<run>"
				nodes, err = service.store.ListNodes(run.RunID)
				if err != nil {
					t.Fatal(err)
				}
				attempts = service.store.ListAttempts(run.RunID)
				for _, attempt := range attempts {
					ids[string(attempt.AttemptID)] = "<attempt-" + string(attempt.NodeID) + ">"
					ids[string(attempt.AgentID)] = "<agent-" + string(attempt.NodeID) + ">"
				}
				for _, node := range nodes {
					nodeArtifacts, err := service.store.ArtifactsForNode(run.RunID, node.NodeID)
					if err != nil {
						t.Fatal(err)
					}
					for _, artifact := range nodeArtifacts {
						snapshot := artifact.Snapshot()
						artifacts = append(artifacts, snapshot)
						ids[string(snapshot.ArtifactID)] = "<artifact-" + string(node.NodeID) + ">"
					}
				}
			}
			outbox := service.store.ListOutbox(workflows.OutboxFilter{})
			for _, notice := range outbox {
				ids[string(notice.MessageID)] = "<outbox>"
			}
			for raw, label := range ids {
				operationDetail = strings.ReplaceAll(operationDetail, raw, label)
			}
			if operationError != row.OperationError || operationDetail != row.OperationDetail {
				t.Fatalf("operation got %s %s want %s %s", operationError, operationDetail, row.OperationError, row.OperationDetail)
			}
			record, exists, err := journal.Get(ctx, "action")
			if err != nil {
				t.Fatal(err)
			}
			var action *ActionRecord
			if exists {
				action = &record
			}
			mu.Lock()
			eventsCopy := append([]WorkflowEvent{}, events...)
			callsCopy := append([]workflowServiceCall{}, calls...)
			mu.Unlock()
			observations := service.ObservabilityErrors()
			if observations == nil {
				observations = []WorkflowObservationError{}
			}
			data, err := json.Marshal(struct {
				Launches      []WorkflowLaunchResult       `json:"launches"`
				Run           *workflows.WorkflowRun       `json:"run"`
				Nodes         []workflows.NodeState        `json:"nodes"`
				Attempts      []workflows.NodeAttempt      `json:"attempts"`
				Artifacts     []workflows.ArtifactSnapshot `json:"artifacts"`
				Outbox        []workflows.OutboxSnapshot   `json:"outbox"`
				Events        []WorkflowEvent              `json:"events"`
				Calls         []workflowServiceCall        `json:"calls"`
				Journal       *ActionRecord                `json:"journal"`
				Observability []WorkflowObservationError   `json:"observability"`
				Active        bool                         `json:"active"`
			}{launches, run, nodes, attempts, artifacts, outbox, eventsCopy, callsCopy, action, observations, service.HasActive("s")})
			if err != nil {
				t.Fatal(err)
			}
			projection, err := jsonvalue.Decode(string(data))
			if err != nil {
				t.Fatal(err)
			}
			got, err := workflows.CanonicalJSON(normalizedWorkflowProjection(projection, ids))
			if err != nil {
				t.Fatal(err)
			}
			expected, err := jsonvalue.Decode(row.Output)
			if err != nil {
				t.Fatal(err)
			}
			want, err := workflows.CanonicalJSON(expected)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != string(want) {
				t.Fatalf("service projection\ngot %s\nwant %s", got, want)
			}
		})
	}
}
