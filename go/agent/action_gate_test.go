package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/workspace"
)

func TestJournalSeesFinalRewriteAndRecordsBeforeObserver(t *testing.T) {
	ctx := context.Background()
	journal, _ := NewInMemoryActionJournal(2)
	handler := &gateHandler{output: "raw"}
	var observed ActionRecord
	hooks := GateHooks{
		Before: []BeforeHook{beforeHookFunc(func(_ context.Context, authority ToolAuthority, _ ToolCall) (BeforeDecision, error) {
			if authority.ActionID == "" {
				t.Fatal("missing pre-hook action identity")
			}
			return RewriteToolCall(protocol.BashToolInput(protocol.BashInput{Command: "echo rewritten"})), nil
		})},
		After: []AfterHook{afterHookFunc(func(_ context.Context, _ ToolAuthority, _ ToolCall, value string) (string, error) {
			return "post:" + value, nil
		})},
		Observers: []ResultObserver{observerFunc(func(ctx context.Context, authority ToolAuthority, _ ToolCall, outcome ToolOutcome) error {
			record, exists, err := journal.Get(ctx, authority.ActionID)
			if err != nil || !exists || record.Status != ActionCompleted {
				t.Fatal("observer ran before settlement", err)
			}
			observed = record
			if outcome.ActionID != authority.ActionID {
				t.Fatal("wrong observer identity")
			}
			return nil
		})},
	}
	gate, _ := NewJournaledToolGate(gateTestCatalog(t, handler), DefaultPermissionPolicy(nil), hooks, journal)
	authority := gateTestAuthority(ModeAuto)
	authority.RunContext = actionContext(t)
	outcome, err := gate.Dispatch(ctx, authority, gateTestCall("echo original"))
	if err != nil || outcome.Output != "post:raw" {
		t.Fatal(outcome, err)
	}
	candidate, _ := actionCandidate(ActionRequest{outcome.ActionID, "s", "m", gateTestCall("").ID, protocol.BashToolInput(protocol.BashInput{Command: "echo rewritten"})})
	if observed.InputHash != candidate.InputHash || *observed.Result != "post:raw" {
		t.Fatal("journal stored original arguments or pre-hook result")
	}
	replay, err := gate.Dispatch(ctx, authority, gateTestCall("echo original"))
	if err != nil || !replay.Replayed || replay.Output != "post:post:raw" || handler.calls != 1 {
		t.Fatal("replay bypassed hooks or ran twice", replay, err)
	}
	authority.Mode = ModeReadonly
	denied, err := gate.Dispatch(ctx, authority, gateTestCall("echo original"))
	if err != nil || !denied.Denied || denied.Replayed {
		t.Fatal("replay bypassed current permission", denied, err)
	}
}
func TestDeniedCallDoesNotStartJournalAndConflictDoesNotExecute(t *testing.T) {
	ctx := context.Background()
	journal, _ := NewInMemoryActionJournal(2)
	handler := &gateHandler{output: "effect"}
	gate, _ := NewJournaledToolGate(gateTestCatalog(t, handler), DefaultPermissionPolicy(nil), GateHooks{}, journal)
	authority := gateTestAuthority(ModeReadonly)
	authority.RunContext = actionContext(t)
	denied, err := gate.Dispatch(ctx, authority, gateTestCall("echo x"))
	if err != nil || !denied.Denied {
		t.Fatal(denied, err)
	}
	_, exists, _ := journal.Get(ctx, denied.ActionID)
	if exists {
		t.Fatal("parked/denied call journalled as dispatched")
	}
	authority.Mode = ModeAuto
	gate.Dispatch(ctx, authority, gateTestCall("echo x"))
	conflict, err := gate.Dispatch(ctx, authority, gateTestCall("echo different"))
	if err != nil || !conflict.Failed || !strings.Contains(conflict.Output, "different payload") || handler.calls != 1 {
		t.Fatal(conflict, handler.calls, err)
	}
}

type cancelledActionHandler struct {
	cancel context.CancelFunc
	store  *testActionStore
	fault  error
}

