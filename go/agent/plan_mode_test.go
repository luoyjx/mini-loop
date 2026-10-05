package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/secrets"
)

type planApproverFunc func(context.Context, PlanReviewRequest) (PlanReview, error)

func (f planApproverFunc) ApprovePlan(ctx context.Context, r PlanReviewRequest) (PlanReview, error) {
	return f(ctx, r)
}

// Raw inputs exist only at this external fixture boundary.
type planModeFixture struct {
	Section  string
	Schemas  []protocol.ToolSchema
	Metadata []struct {
		Name         protocol.ToolName
		Risk         ToolRisk
		Readonly     bool
		ParallelSafe bool `json:"parallel_safe"`
		Capabilities []Capability
	}
	Variants []struct {
		Name                protocol.ToolName
		Input               json.RawMessage
		Canonical           string
		Candidate, Proposed []string
	}
	Cases []struct {
		Name    string
		Reviews []string
		Steps   []struct {
			Name            protocol.ToolName
			Input           json.RawMessage
			Output          string
			Active, Section bool
			Events          []bool
			CatalogStable   bool `json:"catalog_stable"`
		}
	}
	Restored []struct {
		Active, Folded  bool
		RequestSections []bool `json:"request_sections"`
	}
}

func planFixture(t *testing.T) planModeFixture {
	t.Helper()
	b, err := os.ReadFile("../testdata/python-plan-mode.json")
	if err != nil {
		t.Fatal(err)
	}
	var f planModeFixture
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	return f
}
func planDispatch(ctx context.Context, s *Session, id string, input protocol.ToolInput) (ToolOutcome, error) {
	return s.gate.Dispatch(ctx, ToolAuthority{SessionID: s.id, OwnerID: s.owner, Workspace: s.executionRoot(), Mode: s.permissionMode()}, ToolCall{ID: id, Input: input})
}

func TestPlanModeMatchesActualSourceGatesAndPrompts(t *testing.T) {
	f := planFixture(t)
	if f.Section != PlanSection || !reflect.DeepEqual(f.Schemas, protocol.PlanModeSchemas()) {
		t.Fatal("source schema/section drift")
	}
	for _, sample := range f.Cases {
		t.Run(sample.Name, func(t *testing.T) {
			cfg := runtimeConfig(t.TempDir(), &FakeProvider{})
			cfg.PlanModeTools = true
			cfg.Mode = ModeAuto
			if sample.Name == "readonly" {
				cfg.Mode = ModeReadonly
			}
			if sample.Name == "fixed" {
				cfg.SystemBuilder = FixedSystem("fixed")
			}
			var reviews []string
			if sample.Name == "approve" || sample.Name == "reject" {
				cfg.PlanApprover = planApproverFunc(func(_ context.Context, r PlanReviewRequest) (PlanReview, error) {
					if r.Authority.SessionID != cfg.ID || r.Authority.OwnerID != cfg.Owner || r.Authority.ToolUseID == "" {
						t.Fatal(r)
					}
					reviews = append(reviews, r.Plan)
					return PlanReview{Approved: sample.Name == "approve", Feedback: "split step 2 into smaller pieces"}, nil
				})
			}
			s, err := NewRuntimeSession(cfg)
			if err != nil {
				t.Fatal(err)
			}
			before, _ := s.gate.catalog.Snapshot()
			for _, v := range f.Metadata {
				d, ok := s.gate.catalog.Lookup(v.Name)
				if !ok || d.Risk() != v.Risk || d.Readonly() != v.Readonly || d.ParallelSafe() != v.ParallelSafe || !slices.Equal(d.Capabilities(), v.Capabilities) || d.ExecutionMode(ToolCall{}) != ExecutionExclusive {
					t.Fatal(v)
				}
			}
			for _, v := range f.Variants {
				input, err := protocol.DecodeToolInput(v.Name, v.Input)
				if err != nil {
					t.Fatal(err)
				}
				canonical, err := input.CanonicalJSON()
				if err != nil || canonical != v.Canonical || !slices.Equal(DefaultGrantCandidate(input).Tokens(), v.Candidate) || !slices.Equal(ProposedGrantCandidate(input).Tokens(), v.Proposed) {
					t.Fatal(v, canonical, err)
				}
			}
			for i, row := range sample.Steps {
				input, err := protocol.DecodeToolInput(row.Name, row.Input)
				if err != nil {
					t.Fatal(err)
				}
				out, err := planDispatch(context.Background(), s, fmt.Sprint(i), input)
				if err != nil {
					t.Fatal(err)
				}
				request, _, err := s.buildRequest()
				if err != nil {
					t.Fatal(err)
				}
				var events []bool
				for _, r := range s.Events() {
					if v, ok := r.Event.PlanMode(); ok {
						events = append(events, v.Active)
					}
				}
				after, _ := s.gate.catalog.Snapshot()
				if out.Output != row.Output || s.PlanModeActive() != row.Active || strings.Contains(*request.System, PlanSection) != row.Section || !slices.Equal(events, row.Events) || (before.Fingerprint() == after.Fingerprint()) != row.CatalogStable || out.IsError() != strings.HasPrefix(row.Output, "Error") {
					t.Fatalf("step %d: %+v, active %v, section %v, events %v", i, out, s.PlanModeActive(), strings.Contains(*request.System, PlanSection), events)
				}
			}
			if !slices.Equal(reviews, sample.Reviews) {
				t.Fatal(reviews, sample.Reviews)
			}
		})
	}
}

