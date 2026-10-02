package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/luoyjx/mini-loop/go/protocol"
)

type actionContracts struct {
	Inputs []struct {
		Block     protocol.Block
		Canonical string
		InputHash InputHash `json:"input_hash"`
		ActionID  ActionID  `json:"action_id"`
	}
	Runs []struct {
		Backing string
		Records []ActionRecord
		Errors  []bool
	}
	Bounds []struct {
		Tool   protocol.ToolName
		Result string
	}
	Replays []struct {
		Status           ActionStatus
		Verification     string
		Output           string
		Calls            int
		Replayed, Failed bool
		Verdict          *EffectVerdict
		Record           ActionRecord
	}
	ShedResult string `json:"shed_result"`
}

func readActionContracts(t *testing.T) actionContracts {
	t.Helper()
	data, err := os.ReadFile("../testdata/python-actions.json")
	if err != nil {
		t.Fatal(err)
	}
	var value actionContracts
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	return value
}
func actionContext(t *testing.T) RunContext {
	t.Helper()
	run, err := DefaultRunContext()
	if err != nil {
		t.Fatal(err)
	}
	run.messageID = "m"
	return run
}
func normalizedAction(record ActionRecord) ActionRecord {
	record = record.Clone()
	record.CreatedAt = 0
	if record.CompletedAt != nil {
		zero := float64(0)
		record.CompletedAt = &zero
	}
	return record
}
func actionRequest(id ActionID) ActionRequest {
	return ActionRequest{id, "s", "m", "u", protocol.BashToolInput(protocol.BashInput{Command: "echo x"})}
}
func stringPointer(value string) *string { return &value }

// Typed test backing exercises the stored transition adapter. It is deliberately
// not advertised as SQLite or evidence of restart-safe storage.
type testActionStore struct {
	mu                sync.Mutex
	records           map[ActionID]ActionRecord
	readErr, writeErr error
}