func (handler cancelledActionHandler) ExecuteTool(context.Context, ToolAuthority, protocol.ToolInput) (string, error) {
	if handler.store != nil {
		handler.store.writeErr = handler.fault
	}
	handler.cancel()
	return "", context.Canceled
}
func TestCancelledActionSettlesBeforeReturningAndFaultPropagates(t *testing.T) {
	for _, broken := range []bool{false, true} {
		t.Run(map[bool]string{false: "healthy", true: "broken"}[broken], func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			store := newTestActionStore()
			journal, _ := NewStoredActionJournal(store)
			fault := errors.New("settlement failed")
			handler := cancelledActionHandler{cancel: cancel}
			if broken {
				handler.store, handler.fault = store, fault
			}
			gate, _ := NewJournaledToolGate(gateTestCatalog(t, handler), DefaultPermissionPolicy(nil), GateHooks{}, journal)
			authority := gateTestAuthority(ModeAuto)
			authority.RunContext = actionContext(t)
			call := gateTestCall("echo x")
			id, _ := ToolActionID(authority.SessionID, authority.RunContext, call)
			_, err := gate.Dispatch(ctx, authority, call)
			if broken {
				if !errors.Is(err, fault) {
					t.Fatal("cancel settlement fault swallowed", err)
				}
			} else if !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			record, exists, _ := journal.Get(context.Background(), id)
			if !exists {
				t.Fatal("lost action")
			}
			expected := ActionCancelled
			if broken {
				expected = ActionStarted
			}
			if record.Status != expected {
				t.Fatal(record.Status)
			}
		})
	}
}
func TestBrokenVerifierCannotAuthorizeUnknownRetry(t *testing.T) {
	ctx := context.Background()
	journal, _ := NewStoredActionJournal(newTestActionStore())
	handler := &gateHandler{}
	definition, _ := NewToolDefinition(protocol.ToolBash, ToolTraits{Risk: RiskExec}, handler)
	definition = definition.WithVerifier(verifierFunc(func(context.Context, ToolAuthority, ToolCall) (EffectVerdict, error) { panic("broken") }))
	catalog, _ := NewToolCatalog(definition)
	gate, _ := NewJournaledToolGate(catalog, DefaultPermissionPolicy(nil), GateHooks{}, journal)
	authority := gateTestAuthority(ModeAuto)
	authority.RunContext = actionContext(t)
	call := gateTestCall("echo x")
	id, _ := ToolActionID("s", authority.RunContext, call)
	journal.Begin(ctx, ActionRequest{id, "s", "m", call.ID, call.Input})
	journal.Finish(ctx, ActionSettlement{ActionID: id, Status: ActionUnknown})
	outcome, err := gate.Dispatch(ctx, authority, call)
	if err != nil || handler.calls != 0 || outcome.Output != unknownToolResult || !outcome.Replayed {
		t.Fatal(outcome, err)
	}
}
func TestRuntimeJournalReplayUsesRunIdentityAndKeepsSessionsSeparate(t *testing.T) {
	ctx := context.Background()
	journal, _ := NewInMemoryActionJournal(DefaultResultsRetained)
	run := actionContext(t)
	var group sync.WaitGroup
	for i := 0; i < 4; i++ {
		config := runtimeConfig(t.TempDir(), &FakeProvider{})
		config.ID = SessionID(string(rune('a' + i)))
		config.ActionJournal = journal
		config.Bash = &subagentBashSpy{}
		session, err := NewRuntimeSession(config)
		if err != nil {
			t.Fatal(err)
		}
		group.Add(1)
		go func() {
			defer group.Done()
			for n := 0; n < 2; n++ {
				if _, err := session.RunWithContext(ctx, "same", run); err != nil {
					t.Error(err)
				}
			}
			if config.Bash.(*subagentBashSpy).calls != 1 {
				t.Error("same run reexecuted")
			}
		}()
	}
	group.Wait()
}
func TestWriteFileUnknownUsesBoundSourceVerifier(t *testing.T) {
	ctx := context.Background()
	files, err := workspace.NewFiles(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	journal, _ := NewStoredActionJournal(newTestActionStore())
	catalog, err := NewWorkspaceToolCatalog(echoExecutor{}, files)
	if err != nil {
		t.Fatal(err)
	}
	gate, _ := NewJournaledToolGate(catalog, DefaultPermissionPolicy(nil), GateHooks{}, journal)
	authority := gateTestAuthority(ModeAuto)
	authority.Workspace = files.Root()
	authority.RunContext = actionContext(t)
	call := ToolCall{"u", protocol.WriteFileToolInput(protocol.WriteFileInput{Path: "made", Content: "hello"})}
	id, _ := ToolActionID("s", authority.RunContext, call)
	journal.Begin(ctx, ActionRequest{id, "s", "m", "u", call.Input})
	journal.Finish(ctx, ActionSettlement{ActionID: id, Status: ActionUnknown})
	files.Write(ctx, protocol.WriteFileInput{Path: "made", Content: "hello"})
	outcome, err := gate.Dispatch(ctx, authority, call)
	if err != nil || outcome.Output != ReconciledActionResult || !outcome.Replayed {
		t.Fatal(outcome, err)
	}
	// A missing target is proof of non-landing. Source finish intentionally does
	// not change unknown: only the positive reconciliation owns that transition.
	store := newTestActionStore()
	journal, _ = NewStoredActionJournal(store)
	gate, _ = NewJournaledToolGate(catalog, DefaultPermissionPolicy(nil), GateHooks{}, journal)
	journal.Begin(ctx, ActionRequest{id, "s", "m", "u", call.Input})
	journal.Finish(ctx, ActionSettlement{ActionID: id, Status: ActionUnknown})
	os.Remove(filepath.Join(files.Root(), "made"))
	outcome, err = gate.Dispatch(ctx, authority, call)
	if err != nil || outcome.Replayed || outcome.IsError() {
		t.Fatal(outcome, err)
	}
	data, err := os.ReadFile(filepath.Join(files.Root(), "made"))
	if err != nil || string(data) != "hello" {
		t.Fatal(string(data), err)
	}
}
