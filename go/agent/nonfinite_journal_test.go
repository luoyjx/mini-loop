package agent

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
)

func TestNonfiniteJournalFailureMatchesManagedSourceHookOrder(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-provider-nonfinite-stream.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Store, Name string
			Trace       []string
			Events      string `json:"tool_events_json"`
		}
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 4 {
		t.Fatal("Source inventory changed")
	}
	for _, row := range fixture.Cases {
		t.Run(row.Store+"/"+row.Name, func(t *testing.T) {
			value, err := jsonvalue.Decode(row.Events)
			if err != nil {
				t.Fatal(err)
			}
			events, _ := value.Array()
			observed, _ := events[0].Lookup("input")
			raw, err := observed.MarshalLegacyUTF8()
			if err != nil {
				t.Fatal(err)
			}
			input, err := protocol.DecodeModelToolInput(protocol.ToolBash, raw)
			if err != nil {
				t.Fatal(err)
			}
			var journal ActionJournal
			if row.Store == "memory" {
				journal, err = NewInMemoryActionJournal(DefaultResultsRetained)
			} else {
				journal, err = NewStoredActionJournal(newTestActionStore())
			}
			if err != nil {
				t.Fatal(err)
			}
			trace := []string{}
			executor := &bindingExecutorProbe{trace: &trace}
			gate, err := NewJournaledToolGate(gateTestCatalog(t, executor), DefaultPermissionPolicy(nil), GateHooks{
				Before: []BeforeHook{beforeHookFunc(func(context.Context, ToolAuthority, ToolCall) (BeforeDecision, error) {
					trace = append(trace, "before")
					return KeepToolCall(), nil
				})},
				Guards: []GuardHook{guardHookFunc(func(context.Context, ToolAuthority, ToolCall) (string, bool, error) {
					trace = append(trace, "guard")
					return "", false, nil
				})},
				After: []AfterHook{afterHookFunc(func(_ context.Context, _ ToolAuthority, _ ToolCall, output string) (string, error) {
					trace = append(trace, "after")
					return output, nil
				})},
				Observers: []ResultObserver{observerFunc(func(context.Context, ToolAuthority, ToolCall, ToolOutcome) error {
					trace = append(trace, "result")
					return nil
				})},
			}, journal)
			if err != nil {
				t.Fatal(err)
			}
			outcome, err := gate.Dispatch(context.Background(), gateTestAuthority(ModeAuto), ToolCall{ID: "call", Input: input})
			if err != nil {
				t.Fatal(err)
			}
			output, _ := events[1].Lookup("output")
			expected, _ := output.Text()
			if outcome.Output != expected || !outcome.Failed || outcome.Denied || outcome.Replayed || executor.calls != 0 {
				t.Fatal(outcome, expected, executor.calls)
			}
			if !reflect.DeepEqual(trace, row.Trace) {
				t.Fatal(trace, row.Trace)
			}
			if _, present, err := journal.Get(context.Background(), outcome.ActionID); err != nil || present {
				t.Fatal("failed input became journal authority", present, err)
			}
		})
	}
}
