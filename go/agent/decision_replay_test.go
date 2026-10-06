package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/luoyjx/mini-loop/go/decisions"
	"github.com/luoyjx/mini-loop/go/protocol"
)

// Raw fields are fixture wire input only, never stored in a runtime service.
type decisionReplayFixture struct {
	Request, Baseline json.RawMessage
	Cases             []struct {
		Backing         string
		Maximum         bool
		Bytes           int
		CanonicalSHA256 string `json:"canonical_sha256"`
		ReplayExact     bool   `json:"replay_exact"`
		Calls           int
	}
	Bounds []struct {
		Tool             protocol.ToolName
		InputChars       int `json:"input_chars"`
		Chars            int
		SHA256           string
		ReconciledSHA256 string `json:"reconciled_sha256"`
	}
	Aggregate struct {
		Records, Retained, Calls int
		RetainedChars            int `json:"retained_chars"`
		Replay                   string
		Problems                 []string
	}
}

func replayFixture(t *testing.T) decisionReplayFixture {
	t.Helper()
	b, e := os.ReadFile("../testdata/python-decision-replay.json")
	if e != nil {
		t.Fatal(e)
	}
	var f decisionReplayFixture
	if e = json.Unmarshal(b, &f); e != nil {
		t.Fatal(e)
	}
	return f
}
func replayDigest(text string) string {
	h := sha256.Sum256([]byte(text))
	return hex.EncodeToString(h[:])
}
func replayResult(t *testing.T, f decisionReplayFixture, maximum bool) (decisions.Request, decisions.Result) {
	t.Helper()
	r, e := decisions.DecodeRequest(f.Request)
	if e != nil {
		t.Fatal(e)
	}
	value, e := decisions.DecodeValue(f.Baseline)
	if e != nil {
		t.Fatal(e)
	}
	base, e := value.MarshalJSON()
	if e != nil {
		t.Fatal(e)
	}
	if maximum {
		value = value.MapStrings(func(s string) string {
			if s == "test-model" {
				return s + strings.Repeat("m", decisions.MaxResponseBytes-len(base))
			}
			return s
		})
	}
	raw, e := value.MarshalJSON()
	if e != nil {
		t.Fatal(e)
	}
	result, e := decisions.DecodeResult(r, raw)
	if e != nil {
		t.Fatal(e)
	}
	return r, result
}
func replaySession(t *testing.T, journal ActionJournal, result decisions.Result, calls *int) *Session {
	t.Helper()
	cfg := runtimeConfig(t.TempDir(), &FakeProvider{})
	cfg.Mode = ModeAuto
	cfg.DecisionTools = true
	cfg.ActionJournal = journal
	cfg.DecisionProvider = decisionBackendFunc(func(context.Context, decisions.Request) (decisions.Result, error) { *calls++; return result, nil })
	s, e := NewRuntimeSession(cfg)
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func TestDecisionSourceLargeReplayAndStoredAdapter(t *testing.T) {
	f := replayFixture(t)
	for _, row := range f.Cases {
		t.Run(fmt.Sprintf("%s-%t", row.Backing, row.Maximum), func(t *testing.T) {
			request, result := replayResult(t, f, row.Maximum)
			var journal ActionJournal
			var store *testActionStore
			if row.Backing == "memory" {
				journal, _ = NewInMemoryActionJournal(DefaultResultsRetained)
			} else {
				store = newTestActionStore()
				journal, _ = NewStoredActionJournal(store)
			}
			calls := 0
			s := replaySession(t, journal, result, &calls)
			run, _ := DefaultRunContext()
			use := protocol.ToolUseBlock{ID: "same-decision", Name: protocol.ToolDecision, Input: protocol.DecisionToolInput(request)}
			first, e := s.dispatchTool(context.Background(), run, use)
			if e != nil || first.Failed || len(first.Output) != row.Bytes {
				t.Fatal("source result bound", len(first.Output), row.Bytes, e)
			}
			value, e := decisions.DecodeValue([]byte(first.Output))
			if e != nil {
				t.Fatal(e)
			}
			canonical, e := value.MarshalJSON()
			if e != nil || replayDigest(string(canonical)) != row.CanonicalSHA256 {
				t.Fatal("source semantic bytes differ", e)
			}
			if store != nil {
				journal, _ = NewStoredActionJournal(store)
			} // adapter recreation only; not SQL reopen
			s = replaySession(t, journal, result, &calls)
			replay, e := s.dispatchTool(context.Background(), run, use)
			if e != nil || !replay.Replayed || !row.ReplayExact || replay.Output != first.Output || calls != row.Calls {
				t.Fatal("repeat call or changed bytes", e, calls)
			}
			s.mode = ModeReadonly
			denied, e := s.dispatchTool(context.Background(), run, use)
			if e != nil || !denied.Denied || denied.Replayed || calls != row.Calls {
				t.Fatal("replay skipped current guard", e)
			}
		})
	}
}
func TestDecisionSourceAggregateShedding(t *testing.T) {
	f := replayFixture(t)
	request, result := replayResult(t, f, true)
	journal, _ := NewInMemoryActionJournal(DefaultResultsRetained)
	calls := 0
	s := replaySession(t, journal, result, &calls)
	run, _ := DefaultRunContext()
	for i := 0; i < 5; i++ {
		out, e := s.dispatchTool(context.Background(), run, protocol.ToolUseBlock{ID: fmt.Sprintf("decision-%d", i), Name: protocol.ToolDecision, Input: protocol.DecisionToolInput(request)})
		if e != nil || out.Failed || len(out.Output) != decisions.MaxResponseBytes {
			t.Fatal(i, e)
		}
	}
	retained, chars := 0, 0
	for _, record := range journal.records {
		if record.Result != nil && *record.Result != ShedActionResult {
			retained++
			chars += utf8.RuneCountInString(*record.Result)
		}
	}
	if len(journal.records) != f.Aggregate.Records || retained != f.Aggregate.Retained || chars != f.Aggregate.RetainedChars || journal.retainedChars != chars || !reflect.DeepEqual(journal.Problems(), f.Aggregate.Problems) {
		t.Fatal("source retention differs", retained, chars, journal.Problems())
	}
	use := protocol.ToolUseBlock{ID: "decision-0", Name: protocol.ToolDecision, Input: protocol.DecisionToolInput(request)}
	out, e := s.dispatchTool(context.Background(), run, use)
	if e != nil || !out.Replayed || out.Output != f.Aggregate.Replay || calls != f.Aggregate.Calls {
		t.Fatal("shed identity was reexecuted", e, calls)
	}
	// Terminal resettlement must not add the same payload to the aggregate twice.
	id, e := ToolActionID(s.id, run, ToolCall{Input: use.Input, ID: use.ID})
	if e != nil {
		t.Fatal(e)
	}
	before := journal.retainedChars
	raw := "replacement"
	settled, err := journal.Finish(context.Background(), ActionSettlement{ActionID: id, Status: ActionCompleted, Result: &raw})
	if err != nil || settled.Result == nil || *settled.Result != f.Aggregate.Replay {
		t.Fatal("terminal result rewritten", err)
	}
	if journal.retainedChars != before {
		t.Fatal("resettlement double counted")
	}
}
func TestDecisionSourceUnicodeBoundsAndReconciliation(t *testing.T) {
	for _, row := range replayFixture(t).Bounds {
		raw := strings.Repeat("é", row.InputChars)
		got := boundActionResult(&raw, row.Tool)
		if utf8.RuneCountInString(*got) != row.Chars || replayDigest(*got) != row.SHA256 {
			t.Fatal(row.Tool, "bound differs")
		}
		store := newTestActionStore()
		store.records["a1"] = ActionRecord{ActionID: "a1", ToolName: row.Tool, Status: ActionUnknown}
		journal, _ := NewStoredActionJournal(store)
		record, e := journal.Reconcile(context.Background(), ActionSettlement{ActionID: "a1", Status: ActionCompleted, Result: &raw})
		if e != nil || record.Result == nil || replayDigest(*record.Result) != row.ReconciledSHA256 {
			t.Fatal("recorded tool budget lost", e)
		}
	}
}
