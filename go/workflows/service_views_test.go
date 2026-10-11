package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/runmeta"
)

type viewsRecipe struct {
	Name, Text, Mode, Session string
	Status                    RunStatus
	StatusSession             string `json:"status_session"`
	Artifact                  bool
	MissingArtifact           bool `json:"missing_artifact"`
	Value                     Value
	Payload                   Value
	Repeat, Count             int
	LaunchTurn                *ParentTurn `json:"launch_turn"`
	Turn                      *ParentTurn
	RunError                  *string `json:"run_error"`
	CancelReason              *string `json:"cancel_reason"`
}
type notificationAppenderFunc func(context.Context, NotificationAppend) error

func (f notificationAppenderFunc) AppendWorkflowNotifications(ctx context.Context, message NotificationAppend) error {
	return f(ctx, message)
}

func viewsStore(t *testing.T, recipe viewsRecipe) (*ServiceViews, []RunID, map[string]string) {
	t.Helper()
	s := NewInMemoryStore()
	d, err := DecodeDefinition([]byte(`{"name":"wf","revision":"base","return_from":"a","nodes":[{"id":"a","kind":"agent"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RegisterDefinition(d); err != nil {
		t.Fatal(err)
	}
	views, _ := NewServiceViews(s)
	ids, labels := []RunID{}, map[string]string{}
	for i := 0; i < max(1, recipe.Count); i++ {
		r, err := s.CreateRun(CreateRunInput{DefinitionRevision: "base", SessionID: "s", IdempotencyKey: IdempotencyKey(fmt.Sprint("k", i)), Args: jsonvalue.ObjectValue(nil), RunContext: runmeta.Snapshot{MessageID: "msg", Origin: "api", Channel: "internal", Authority: runmeta.AuthorityUntrusted, StampedBy: "mini_loop", ApprovedCapabilities: []runmeta.Capability{}}})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, r.RunID)
		labels[string(r.RunID)] = fmt.Sprintf("<run-%d>", i)
		r.Status = recipe.Status
		if r.Status == "" {
			r.Status = RunCompleted
		}
		r.Error, r.CancelReason = recordPointer(recipe.RunError), recordPointer(recipe.CancelReason)
		if recipe.Artifact {
			value := recipe.Value
			if recipe.Repeat != 0 {
				value = jsonvalue.TextValue(strings.Repeat(recipe.Text, recipe.Repeat))
			}
			a, err := NewArtifact(ArtifactInput{Run: r.RunID, Node: "a", Attempt: "seed", Value: value, Schema: jsonvalue.ObjectValue(nil), Verification: NotApplicable})
			if err != nil {
				t.Fatal(err)
			}
			s.artifacts[a.Snapshot().ArtifactID] = a
			id := a.Snapshot().ArtifactID
			r.FinalArtifactID = &id
			labels[string(id)] = fmt.Sprintf("<artifact-%d>", i)
		}
		if recipe.MissingArtifact {
			id := ArtifactID("absent")
			r.FinalArtifactID = &id
		}
		s.runs[r.RunID] = r
		if recipe.LaunchTurn != nil {
			if err := views.RecordLaunchTurn(r.RunID, *recipe.LaunchTurn); err != nil {
				t.Fatal(err)
			}
		}
		payload := recipe.Payload
		if payload.Kind() != jsonvalue.Object {
			payload = jsonvalue.ObjectValue(nil)
		}
		notice, err := s.EnqueueOutbox(EnqueueOutboxInput{RunID: r.RunID, Kind: "notice", Payload: payload})
		if err != nil {
			t.Fatal(err)
		}
		labels[string(notice.MessageID)] = fmt.Sprintf("<notice-%d>", i)
	}
	return views, ids, labels
}

func TestServiceViewsMatchActualPython(t *testing.T) {
	var wireFixture struct {
		Rows []struct {
			Recipe        viewsRecipe
			Error, Detail string
			OutputJSON    string `json:"output_json"`
		}
	}
	wire, err := os.ReadFile("../testdata/python-workflow-views.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(wire, &wireFixture); err != nil {
		t.Fatal(err)
	}
	if len(wireFixture.Rows) != 33 {
		t.Fatal(len(wireFixture.Rows))
	}
	for _, row := range wireFixture.Rows {
		t.Run(row.Recipe.Name, func(t *testing.T) {
			views, ids, labels := viewsStore(t, row.Recipe)
			s := views.store
			session := SessionID(row.Recipe.Session)
			if session == "" {
				session = "s"
			}
			turn := ParentTurn(1)
			if row.Recipe.Turn != nil {
				turn = *row.Recipe.Turn
			}
			statusSession := SessionID(row.Recipe.StatusSession)
			if statusSession == "" {
				statusSession = "s"
			}
			type parentMessage struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			}
			output := struct {
				Statuses              []RunStatusView        `json:"statuses"`
				StatusError           string                 `json:"status_error"`
				Summaries             []RunSummary           `json:"summaries"`
				Notifications         []WorkflowNotification `json:"notifications"`
				MessageIDs            []OutboxID             `json:"message_ids"`
				HasToken              bool                   `json:"has_token"`
				Messages              []parentMessage        `json:"messages"`
				AppendObservedPending bool                   `json:"append_observed_pending"`
				Outbox                []OutboxSnapshot       `json:"outbox"`
			}{Statuses: []RunStatusView{}, Notifications: []WorkflowNotification{}, MessageIDs: []OutboxID{}, Messages: []parentMessage{}}
			for _, id := range ids {
				view, err := views.Status(id, &statusSession)
				if err != nil {
					var failure *StoreError
					if !errors.As(err, &failure) {
						t.Fatal(err)
					}
					output.StatusError = string(failure.Kind)
					output.Statuses = []RunStatusView{}
					break
				}
				output.Statuses = append(output.Statuses, view)
			}
			output.Summaries, err = views.Summaries(session)
			if err != nil {
				t.Fatal(err)
			}
			if row.Recipe.Mode == "append" || row.Recipe.Mode == "append-failure" {
				_, err = views.DeliverNotifications(context.Background(), session, turn, notificationAppenderFunc(func(ctx context.Context, request NotificationAppend) error {
					if request.SessionID != session || request.ParentTurn != turn {
						t.Fatal("wrong append scope")
					}
					output.AppendObservedPending = true
					for _, n := range s.ListOutbox(OutboxFilter{}) {
						output.AppendObservedPending = output.AppendObservedPending && n.DeliveredAt == nil && n.ClaimToken != nil
					}
					if row.Recipe.Mode == "append-failure" {
						return &RunnerError{Kind: RunnerRuntimeError, Detail: "append failed"}
					}
					output.Messages = append(output.Messages, parentMessage{Role: "user", Content: request.Content})
					return nil
				}))
			} else {
				var batch NotificationBatch
				batch, err = views.PrepareNotifications(session, turn)
				if err == nil {
					output.Notifications, output.MessageIDs, output.HasToken = batch.Notifications(), batch.MessageIDs(), batch.ClaimToken() != ""
					if row.Recipe.Mode == "ack" {
						err = views.AcknowledgeNotifications(batch)
					}
					if row.Recipe.Mode == "release" {
						err = views.ReleaseNotifications(batch)
					}
				}
			}
			kind, detail := engineError(err)
			var failure *RunnerError
			if errors.As(err, &failure) {
				kind, detail = string(failure.Kind), failure.Detail
			}
			if kind != row.Error || detail != row.Detail {
				t.Fatalf("outcome %s %s want %s %s", kind, detail, row.Error, row.Detail)
			}
			output.Outbox = s.ListOutbox(OutboxFilter{})
			for i := range output.Outbox {
				n := &output.Outbox[i]
				n.CreatedAt = 0
				if n.ClaimedAt != nil {
					zero := float64(0)
					n.ClaimedAt = &zero
				}
				if n.DeliveredAt != nil {
					zero := float64(0)
					n.DeliveredAt = &zero
				}
				if n.ClaimToken != nil {
					token := ClaimToken("<claim>")
					n.ClaimToken = &token
				}
			}
			actualWire, err := json.Marshal(output)
			if err != nil {
				t.Fatal(err)
			}
			actual, err := jsonvalue.Decode(string(actualWire))
			if err != nil {
				t.Fatal(err)
			}
			actual = actual.MapStrings(func(text string) string {
				for raw, label := range labels {
					text = strings.ReplaceAll(text, raw, label)
				}
				return text
			})
			compareAttemptProjection(t, actual, row.OutputJSON)
		})
	}
}

func TestServiceViewsConcurrentDeliveryAndBookkeeping(t *testing.T) {
	views, ids, _ := viewsStore(t, viewsRecipe{Count: 51})
	var mu sync.Mutex
	appended := 0
	appender := notificationAppenderFunc(func(ctx context.Context, request NotificationAppend) error {
		if !strings.HasPrefix(request.Content, `<workflow-results trust="untrusted-artifact-data">`) {
			t.Error("missing trust wrapper")
		}
		mu.Lock()
		appended++
		mu.Unlock()
		return nil
	})
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := views.DeliverNotifications(context.Background(), "s", 1, appender); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if appended != 2 {
		t.Fatal("duplicate or lost batches", appended)
	}
	for _, message := range views.store.ListOutbox(OutboxFilter{}) {
		if message.DeliveredAt == nil {
			t.Fatal("undelivered message")
		}
	}
	if err := views.RecordLaunchTurn(ids[0], 3); err != nil {
		t.Fatal(err)
	}
	if err := views.RecordLaunchTurn(ids[0], 9); err != nil {
		t.Fatal(err)
	}
	if views.launchTurns[ids[0]] != 3 {
		t.Fatal("launch turn changed on replay")
	}
	zero := TerminalRunLimit(0)
	removed := views.PruneTerminalRuns(&zero)
	if len(removed) != 51 || len(views.launchTurns) != 0 {
		t.Fatal("retained service bookkeeping", len(removed), views.launchTurns)
	}
}

func TestServiceStatusDetachedAndCoherentDuringExecution(t *testing.T) {
	seed := "seed"
	views, ids, _ := viewsStore(t, viewsRecipe{RunError: &seed})
	first, err := views.Status(ids[0], nil)
	if err != nil {
		t.Fatal(err)
	}
	*first.Error = "changed"
	first.Nodes[0].Status = NodeFailed
	again, _ := views.Status(ids[0], nil)
	if *again.Error != "seed" || again.Nodes[0].Status != NodePending {
		t.Fatal("mutable status projection")
	}
	r := views.store.runs[ids[0]]
	r.Status, r.Error = RunQueued, nil
	views.store.runs[ids[0]] = r
	entered, resume := make(chan struct{}), make(chan struct{})
	engine, err := NewWorkflowEngine(views.store, WorkflowRunnerFunc(func(ctx context.Context, input AttemptExecution) (*ArtifactSubmission, error) {
		close(entered)
		<-resume
		submission := ReturnArtifact(jsonvalue.ObjectValue(nil))
		return &submission, nil
	}), EngineOptions{})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := engine.Execute(context.Background(), ids[0]); done <- err }()
	<-entered
	close(resume)
	for {
		view, err := views.Status(ids[0], nil)
		if err != nil {
			t.Fatal(err)
		}
		active := 0
		for _, node := range view.Nodes {
			if node.Status == NodeRunning {
				active++
			}
		}
		if active != len(view.ActiveNodeIDs) {
			t.Fatal("mixed run/node projection", view)
		}
		if view.Status.Terminal() {
			break
		}
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