func newTestActionStore() *testActionStore {
	return &testActionStore{records: make(map[ActionID]ActionRecord)}
}
func (store *testActionStore) ReadAction(ctx context.Context, id ActionID) (ActionRecord, bool, error) {
	if err := ctx.Err(); err != nil {
		return ActionRecord{}, false, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	record, exists := store.records[id]
	return record.Clone(), exists, store.readErr
}
func (store *testActionStore) WriteAction(ctx context.Context, record ActionRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.writeErr != nil {
		return store.writeErr
	}
	store.records[record.ActionID] = record.Clone()
	return nil
}
func (store *testActionStore) MarkInflightUnknown(ctx context.Context, session *SessionID) ([]ActionID, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	ids := []ActionID{}
	for id, record := range store.records {
		if record.Status == ActionStarted && (session == nil || record.SessionID == *session) {
			record.Status = ActionUnknown
			store.records[id] = record
			ids = append(ids, id)
		}
	}
	return ids, nil
}
func TestActionIdentityAndCanonicalHashMatchPython(t *testing.T) {
	ctx := context.Background()
	run := actionContext(t)
	for _, fixture := range readActionContracts(t).Inputs {
		use, ok := fixture.Block.ToolUse()
		if !ok {
			t.Fatal("expected typed tool use")
		}
		call := ToolCall{use.ID, use.Input}
		encoded, err := call.Input.CanonicalJSON()
		if err != nil || encoded != fixture.Canonical {
			t.Fatalf("%s canonical: %q, %v; want %q", call.Name(), encoded, err, fixture.Canonical)
		}
		id, err := ToolActionID("s", run, call)
		if err != nil || id != fixture.ActionID {
			t.Fatalf("identity %s, %v; want %s", id, err, fixture.ActionID)
		}
		journal, _ := NewInMemoryActionJournal(1)
		record, err := journal.Begin(ctx, ActionRequest{id, "s", "m", use.ID, use.Input})
		if err != nil || record.InputHash != fixture.InputHash {
			t.Fatalf("hash %s: %s, %v; want %s", call.Name(), record.InputHash, err, fixture.InputHash)
		}
	}
	a, _ := ToolActionID("s", run, ToolCall{Input: protocol.CompressToolInput()})
	b, _ := ToolActionID("s", run, ToolCall{Input: protocol.CompressToolInput()})
	if a == b {
		t.Fatal("blank provider ID collapsed independent actions")
	}
	other, _ := run.WithNewMessage(nil)
	a, _ = ToolActionID("s", run, gateTestCall("echo x"))
	b, _ = ToolActionID("s", other, gateTestCall("echo x"))
	if a == b {
		t.Fatal("different messages collapsed")
	}
}
func TestJournalTransitionsMatchPythonMemoryAndSQLite(t *testing.T) {
	ctx := context.Background()
	for _, fixture := range readActionContracts(t).Runs {
		t.Run(fixture.Backing, func(t *testing.T) {
			var journal ActionJournal
			if fixture.Backing == "memory" {
				journal, _ = NewInMemoryActionJournal(DefaultResultsRetained)
			} else {
				journal, _ = NewStoredActionJournal(newTestActionStore())
			}
			request := actionRequest("a")
			records := []ActionRecord{}
			appendRecord := func(record ActionRecord, err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
				records = append(records, normalizedAction(record))
			}
			appendRecord(journal.Begin(ctx, request))
			appendRecord(journal.Finish(ctx, ActionSettlement{"a", ActionCompleted, stringPointer("done")}))
			appendRecord(journal.Finish(ctx, ActionSettlement{"a", ActionFailed, stringPointer("overwrite")}))
			appendRecord(journal.Begin(ctx, request))
			appendRecord(journal.AttachWorkflow(ctx, "a", "workflow"))
			record, exists, err := journal.Get(ctx, "a")
			if !exists {
				t.Fatal("missing action")
			}
			appendRecord(record, err)
			if !reflect.DeepEqual(records, fixture.Records) {
				t.Fatalf("records mismatch: %#v; want %#v", records, fixture.Records)
			}
			request.Input = protocol.BashToolInput(protocol.BashInput{Command: "other"})
			_, err1 := journal.Begin(ctx, request)
			_, err2 := journal.AttachWorkflow(ctx, "a", "different")
			_, err3 := journal.Finish(ctx, ActionSettlement{ActionID: "a", Status: ActionStarted})
			if !reflect.DeepEqual([]bool{err1 != nil, err2 != nil, err3 != nil}, fixture.Errors) {
				t.Fatal("invalid transitions accepted")
			}
			var conflict *ActionJournalConflict
			if !errors.As(err1, &conflict) {
				t.Fatal("missing typed payload conflict")
			}
			*record.Result = "mutated"
			*record.WorkflowRunID = "mutated"
			*record.CompletedAt = -1
			current, _, _ := journal.Get(ctx, "a")
			if *current.Result != "done" || *current.WorkflowRunID != "workflow" || *current.CompletedAt < 0 {
				t.Fatal("snapshot aliases stored data")
			}
		})
	}
	for _, fixture := range readActionContracts(t).Bounds {
		actual := boundActionResult(stringPointer(strings.Repeat("😀", 4100)), fixture.Tool)
		if *actual != fixture.Result {
			t.Fatalf("%s result bound mismatch", fixture.Tool)
		}
	}
}

type verifierFunc func(context.Context, ToolAuthority, ToolCall) (EffectVerdict, error)

func (verify verifierFunc) VerifyTool(ctx context.Context, authority ToolAuthority, call ToolCall) (EffectVerdict, error) {
	return verify(ctx, authority, call)
}
func TestGateReplayAndReconciliationMatchRealPythonCalls(t *testing.T) {
	ctx := context.Background()
	run := actionContext(t)
	for _, fixture := range readActionContracts(t).Replays {
		t.Run(string(fixture.Status)+"/"+fixture.Verification, func(t *testing.T) {
			journal, _ := NewStoredActionJournal(newTestActionStore())
			handler := &gateHandler{output: "effect"}
			definition, _ := NewToolDefinition(protocol.ToolBash, ToolTraits{Risk: RiskExec}, handler)
			if fixture.Verification != "absent" {
				definition = definition.WithVerifier(verifierFunc(func(context.Context, ToolAuthority, ToolCall) (EffectVerdict, error) {
					switch fixture.Verification {
					case "yes":
						return EffectAlreadyApplied, nil
					case "no":
						return EffectNotApplied, nil
					case "error":
						return EffectNotApplied, errors.New("cannot tell")
					case "invalid":
						return "no", nil
					}
					return EffectUndetermined, nil
				}))
			}
			catalog, _ := NewToolCatalog(definition)
			gate, _ := NewJournaledToolGate(catalog, DefaultPermissionPolicy(nil), GateHooks{}, journal)
			call := ToolCall{"u", protocol.BashToolInput(protocol.BashInput{Command: "echo x"})}
			id, _ := ToolActionID("s", run, call)
			_, err := journal.Begin(ctx, ActionRequest{id, "s", "m", "u", call.Input})
			if err != nil {
				t.Fatal(err)
			}
			result := stringPointer("recorded")
			if fixture.Status == ActionUnknown {
				result = nil
			}
			if _, err := journal.Finish(ctx, ActionSettlement{id, fixture.Status, result}); err != nil {
				t.Fatal(err)
			}
			authority := gateTestAuthority(ModeAuto)
			authority.RunContext = run
			outcome, err := gate.Dispatch(ctx, authority, call)
			if err != nil {
				t.Fatal(err)
			}
			if outcome.Output != fixture.Output || handler.calls != fixture.Calls || outcome.Replayed != fixture.Replayed || outcome.Failed != fixture.Failed || outcome.ActionID != id {
				t.Fatalf("outcome %#v, calls %d; want %#v", outcome, handler.calls, fixture)
			}
			if fixture.Verdict != nil {
				if outcome.reconciliation == nil || outcome.reconciliation.Verdict != *fixture.Verdict {
					t.Fatalf("reconciliation %#v", outcome.reconciliation)
				}
			} else if outcome.reconciliation != nil {
				t.Fatal("unexpected reconciliation")
			}
			record, _, _ := journal.Get(ctx, id)
			if !reflect.DeepEqual(normalizedAction(record), fixture.Record) {
				t.Fatalf("record %#v; want %#v", record, fixture.Record)
			}
		})
	}
}
func TestJournalResultSheddingKeepsReplayIdentity(t *testing.T) {
	ctx := context.Background()
	journal, _ := NewInMemoryActionJournal(1000)
	for i := 0; i < 520; i++ {
		id := ActionID(fmt.Sprint(i))
		if _, err := journal.Begin(ctx, actionRequest(id)); err != nil {
			t.Fatal(err)
		}
		if _, err := journal.Finish(ctx, ActionSettlement{id, ActionCompleted, stringPointer(strings.Repeat("x", 4000))}); err != nil {
			t.Fatal(err)
		}
	}
	first, exists, _ := journal.Get(ctx, "0")
	if !exists || first.Status != ActionCompleted || *first.Result != readActionContracts(t).ShedResult {
		t.Fatal("identity discarded instead of payload")
	}
	last, _, _ := journal.Get(ctx, "519")
	if len(*last.Result) != 4000 {
		t.Fatal("fresh payload shed")
	}
	if len(journal.Problems()) == 0 || journal.retainedChars > MaxRetainedResultChars {
		t.Fatal("retention not bounded/reported")
	}
	small, _ := NewInMemoryActionJournal(0)
	small.Begin(ctx, actionRequest("a"))
	small.Finish(ctx, ActionSettlement{"a", ActionUnknown, nil})
	record, exists, _ := small.Get(ctx, "a")
	if !exists || record.Result != nil {
		t.Fatal("nil result retention changed")
	}
}
func TestStoredJournalReportsFaultsAndOnlyReconcilesUnknown(t *testing.T) {
	ctx := context.Background()
	store := newTestActionStore()
	journal, _ := NewStoredActionJournal(store)
	journal.Begin(ctx, actionRequest("a"))
	journal.Begin(ctx, ActionRequest{"b", "other", "m", "u", protocol.CompressToolInput()})
	session := SessionID("s")
	ids, err := journal.MarkInflightUnknown(ctx, &session)
	if err != nil || !reflect.DeepEqual(ids, []ActionID{"a"}) {
		t.Fatal(ids, err)
	}
	a, _, _ := journal.Get(ctx, "a")
	b, _, _ := journal.Get(ctx, "b")
	if a.Status != ActionUnknown || a.CompletedAt != nil || b.Status != ActionStarted {
		t.Fatal("incorrect inflight scope")
	}
	journal.Finish(ctx, ActionSettlement{"a", ActionCompleted, stringPointer("rewrite")})
	a, _, _ = journal.Get(ctx, "a")
	if a.Status != ActionUnknown {
		t.Fatal("finish rewrote unknown")
	}
	if _, err := journal.Reconcile(ctx, ActionSettlement{ActionID: "a", Status: ActionUnknown}); err == nil {
		t.Fatal("invalid reconciliation accepted")
	}
	journal.Reconcile(ctx, ActionSettlement{"a", ActionCompleted, stringPointer("proven")})
	journal.Reconcile(ctx, ActionSettlement{"a", ActionFailed, stringPointer("rewrite")})
	a, _, _ = journal.Get(ctx, "a")
	if a.Status != ActionCompleted || *a.Result != "proven" {
		t.Fatal("terminal record rewritten")
	}
	fault := errors.New("disk failure")
	store.writeErr = fault
	if _, err := journal.Begin(ctx, actionRequest("c")); !errors.Is(err, fault) {
		t.Fatal("begin write fault swallowed")
	}
	if _, err := journal.Finish(ctx, ActionSettlement{"b", ActionCompleted, stringPointer("done")}); !errors.Is(err, fault) {
		t.Fatal("finish write fault swallowed")
	}
	store.readErr = fault
	if _, err := journal.Begin(ctx, actionRequest("d")); !errors.Is(err, fault) {
		t.Fatal("read fault swallowed")
	}
}
