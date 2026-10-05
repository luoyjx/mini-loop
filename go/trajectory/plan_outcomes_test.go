package trajectory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"slices"
	"testing"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/protocol"
)

type planUnusedBash struct{}

func (planUnusedBash) ExecuteBash(context.Context, protocol.BashInput) (string, error) {
	return "", errors.New("unexpected shell call in plan audit")
}

type planOutcome struct {
	Name                     protocol.ToolName
	Output                   string
	Failed, Denied, Replayed bool
}
type planObserverOutcome struct {
	Name           protocol.ToolName
	Output         string
	Failed, Denied bool
	Status         *agent.ActionStatus
	Result         *string
}
type planOutcomeStep struct {
	Name           protocol.ToolName
	Failed, Denied bool
}

// Compare the source wire result and its optional failure marker. Cache
// annotations are independently covered by cache contracts.
type planModelResult struct {
	Type      string
	ToolUseID string `json:"tool_use_id"`
	Content   string
	IsError   *bool `json:"is_error"`
}
type planOutcomeCase struct {
	Name, Final    string
	Requests       int
	Active         bool
	ReplayActive   bool `json:"replay_active"`
	Live, Stored   []planOutcome
	Observers      []planObserverOutcome
	ReplayObserver planObserverOutcome `json:"replay_observer"`
	Replay         planOutcome
	Reviews        []string
	Steps          []planOutcomeStep
	ModelResults   [][]planModelResult `json:"model_results"`
	ToolErrors     int                 `json:"tool_errors"`
}

// Harness state is local to one serialized test session; its interfaces exercise
// the production provider, gate, review, observer, journal and detector seams.
type planOutcomeHarness struct {
	name      string
	stage     int
	replay    bool
	reviews   []string
	observers []planObserverOutcome
	requests  []protocol.ModelRequest
	steps     []planOutcomeStep
	journal   *agent.InMemoryActionJournal
}