func TestPlanModeSoftGuidanceAndMasking(t *testing.T) {
	for _, mode := range []PermissionMode{ModeAuto, ModeReadonly} {
		t.Run(string(mode), func(t *testing.T) {
			cfg := runtimeConfig(t.TempDir(), &FakeProvider{})
			cfg.Mode = mode
			cfg.PlanModeTools = true
			registry := secrets.New(secrets.Config{})
			registry.RegisterValue("KEY", "plan-private-secret")
			cfg.Secrets = registry
			cfg.PlanApprover = planApproverFunc(func(_ context.Context, r PlanReviewRequest) (PlanReview, error) {
				if r.Plan != "# plan-private-secret" {
					t.Fatal(r.Plan)
				}
				return PlanReview{Feedback: "revise plan-private-secret"}, nil
			})
			s, err := NewRuntimeSession(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if out, err := planDispatch(context.Background(), s, "enter", protocol.EnterPlanModeToolInput()); err != nil || out.IsError() {
				t.Fatal(out, err)
			}
			out, err := planDispatch(context.Background(), s, "write", protocol.WriteFileToolInput(protocol.WriteFileInput{Path: "result", Content: "written"}))
			_, statErr := os.Stat(cfg.Workspace + "/result")
			if err != nil || out.Denied != (mode == ModeReadonly) || (statErr == nil) != (mode == ModeAuto) || !s.PlanModeActive() {
				t.Fatal(out, err, statErr)
			}
			out, err = planDispatch(context.Background(), s, "reject", protocol.ExitPlanModeToolInput(protocol.ExitPlanModeInput{Plan: "# plan-private-secret"}))
			if err != nil || !out.IsError() || strings.Contains(out.Output, "plan-private-secret") || !s.PlanModeActive() {
				t.Fatal(out, err)
			}
			foreign := ToolAuthority{SessionID: s.id, OwnerID: "foreign", Workspace: s.executionRoot(), Mode: mode}
			out, err = s.gate.Dispatch(context.Background(), foreign, ToolCall{ID: "foreign", Input: protocol.ExitPlanModeToolInput(protocol.ExitPlanModeInput{Plan: "# nope"})})
			if err == nil && !out.IsError() {
				t.Fatal("foreign reviewer call admitted")
			}
		})
	}
}

func TestPlanModeReviewerFaultsAndCancelledApprovalRetainState(t *testing.T) {
	for _, fault := range []string{"error", "panic", "cancel"} {
		t.Run(fault, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cfg := runtimeConfig(t.TempDir(), &FakeProvider{})
			cfg.PlanModeTools = true
			cfg.PlanApprover = planApproverFunc(func(_ context.Context, _ PlanReviewRequest) (PlanReview, error) {
				switch fault {
				case "panic":
					panic("unavailable")
				case "cancel":
					cancel()
					return PlanReview{Approved: true}, nil
				default:
					return PlanReview{}, errors.New("unavailable")
				}
			})
			s, err := NewRuntimeSession(cfg)
			if err != nil {
				t.Fatal(err)
			}
			planDispatch(ctx, s, "enter", protocol.EnterPlanModeToolInput())
			out, err := planDispatch(ctx, s, "exit", protocol.ExitPlanModeToolInput(protocol.ExitPlanModeInput{Plan: "# Complete"}))
			if !s.PlanModeActive() || len(s.Events()) != 1 || (err == nil && !out.IsError()) {
				t.Fatal(out, err, s.Events())
			}
			if fault == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		})
	}
}

