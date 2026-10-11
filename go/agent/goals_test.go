package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/secrets"
)

// Raw JSON is confined to the external differential fixture boundary.
type goalFixture struct {
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
		Mode  PermissionMode
		Steps []struct {
			Authority             string
			Name                  protocol.ToolName
			Input                 json.RawMessage
			Output                *string
			Failed, Denied, Armed bool
			Goal                  *GoalRecord
			Events                []GoalChangeEvent
			CatalogStable         bool `json:"catalog_stable"`
		}
	}
	Loops []struct {
		Custom   bool
		Requests int
		Goal     GoalRecord
		Armed    bool
	}
	Restored struct {
		Before, After struct {
			Goal  GoalRecord
			Armed bool
		}
		UntrustedRequests int `json:"untrusted_requests"`
		HumanRequests     int `json:"human_requests"`
	}
}

func readGoalFixture(t *testing.T) goalFixture {
	t.Helper()
	b, e := os.ReadFile("../testdata/python-goals.json")
	if e != nil {
		t.Fatal(e)
	}
	var f goalFixture
	if e = json.Unmarshal(b, &f); e != nil {
		t.Fatal(e)
	}
	return f
}
func goalHuman(t *testing.T) RunContext {
	t.Helper()
	v, e := ExplicitHumanRunContext(HumanRunConfig{})
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func goalDispatch(t *testing.T, s *Session, run RunContext, input protocol.ToolInput) ToolOutcome {
	t.Helper()
	v, e := s.gate.Dispatch(context.Background(), ToolAuthority{SessionID: s.id, OwnerID: s.owner, Workspace: s.executionRoot(), Mode: s.permissionMode(), RunContext: run}, ToolCall{ID: fmt.Sprint(len(s.Events())), Input: input})
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func goalEvents(s *Session) []GoalChangeEvent {
	var out []GoalChangeEvent
	for _, r := range s.Events() {
		if v, ok := r.Event.GoalChange(); ok {
			out = append(out, v)
		}
	}
	return out
}
func TestGoalsActualPythonToolsAndContinuation(t *testing.T) {
	f := readGoalFixture(t)
	if !reflect.DeepEqual(f.Schemas, protocol.GoalSchemas()) {
		t.Fatal("schema drift")
	}
	for _, scenario := range f.Cases {
		t.Run(string(scenario.Mode), func(t *testing.T) {
			cfg := runtimeConfig(t.TempDir(), &FakeProvider{})
			cfg.GoalTools = true
			cfg.Mode = scenario.Mode
			s, e := NewRuntimeSession(cfg)
			if e != nil {
				t.Fatal(e)
			}
			before, _ := s.gate.catalog.Snapshot()
			for _, m := range f.Metadata {
				d, ok := s.gate.catalog.Lookup(m.Name)
				if !ok || d.Risk() != m.Risk || d.Readonly() != m.Readonly || d.ParallelSafe() != m.ParallelSafe || !slices.Equal(d.Capabilities(), m.Capabilities) || d.ExecutionMode(ToolCall{}) != ExecutionExclusive {
					t.Fatal(m)
				}
			}
			for _, v := range f.Variants {
				input, e := protocol.DecodeToolInput(v.Name, v.Input)
				if e != nil {
					t.Fatal(e)
				}
				canonical, e := input.CanonicalJSON()
				if e != nil || canonical != v.Canonical || !slices.Equal(DefaultGrantCandidate(input).Tokens(), v.Candidate) || !slices.Equal(ProposedGrantCandidate(input).Tokens(), v.Proposed) {
					t.Fatal(v, canonical, e)
				}
			}
			aliases := map[GoalID]GoalID{}
			for i, row := range scenario.Steps {
				run := RunContext{}
				if row.Authority == "human" {
					run = goalHuman(t)
				} else if row.Authority == "peer" {
					run, e = goalHuman(t).DerivePeerAgent("parent")
					if e != nil {
						t.Fatal(e)
					}
				}
				var output *string
				if row.Name == "stop" {
					output, e = (GoalContinuation{}).Stop(context.Background(), StopContext{session: s})
					if e != nil {
						t.Fatal(e)
					}
				} else {
					input, e := protocol.DecodeToolInput(row.Name, row.Input)
					if e != nil {
						t.Fatal(e)
					}
					v := goalDispatch(t, s, run, input)
					output = &v.Output
					if v.Failed != row.Failed || v.Denied != row.Denied {
						t.Fatalf("step %d telemetry %+v want failed=%v denied=%v", i, v, row.Failed, row.Denied)
					}
				}
				snapshot := s.GoalSnapshot()
				if snapshot.Goal != nil {
					if _, ok := aliases[snapshot.Goal.ID]; !ok {
						aliases[snapshot.Goal.ID] = GoalID(fmt.Sprintf("<goal%d>", len(aliases)+1))
					}
					snapshot.Goal.ID = aliases[snapshot.Goal.ID]
				}
				if output != nil {
					v := *output
					for id, alias := range aliases {
						v = strings.ReplaceAll(v, string(id), string(alias))
					}
					output = &v
				}
				events := goalEvents(s)
				for j := range events {
					if events[j].Goal != nil {
						events[j].Goal.ID = aliases[events[j].Goal.ID]
					}
				}
				after, _ := s.gate.catalog.Snapshot()
				if !reflect.DeepEqual(output, row.Output) || !reflect.DeepEqual(snapshot.Goal, row.Goal) || snapshot.Armed != row.Armed || !reflect.DeepEqual(events, emptyGoalEvents(row.Events)) || (before.Fingerprint() == after.Fingerprint()) != row.CatalogStable {
					t.Fatalf("step %d output=%v goal=%+v armed=%v events=%+v want %+v", i, output, snapshot.Goal, snapshot.Armed, events, row)
				}
			}
		})
	}
}
func emptyGoalEvents(v []GoalChangeEvent) []GoalChangeEvent {
	if len(v) == 0 {
		return nil
	}
	return v
}
func TestGoalDefaultStopConsumerAndExplicitReplacement(t *testing.T) {
	for _, row := range readGoalFixture(t).Loops {
		t.Run(fmt.Sprint(row.Custom), func(t *testing.T) {
			calls := 0
			cfg := managerTestConfig(t.TempDir(), stateProviderFunc(func(_ context.Context, r protocol.ModelRequest) (protocol.ModelReply, error) {
				calls++
				return stateFinal(), nil
			}))
			cfg.Services.GoalTools = true
			cfg.Defaults.MaxRounds = 10
			cfg.Services.StuckDetector = NullStuckDetector{}
			if row.Custom {
				cfg.Services.StopHooks = []StopHook{}
			}
			m := makeManager(t, cfg)
			s := createManaged(t, m, CreateSessionRequest{Owner: "owner"})
			goal := GoalRecord{ID: "goal_saved", Revision: 1, Objective: "finish", Phase: GoalActive, MaxRounds: 2}
			s.core.goals.current = &goal
			s.core.goals.armed = true
			if _, e := s.RunWithContext(context.Background(), "begin", goalHuman(t)); e != nil {
				t.Fatal(e)
			}
			got := s.GoalSnapshot()
			if calls != row.Requests || got.Goal == nil || !reflect.DeepEqual(*got.Goal, row.Goal) || got.Armed != row.Armed {
				t.Fatal(calls, got, row)
			}
		})
	}
}
func TestGoalConcurrentCASDetachedReadsAndOverflow(t *testing.T) {
	cfg := runtimeConfig(t.TempDir(), &FakeProvider{})
	cfg.Mode = ModeAuto
	cfg.GoalTools = true
	s, e := NewRuntimeSession(cfg)
	if e != nil {
		t.Fatal(e)
	}
	human := goalHuman(t)
	goalDispatch(t, s, human, protocol.CreateGoalToolInput(protocol.CreateGoalInput{Objective: "work"}))
	var wg sync.WaitGroup
	outputs := make(chan ToolOutcome, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			v, e := s.gate.Dispatch(context.Background(), ToolAuthority{SessionID: s.id, OwnerID: s.owner, Workspace: s.executionRoot(), Mode: ModeAuto, RunContext: human}, ToolCall{ID: fmt.Sprint(i), Input: protocol.CompleteGoalToolInput(protocol.GoalReferenceInput{Revision: 1})})
			if e != nil {
				t.Error(e)
			}
			outputs <- v
			_ = s.GoalSnapshot()
		}(i)
	}
	wg.Wait()
	close(outputs)
	success := 0
	for v := range outputs {
		if !strings.HasPrefix(v.Output, "Error:") {
			success++
		}
	}
	if success != 1 || len(goalEvents(s)) != 2 {
		t.Fatal(success, len(goalEvents(s)))
	}
	saved := s.GoalSnapshot()
	saved.Goal.Objective = "changed"
	if s.GoalSnapshot().Goal.Objective != "work" {
		t.Fatal("snapshot aliases live state")
	}
	s.goals.mu.Lock()
	s.goals.current.Revision = GoalRevision(math.MaxInt64)
	s.goals.current.Phase = GoalActive
	s.goals.armed = true
	s.goals.mu.Unlock()
	out := goalDispatch(t, s, human, protocol.CompleteGoalToolInput(protocol.GoalReferenceInput{Revision: GoalRevision(math.MaxInt64)}))
	if !out.Failed || s.GoalSnapshot().Goal.Revision != GoalRevision(math.MaxInt64) {
		t.Fatal(out)
	}
	if _, e = (GoalContinuation{}).Stop(context.Background(), StopContext{session: s}); e == nil {
		t.Fatal("overflow wrapped")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e = (GoalContinuation{}).Stop(ctx, StopContext{session: s}); e == nil {
		t.Fatal("cancelled stop")
	}
	if v, e := (GoalContinuation{}).Stop(context.Background(), StopContext{}); v != nil || e != nil {
		t.Fatal("unbound hook")
	}
}
func TestGoalPersistenceRestoreIsDisarmedAndTrustedResumeIsRequired(t *testing.T) {
	f := readGoalFixture(t).Restored
	store := newRuntimeStateStore()
	saved := seedRestore(t, store, nil)
	store.AppendEvent(context.Background(), saved.SessionID, SessionEventRecord{Sequence: 10, SessionID: saved.SessionID, TranscriptEpoch: 2, Event: SessionEvent{kind: EventGoalChange, goalChange: GoalChangeEvent{Operation: GoalResumeOperation, Goal: &f.Before.Goal}}})
	count := 0
	cfg := managerTestConfig(t.TempDir(), stateProviderFunc(func(context.Context, protocol.ModelRequest) (protocol.ModelReply, error) {
		count++
		return stateFinal(), nil
	}))
	cfg.Services.StateStore = store
	cfg.Services.GoalTools = true
	cfg.Services.StuckDetector = NullStuckDetector{}
	m := makeManager(t, cfg)
	s := restoreOne(t, m)
	before := s.GoalSnapshot()
	if before.Armed || before.Goal == nil || !reflect.DeepEqual(*before.Goal, f.Before.Goal) || s.Info().Busy {
		t.Fatal(before)
	}
	if _, e := s.Run(context.Background(), "continue"); e != nil {
		t.Fatal(e)
	}
	if count != f.UntrustedRequests {
		t.Fatal(count)
	}
	goalDispatch(t, s.core, goalHuman(t), protocol.ResumeGoalToolInput(protocol.GoalReferenceInput{Revision: 5}))
	if _, e := s.RunWithContext(context.Background(), "continue", goalHuman(t)); e != nil {
		t.Fatal(e)
	}
	after := s.GoalSnapshot()
	if count-f.UntrustedRequests != f.HumanRequests || after.Armed || !reflect.DeepEqual(*after.Goal, f.After.Goal) {
		t.Fatal(count, after)
	}
	child, e := m.Fork(context.Background(), saved.Owner, s.ID())
	if e != nil {
		t.Fatal(e)
	}
	if child.GoalSnapshot().Goal != nil || child.GoalSnapshot().Armed {
		t.Fatal("fork inherited goal")
	}
	rows, e := store.LoadEvents(context.Background(), saved.SessionID, 0, nil)
	if e != nil {
		t.Fatal(e)
	}
	folded := FoldGoal(rows)
	if folded == nil || !reflect.DeepEqual(*folded, *after.Goal) {
		t.Fatal("persisted fold", folded)
	}
}
func TestGoalPendingRestoreReloadsClearAndArchivalValidation(t *testing.T) {
	store := newRuntimeStateStore()
	saved := seedRestore(t, store, nil)
	store.holders[saved.SessionID] = "foreign"
	goal := GoalRecord{ID: "goal_saved", Revision: 1, Phase: GoalActive, MaxRounds: 2}
	record := SessionEventRecord{Sequence: 10, SessionID: saved.SessionID, TranscriptEpoch: 2, Event: SessionEvent{kind: EventGoalChange, goalChange: GoalChangeEvent{Operation: GoalCreateOperation, Goal: &goal}}}
	store.AppendEvent(context.Background(), saved.SessionID, record)
	m := restoreManager(t, store, &FakeProvider{})
	s := restoreOne(t, m)
	if s.GoalSnapshot().Armed || s.GoalSnapshot().Goal == nil || !s.PersistenceStatus().RestorePending {
		t.Fatal("pending restore")
	}
	store.mu.Lock()
	delete(store.holders, saved.SessionID)
	store.mu.Unlock()
	record.Sequence = 11
	record.Event.goalChange = GoalChangeEvent{Operation: GoalClearOperation}
	store.AppendEvent(context.Background(), saved.SessionID, record)
	if _, e := s.Run(context.Background(), "claim"); e != nil {
		t.Fatal(e)
	}
	if s.GoalSnapshot().Goal != nil {
		t.Fatal("clear ignored")
	}
	record.Event.goalChange = GoalChangeEvent{Operation: GoalCreateOperation, Goal: &goal}
	b, e := json.Marshal(record)
	if e != nil {
		t.Fatal(e)
	}
	decoded, e := DecodeStoredEvent(b)
	if e != nil {
		t.Fatal(e)
	}
	v, ok := decoded.Event.GoalChange()
	if !ok || !reflect.DeepEqual(*v.Goal, goal) {
		t.Fatal(v, e)
	}
	for _, change := range []GoalChangeEvent{{Operation: "unknown", Goal: &goal}, {Operation: GoalCreateOperation}, {Operation: GoalCreateOperation, Goal: &GoalRecord{ID: "bad", Revision: 1, Phase: GoalBlocked, MaxRounds: 2}}} {
		if e = change.Validate(); e == nil {
			t.Fatal("invalid archive accepted", change)
		}
	}
	for _, invalid := range []string{strings.Replace(string(b), `"phase":"active"`, `"phase":"unknown"`, 1), strings.Replace(string(b), `"max_rounds":2`, `"max_rounds":0`, 1), strings.Replace(string(b), `"revision":1`, `"revision":-1`, 1)} {
		if _, err := DecodeStoredEvent([]byte(invalid)); err == nil {
			t.Fatal("invalid archived goal accepted", invalid)
		}
	}
	record.Event.goalChange = GoalChangeEvent{Operation: GoalClearOperation}
	if FoldGoal([]SessionEventRecord{decoded, record}) != nil {
		t.Fatal("tombstone ignored")
	}
}
func TestGoalLiveRawAndMaskedEventsAndChildren(t *testing.T) {
	registry := secrets.New(secrets.Config{})
	registry.RegisterValue("SECRET", "sensitive-value")
	cfg := runtimeConfig(t.TempDir(), &FakeProvider{})
	cfg.GoalTools = true
	cfg.Mode = ModeAuto
	cfg.Secrets = registry
	s, e := NewRuntimeSession(cfg)
	if e != nil {
		t.Fatal(e)
	}
	out := goalDispatch(t, s, goalHuman(t), protocol.CreateGoalToolInput(protocol.CreateGoalInput{Objective: "sensitive-value"}))
	if strings.Contains(out.Output, "sensitive-value") || s.GoalSnapshot().Goal.Objective != "sensitive-value" {
		t.Fatal(out)
	}
	raw := s.Events()[0]
	for _, r := range s.Events() {
		if r.Event.Kind() == EventGoalChange {
			raw = r
		}
	}
	projected := maskedEvent(registry, raw.Event)
	b, e := json.Marshal(projected)
	if e != nil || strings.Contains(string(b), "sensitive-value") {
		t.Fatal(string(b), e)
	}
	goalDispatch(t, s, RunContext{}, protocol.BlockGoalToolInput(protocol.BlockGoalInput{Revision: 1, Code: "private-goal", Message: "sensitive-value"}))
	blocked := s.GoalSnapshot()
	blocked.Goal.Blocked.Message = "caller mutation"
	if s.GoalSnapshot().Goal.Blocked.Message != "sensitive-value" {
		t.Fatal("blocked pointer aliases live goal")
	}
	for _, event := range s.Events() {
		if event.Event.Kind() == EventGoalChange {
			v, e := json.Marshal(maskedEvent(registry, event.Event))
			if e != nil || strings.Contains(string(v), "sensitive-value") {
				t.Fatal(string(v), e)
			}
		}
	}
	if len(protocol.DefaultToolNames()) != 10 {
		t.Fatal("default catalog changed")
	}
	// Capability-free goal tools are excluded by the default role policy.
	def, ok := s.gate.catalog.Lookup(protocol.ToolGoalCreate)
	if !ok {
		t.Fatal("selected tool absent")
	}
	if len(def.Capabilities()) != 0 {
		t.Fatal(def.Capabilities())
	}
}

func TestGoalModelTurnPersistsBeforeContinuationAndKeepsRunProvenance(t *testing.T) {
	store := newRuntimeStateStore()
	calls := 0
	var requests []protocol.ModelRequest
	provider := stateProviderFunc(func(_ context.Context, r protocol.ModelRequest) (protocol.ModelReply, error) {
		calls++
		requests = append(requests, r.Clone())
		snapshot, _ := store.LoadEvents(context.Background(), "session", 0, nil)
		if calls == 2 {
			g := FoldGoal(snapshot)
			if g == nil || g.Revision != 1 || g.RoundsStarted != 0 {
				t.Fatal("create not captured before next request", g)
			}
		}
		if calls == 3 {
			g := FoldGoal(snapshot)
			if g == nil || g.Revision != 2 || g.RoundsStarted != 1 {
				t.Fatal("round not captured before next request", g)
			}
			return fakeReply([]protocol.Block{protocol.NewToolUse("complete", protocol.CompleteGoalToolInput(protocol.GoalReferenceInput{Revision: 2}))}, protocol.StopToolUse), nil
		}
		if calls == 1 {
			return fakeReply([]protocol.Block{protocol.NewToolUse("create", protocol.CreateGoalToolInput(protocol.CreateGoalInput{Objective: "finish"}))}, protocol.StopToolUse), nil
		}
		return stateFinal(), nil
	})
	cfg := runtimeConfig(t.TempDir(), provider)
	cfg.Mode = ModeAuto
	cfg.MaxRounds = 10
	cfg.GoalTools = true
	cfg.StateStore = store
	cfg.StateLeaseOwner = "process"
	cfg.StuckDetector = NullStuckDetector{}
	managed, e := NewManagedSession(cfg)
	if e != nil {
		t.Fatal(e)
	}
	s := managed.core
	if _, e = managed.RunWithContext(context.Background(), "begin", goalHuman(t)); e != nil {
		t.Fatal(e)
	}
	goal := s.GoalSnapshot()
	if calls != 4 || goal.Armed || goal.Goal.Phase != GoalComplete || goal.Goal.RoundsStarted != 1 {
		t.Fatal(calls, goal)
	}
	var contextIDs []MessageID
	for _, event := range s.Events() {
		if _, ok := event.Event.ToolUse(); ok {
			if event.Scope.RunContext.Authority() != AuthorityExplicitHuman {
				t.Fatal("continuation changed source provenance")
			}
			contextIDs = append(contextIDs, event.Scope.RunContext.MessageID())
		}
	}
	if len(contextIDs) != 2 || contextIDs[0] != contextIDs[1] {
		t.Fatal(contextIDs)
	}
	// The goal counter records a requested continuation even if the global loop
	// budget prevents its next model request, as the source stop ordering does.
	cfg.StateStore = nil
	cfg.StateLeaseOwner = ""
	cfg.MaxRounds = 1
	cfg.Provider = stateProviderFunc(func(context.Context, protocol.ModelRequest) (protocol.ModelReply, error) { return stateFinal(), nil })
	one, e := NewRuntimeSession(cfg)
	if e != nil {
		t.Fatal(e)
	}
	goalDispatch(t, one, goalHuman(t), protocol.CreateGoalToolInput(protocol.CreateGoalInput{Objective: "work"}))
	if _, e = one.Run(context.Background(), "one request"); e != nil {
		t.Fatal(e)
	}
	if one.GoalSnapshot().Goal.RoundsStarted != 1 {
		t.Fatal("attempt counter semantics changed")
	}
}

func TestGoalSelectedChildReadsFreshStateAndCannotInheritHumanArming(t *testing.T) {
	calls := 0
	provider := stateProviderFunc(func(_ context.Context, r protocol.ModelRequest) (protocol.ModelReply, error) {
		calls++
		switch calls {
		case 1:
			return fakeReply([]protocol.Block{protocol.NewToolUse("status", protocol.GoalStatusToolInput())}, protocol.StopToolUse), nil
		case 2:
			blocks, _ := r.Messages[len(r.Messages)-1].Content.Blocks()
			result, _ := blocks[0].ToolResult()
			if result.Content != "No current goal." {
				t.Fatal(result)
			}
			return fakeReply([]protocol.Block{protocol.NewToolUse("create", protocol.CreateGoalToolInput(protocol.CreateGoalInput{Objective: "child"}))}, protocol.StopToolUse), nil
		case 3:
			blocks, _ := r.Messages[len(r.Messages)-1].Content.Blocks()
			result, _ := blocks[0].ToolResult()
			if result.Content != goalHumanRefusal {
				t.Fatal(result)
			}
		}
		return stateFinal(), nil
	})
	cfg := runtimeConfig(t.TempDir(), provider)
	cfg.Mode = ModeAuto
	cfg.GoalTools = true
	cfg.RoleToolPolicy = allRoleTools{}
	cfg.SubagentMaxRounds = 5
	cfg.StuckDetector = NullStuckDetector{}
	s, e := NewRuntimeSession(cfg)
	if e != nil {
		t.Fatal(e)
	}
	goalDispatch(t, s, goalHuman(t), protocol.CreateGoalToolInput(protocol.CreateGoalInput{Objective: "parent"}))
	before := s.GoalSnapshot()
	s.currentRun = goalHuman(t)
	if _, e = s.Delegate(context.Background(), "independent", RoleGeneralPurpose); e != nil {
		t.Fatal(e)
	}
	after := s.GoalSnapshot()
	if !reflect.DeepEqual(before, after) || calls != 3 {
		t.Fatal("child changed parent goal", after, calls)
	}
	for _, r := range s.Events() {
		if r.Scope.Depth == 1 && r.Event.Kind() == EventGoalChange {
			t.Fatal("child armed goal")
		}
	}
}