func (h *planOutcomeHarness) Respond(_ context.Context, r protocol.ModelRequest) (agent.FakeGeneration, error) {
	h.requests = append(h.requests, r.Clone())
	h.stage++
	index := h.stage
	if h.replay {
		index = 3
		if h.stage > 1 {
			index = 4
		}
	}
	var input protocol.ToolInput
	switch index {
	case 1:
		input = protocol.EnterPlanModeToolInput()
	case 2:
		input = protocol.ExitPlanModeToolInput(protocol.ExitPlanModeInput{Plan: "prose"})
	case 3:
		input = protocol.ExitPlanModeToolInput(protocol.ExitPlanModeInput{Plan: "# Execute\n1. do it"})
	default:
		return agent.FakeGeneration{Content: []protocol.Block{protocol.NewTextBlock("done")}, StopReason: protocol.StopEndTurn}, nil
	}
	return agent.FakeGeneration{Content: []protocol.Block{protocol.NewToolUse(fmt.Sprintf("plan-%d", index), input)}, StopReason: protocol.StopToolUse}, nil
}
func (h *planOutcomeHarness) ApprovePlan(_ context.Context, r agent.PlanReviewRequest) (agent.PlanReview, error) {
	h.reviews = append(h.reviews, r.Plan)
	if h.name == "reviewer-fault" {
		return agent.PlanReview{}, errors.New("reviewer unavailable")
	}
	return agent.PlanReview{Approved: h.name == "approved", Feedback: "revise step 2"}, nil
}
func (h *planOutcomeHarness) BeforeTool(_ context.Context, _ agent.ToolAuthority, c agent.ToolCall) (agent.BeforeDecision, error) {
	if h.name == "before-deny" && c.Name() == protocol.ToolExitPlanMode {
		return agent.DenyToolCall("DENIED: plan policy"), nil
	}
	return agent.KeepToolCall(), nil
}
func (h *planOutcomeHarness) AfterTool(_ context.Context, _ agent.ToolAuthority, c agent.ToolCall, value string) (string, error) {
	if h.name == "after-fault" && c.Name() == protocol.ToolExitPlanMode {
		return "", errors.New("post hook unavailable")
	}
	return value, nil
}
func (h *planOutcomeHarness) OnResult(ctx context.Context, a agent.ToolAuthority, c agent.ToolCall, v agent.ToolOutcome) error {
	row := planObserverOutcome{Name: c.Name(), Output: v.Output, Failed: v.Failed, Denied: v.Denied}
	record, exists, e := h.journal.Get(ctx, a.ActionID)
	if e != nil {
		return e
	}
	if exists {
		status := record.Status
		row.Status = &status
		row.Result = record.Result
	}
	h.observers = append(h.observers, row)
	return nil
}
func (h *planOutcomeHarness) Inspect(s agent.StuckState) (*agent.StuckSignal, error) {
	h.steps = nil
	for _, v := range s.Steps {
		h.steps = append(h.steps, planOutcomeStep{v.Name, v.Failed, v.Denied})
	}
	return nil, nil
}
func (h *planOutcomeHarness) MaxNudges() int { return 0 }
func planOutcomeEvents(records []agent.SessionEventRecord) []planOutcome {
	var out []planOutcome
	for _, r := range records {
		if v, ok := r.Event.ToolResult(); ok {
			out = append(out, planOutcome{v.Name, v.Output, v.Failed, v.Denied, v.Replayed})
		}
	}
	return out
}
func TestPlanOutcomesMatchActualPythonJournalEventsRequestsAndRecording(t *testing.T) {
	b, e := os.ReadFile("../testdata/python-plan-outcomes.json")
	if e != nil {
		t.Fatal(e)
	}
	var fixture struct{ Cases []planOutcomeCase }
	if e = json.Unmarshal(b, &fixture); e != nil {
		t.Fatal(e)
	}
	for _, row := range fixture.Cases {
		t.Run(row.Name, func(t *testing.T) {
			store, e := New(Config{Root: t.TempDir(), CaptureContent: true})
			if e != nil {
				t.Fatal(e)
			}
			journal, e := agent.NewInMemoryActionJournal(20)
			if e != nil {
				t.Fatal(e)
			}
			h := &planOutcomeHarness{name: row.Name, journal: journal}
			thinking := false
			cfg := agent.RuntimeConfig{ID: "session", Owner: "alice", Bash: planUnusedBash{}, Workspace: t.TempDir(), Provider: agent.NewFakeProvider(agent.FakeProviderConfig{Responder: h, Thinking: &thinking}), PlanModeTools: true, PlanApprover: h, Mode: agent.ModeAuto, MaxRounds: 10, Trajectories: store, ActionJournal: journal, StuckDetector: h, Hooks: agent.GateHooks{Before: []agent.BeforeHook{h}, After: []agent.AfterHook{h}, Observers: []agent.ResultObserver{h}}}
			s, e := agent.NewManagedSession(cfg)
			if e != nil {
				t.Fatal(e)
			}
			run, e := agent.DefaultRunContext()
			if e != nil {
				t.Fatal(e)
			}
			final, e := s.RunWithContext(context.Background(), "plan", run)
			if e != nil {
				t.Fatal(e)
			}
			if final != row.Final || len(h.requests) != row.Requests || s.PlanModeActive() != row.Active {
				t.Fatal(final, len(h.requests), s.PlanModeActive(), row)
			}
			if got := planOutcomeEvents(s.Events()); !reflect.DeepEqual(got, row.Live) {
				t.Fatal("live outcomes", got, row.Live)
			}
			if !reflect.DeepEqual(h.observers, row.Observers) || !reflect.DeepEqual(h.steps, row.Steps) {
				t.Fatal("settled observer/stuck outcomes", h.observers, h.steps, row)
			}
			var results [][]planModelResult
			for _, m := range h.requests[len(h.requests)-1].Messages {
				if blocks, ok := m.Content.Blocks(); ok && m.Role == protocol.RoleUser {
					for _, v := range blocks {
						if _, ok := v.ToolResult(); ok {
							encoded, e := json.Marshal(blocks)
							if e != nil {
								t.Fatal(e)
							}
							var wire []planModelResult
							if e = json.Unmarshal(encoded, &wire); e != nil {
								t.Fatal(e)
							}
							results = append(results, wire)
							break
						}
					}
				}
			}
			if !reflect.DeepEqual(results, row.ModelResults) {
				t.Fatal("model result shape", results, row.ModelResults)
			}
			rows, e := store.List(agent.TrajectoryQuery{Limit: 10})
			if e != nil || len(rows) != 1 {
				t.Fatal(rows, e)
			}
			if rows[0].Metrics.ToolErrors != row.ToolErrors || rows[0].Status != agent.TrajectoryCompleted {
				t.Fatal(rows[0])
			}
			stored, e := store.JSON(rows[0].ID, 8*1024*1024)
			if e != nil {
				t.Fatal(e)
			}
			var document struct {
				Events []struct {
					Type             agent.SessionEventKind
					Name             protocol.ToolName
					Output           string
					Failed           bool `json:"error"`
					Denied, Replayed bool
				}
			}
			if e = json.Unmarshal(stored, &document); e != nil {
				t.Fatal(e)
			}
			var outcomes []planOutcome
			for _, v := range document.Events {
				if v.Type == agent.EventToolResult {
					outcomes = append(outcomes, planOutcome{v.Name, v.Output, v.Failed, v.Denied, v.Replayed})
				}
			}
			if !reflect.DeepEqual(outcomes, row.Stored) {
				t.Fatal("stored outcomes", outcomes, row.Stored)
			}
			h.stage = 0
			h.replay = true
			if _, e = s.RunWithContext(context.Background(), "same source action", run); e != nil {
				t.Fatal(e)
			}
			events := planOutcomeEvents(s.Events())
			if !reflect.DeepEqual(events[len(events)-1], row.Replay) || !reflect.DeepEqual(h.observers[len(h.observers)-1], row.ReplayObserver) || !slices.Equal(h.reviews, row.Reviews) || s.PlanModeActive() != row.ReplayActive {
				t.Fatal("replay", events, h.observers, h.reviews, s.PlanModeActive(), row)
			}
		})
	}
}