func TestPlanModeLoggedRestoreAndPendingReload(t *testing.T) {
	f := planFixture(t)
	for _, row := range f.Restored {
		t.Run(fmt.Sprint(row.Active), func(t *testing.T) {
			store := newRuntimeStateStore()
			saved := seedRestore(t, store, []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent("prior")}, {Role: protocol.RoleAssistant, Content: protocol.PlainContent("remembered")}})
			saved.System = nil
			store.UpsertSession(context.Background(), saved)
			for i, active := range []bool{false, true, row.Active} {
				store.AppendEvent(context.Background(), saved.SessionID, SessionEventRecord{Sequence: EventSequence(10 + i), SessionID: saved.SessionID, TranscriptEpoch: 2, Event: SessionEvent{kind: EventPlanMode, planMode: PlanModeEvent{active}}})
			}
			var sections []bool
			provider := stateProviderFunc(func(_ context.Context, r protocol.ModelRequest) (protocol.ModelReply, error) {
				sections = append(sections, r.System != nil && strings.Contains(*r.System, PlanSection))
				return stateFinal(), nil
			})
			cfg := managerTestConfig(t.TempDir(), provider)
			cfg.Services.StateStore = store
			cfg.Services.PlanModeTools = true
			m := makeManager(t, cfg)
			s := restoreOne(t, m)
			if s.core.PlanModeActive() != row.Folded || s.Info().Busy {
				t.Fatal("fold did not restore idle facts")
			}
			if _, err := s.Run(context.Background(), "continue"); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(sections, row.RequestSections) {
				t.Fatal(sections, row.RequestSections)
			}
			child, err := m.Fork(context.Background(), saved.Owner, s.ID())
			if err != nil {
				t.Fatal(err)
			}
			if child.core.PlanModeActive() {
				t.Fatal("fork inherited plan state")
			}
		})
	}
	store := newRuntimeStateStore()
	saved := seedRestore(t, store, nil)
	store.holders[saved.SessionID] = "foreign"
	store.AppendEvent(context.Background(), saved.SessionID, SessionEventRecord{Sequence: 10, SessionID: saved.SessionID, TranscriptEpoch: 2, Event: SessionEvent{kind: EventPlanMode, planMode: PlanModeEvent{true}}})
	m := restoreManager(t, store, &FakeProvider{})
	s := restoreOne(t, m)
	if !s.core.PlanModeActive() || !s.PersistenceStatus().RestorePending {
		t.Fatal(s.PersistenceStatus())
	}
	store.mu.Lock()
	delete(store.holders, saved.SessionID)
	store.mu.Unlock()
	store.AppendEvent(context.Background(), saved.SessionID, SessionEventRecord{Sequence: 11, SessionID: saved.SessionID, TranscriptEpoch: 2, Event: SessionEvent{kind: EventPlanMode, planMode: PlanModeEvent{false}}})
	if _, err := s.Run(context.Background(), "claim and reload"); err != nil {
		t.Fatal(err)
	}
	if s.core.PlanModeActive() {
		t.Fatal("pending state did not fold latest claimed log")
	}
}

func TestPlanModeModelRoundPersistenceAndEventCatchup(t *testing.T) {
	store := newRuntimeStateStore()
	step := 0
	var active []bool
	var fingerprint string
	p := stateProviderFunc(func(_ context.Context, r protocol.ModelRequest) (protocol.ModelReply, error) {
		active = append(active, strings.Contains(*r.System, PlanSection))
		encoded, _ := json.Marshal(r.Tools)
		if fingerprint == "" {
			fingerprint = string(encoded)
		} else if fingerprint != string(encoded) {
			t.Fatal("plan changed catalog")
		}
		for _, v := range store.events["session"] {
			if v.Event.Kind() == EventPlanMode && v.TranscriptEpoch < 1 {
				t.Fatal(v)
			}
		}
		step++
		switch step {
		case 1:
			return fakeReply([]protocol.Block{protocol.NewToolUse("enter", protocol.EnterPlanModeToolInput())}, protocol.StopToolUse), nil
		case 2:
			if !FoldPlanMode(store.events["session"]) {
				t.Fatal("state flip not persisted before request")
			}
			return fakeReply([]protocol.Block{protocol.NewToolUse("exit", protocol.ExitPlanModeToolInput(protocol.ExitPlanModeInput{Plan: "# Execute"}))}, protocol.StopToolUse), nil
		default:
			return stateFinal(), nil
		}
	})
	cfg := runtimeConfig(t.TempDir(), p)
	cfg.PlanModeTools = true
	cfg.MaxRounds = 4
	cfg.StateStore = store
	cfg.StateLeaseOwner = "process"
	cfg.StateLeaseTTL = time.Minute
	s, err := NewManagedSession(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Run(context.Background(), "plan and present"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(active, []bool{false, true, false}) || s.core.PlanModeActive() {
		t.Fatal(active)
	}
	if err := protocol.ValidateTranscript(s.Messages()); err != nil {
		t.Fatal(err)
	}
	catchup, err := s.CatchUpEvents(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	var flips []bool
	for _, r := range catchup {
		if v, ok := r.Event.PlanMode(); ok {
			flips = append(flips, v.Active)
		}
	}
	if !slices.Equal(flips, []bool{true, false}) {
		t.Fatal(flips)
	}
}

func TestPlanModeArchivedBooleanAndFold(t *testing.T) {
	var rows []SessionEventRecord
	for _, active := range []bool{true, false, true} {
		row := assertStoredEventRoundTrip(t, []byte(fmt.Sprintf(`{"type":"plan_mode","active":%v,"seq":1,"ts":1,"session":"saved","transcript_epoch":1}`, active)))
		rows = append(rows, row)
	}
	if !FoldPlanMode(rows) || FoldPlanMode(rows[:2]) || FoldPlanMode(nil) {
		t.Fatal("fold")
	}
	for _, raw := range []string{`{}`, `{"active":null}`, `{"active":"yes"}`, `{"active":1}`} {
		raw = strings.TrimSuffix(raw, "}") + `,"type":"plan_mode","seq":1,"ts":1,"session":"saved","transcript_epoch":1}`
		if _, err := DecodeStoredEvent([]byte(raw)); err == nil {
			t.Fatal(raw)
		}
	}
}

func TestPlanModeSelectedChildHasFreshStateAndBoundReviewer(t *testing.T) {
	stage := 0
	reviews := 0
	p := stateProviderFunc(func(_ context.Context, r protocol.ModelRequest) (protocol.ModelReply, error) {
		stage++
		switch stage {
		case 1:
			return fakeReply([]protocol.Block{protocol.NewToolUse("child-enter", protocol.EnterPlanModeToolInput())}, protocol.StopToolUse), nil
		case 2:
			return fakeReply([]protocol.Block{protocol.NewToolUse("child-exit", protocol.ExitPlanModeToolInput(protocol.ExitPlanModeInput{Plan: "# Child"}))}, protocol.StopToolUse), nil
		default:
			return stateFinal(), nil
		}
	})
	cfg := runtimeConfig(t.TempDir(), p)
	cfg.PlanModeTools = true
	cfg.RoleToolPolicy = allRoleTools{}
	cfg.PlanApprover = planApproverFunc(func(_ context.Context, r PlanReviewRequest) (PlanReview, error) {
		if r.Authority.SessionID == cfg.ID || r.Authority.OwnerID != cfg.Owner || !strings.Contains(string(r.Authority.SessionID), ">") || r.Plan != "# Child" {
			t.Fatal(r)
		}
		reviews++
		return PlanReview{Approved: true}, nil
	})
	s, err := NewRuntimeSession(cfg)
	if err != nil {
		t.Fatal(err)
	}
	planDispatch(context.Background(), s, "parent-enter", protocol.EnterPlanModeToolInput())
	if _, err = s.Delegate(context.Background(), "plan independently", RoleExplore); err != nil {
		t.Fatal(err)
	}
	if reviews != 1 || !s.PlanModeActive() {
		t.Fatal(reviews, s.PlanModeActive())
	}
	var childFlips []bool
	for _, r := range s.Events() {
		if v, ok := r.Event.PlanMode(); ok && r.Scope.Depth == 1 {
			childFlips = append(childFlips, v.Active)
		}
	}
	if !slices.Equal(childFlips, []bool{true, false}) {
		t.Fatal(childFlips)
	}
}
