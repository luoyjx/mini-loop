package agent

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/luoyjx/mini-loop/go/protocol"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Synchronized memory backing only. Overrides exercise reader faults and races.
type catchupStore struct {
	*runtimeStateStore
	read func(context.Context, SessionID, EventOrdinal, *int) ([]SessionEventRecord, error)
}

func (s *catchupStore) LoadEvents(ctx context.Context, id SessionID, after EventOrdinal, limit *int) ([]SessionEventRecord, error) {
	if s.read != nil {
		return s.read(ctx, id, after, limit)
	}
	return s.runtimeStateStore.LoadEvents(ctx, id, after, limit)
}
func catchupStatus(s *ManagedSession) {
	s.core.events.append(SessionEvent{kind: EventStatus, status: StatusEvent{Status: StatusIdle}})
}
func catchupIDs(records []SessionEventRecord) []EventSequence {
	out := make([]EventSequence, 0, len(records))
	for _, r := range records {
		out = append(out, r.Sequence)
	}
	return out
}
func TestCatchupActualPythonWindowAndBoundary(t *testing.T) {
	var fixture struct {
		Cases []struct {
			Name             string
			Count, Ephemeral int
			Null, Boundary   bool
			Header           string
			PhysicalHead     EventOrdinal  `json:"physical_head"`
			LiveSequence     EventSequence `json:"live_sequence"`
			IDs              []EventSequence
			Reads            []struct {
				After string
				Limit int
			}
			Subscribers int `json:"subscribers_after"`
		}
	}
	data, err := os.ReadFile("../testdata/python-event-catchup.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, recipe := range fixture.Cases {
		if strings.HasPrefix(recipe.Name, "header-") {
			continue
		}
		t.Run(recipe.Name, func(t *testing.T) {
			backing := newRuntimeStateStore()
			store := &catchupStore{runtimeStateStore: backing}
			var configured StateStore = store
			if recipe.Null {
				configured = nil
			}
			session := stateManaged(t, configured, stateProviderFunc(func(context.Context, protocol.ModelRequest) (protocol.ModelReply, error) { return stateFinal(), nil }), "")
			for i := 0; i < recipe.Count; i++ {
				for j := 0; j < recipe.Ephemeral; j++ {
					session.core.events.append(SessionEvent{kind: EventAssistantDelta, delta: AssistantDeltaEvent{Text: "progress", StreamID: "stream", Phase: PhaseCommentary, Provisional: true}})
				}
				catchupStatus(session)
			}
			head, _ := backing.EventCursor(context.Background(), session.ID())
			if head != recipe.PhysicalHead {
				t.Fatal(head, recipe.PhysicalHead)
			}
			sub := session.Subscribe(true)
			defer sub.Close()
			var afterRead EventOrdinal
			var limitRead int
			store.read = func(ctx context.Context, id SessionID, after EventOrdinal, limit *int) ([]SessionEventRecord, error) {
				afterRead = after
				limitRead = *limit
				if recipe.Boundary {
					catchupStatus(session)
				} // Must not deadlock live persistence.
				return backing.LoadEvents(ctx, id, after, limit)
			}
			caught, err := session.CatchUpEvents(context.Background(), 5)
			if err != nil {
				t.Fatal(err)
			}
			got := catchupIDs(caught)
			delivered := EventSequence(5)
			if len(got) > 0 {
				delivered = got[len(got)-1]
			}
			for draining := true; draining; {
				select {
				case record := <-sub.Events():
					if record.Sequence > delivered {
						got = append(got, record.Sequence)
						delivered = record.Sequence
					}
				default:
					draining = false
				}
			}
			if recipe.Ephemeral == 0 {
				if !reflect.DeepEqual(got, recipe.IDs) {
					t.Fatalf("got %v, source %v", got, recipe.IDs)
				}
			} else {
				// Actual source uses sequence 750 as a physical ordinal, losing 50 rows.
				if len(recipe.IDs) != 200 || recipe.Reads[0].After != "750" || len(got) != 250 || got[0] != 11 || got[249] != 2750 {
					t.Fatal("ephemeral-window evidence", len(got), recipe.Reads)
				}
			}
			if !recipe.Null && limitRead != MaxEventCatchup {
				t.Fatal(limitRead)
			}
			if recipe.Name == "bounded-tail" && strconv.FormatUint(uint64(afterRead), 10) != recipe.Reads[0].After {
				t.Fatal(afterRead)
			}
			if session.core.events.next != recipe.LiveSequence || recipe.Subscribers != 0 {
				t.Fatal("source/live terminal", session.core.events.next, recipe)
			}
			sub.Close()
			if session.Info().Subscribers != 0 {
				t.Fatal("subscription leak")
			}
		})
	}
}
func TestCatchupFailuresAreReadOnlyAndFailClosed(t *testing.T) {
	for _, name := range []string{"cursor", "read", "panic", "foreign", "order", "unknown", "ephemeral", "oversize"} {
		t.Run(name, func(t *testing.T) {
			backing := newRuntimeStateStore()
			store := &catchupStore{runtimeStateStore: backing}
			s := stateManaged(t, store, stateProviderFunc(func(context.Context, protocol.ModelRequest) (protocol.ModelReply, error) { return stateFinal(), nil }), "")
			catchupStatus(s)
			if name == "cursor" {
				backing.faults["cursor"] = errors.New("cursor private canary")
			}
			store.read = func(ctx context.Context, id SessionID, after EventOrdinal, limit *int) ([]SessionEventRecord, error) {
				rows, _ := backing.LoadEvents(ctx, id, after, limit)
				switch name {
				case "read":
					return nil, errors.New("read private canary")
				case "panic":
					panic("private canary")
				case "foreign":
					rows[0].SessionID = "foreign"
				case "order":
					rows = append(rows, rows[0])
				case "unknown":
					rows[0].Event = SessionEvent{}
				case "ephemeral":
					rows[0].Event = SessionEvent{kind: EventAssistantDelta}
				case "oversize":
					for len(rows) <= MaxEventCatchup {
						rows = append(rows, rows[0])
					}
				}
				return rows, nil
			}
			got, err := s.CatchUpEvents(context.Background(), 1)
			if !errors.Is(err, ErrEventCatchup) || len(got) != 0 {
				t.Fatal(got, err)
			}
			if s.PersistenceStatus().Error != nil || backing.acquireCalls != 0 {
				t.Fatal("reader changed write/lease status")
			}
			if name == "cursor" {
				delete(backing.faults, "cursor")
			}
			store.read = nil
			catchupStatus(s)
			if s.PersistenceStatus().Error != nil {
				t.Fatal(s.PersistenceStatus())
			}
			got, err = s.CatchUpEvents(context.Background(), 1)
			if err != nil || len(got) != 1 {
				t.Fatal(got, err)
			}
		})
	}
}
func TestCatchupCancellationFreshDeletedAndRestored(t *testing.T) {
	store := &catchupStore{runtimeStateStore: newRuntimeStateStore()}
	s := stateManaged(t, store, stateProviderFunc(func(context.Context, protocol.ModelRequest) (protocol.ModelReply, error) { return stateFinal(), nil }), "")
	catchupStatus(s)
	reads := 0
	store.read = func(ctx context.Context, id SessionID, after EventOrdinal, limit *int) ([]SessionEventRecord, error) {
		reads++
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if got, err := s.CatchUpEvents(context.Background(), 0); err != nil || len(got) != 0 || reads != 0 {
		t.Fatal(got, err, reads)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := s.CatchUpEvents(ctx, 1); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if got, err := s.CatchUpEvents(ctx, 1); !errors.Is(err, context.DeadlineExceeded) || len(got) != 0 {
		t.Fatal(got, err)
	}
	if err := s.core.persistence.delete(); err != nil {
		t.Fatal(err)
	}
	if got, err := s.CatchUpEvents(context.Background(), 1); err != nil || len(got) != 0 || reads != 1 {
		t.Fatal(got, err, reads)
	}
	backing := newRuntimeStateStore()
	seedRestore(t, backing, nil)
	m := restoreManager(t, backing, stateProviderFunc(func(context.Context, protocol.ModelRequest) (protocol.ModelReply, error) { return stateFinal(), nil }))
	sessions, err := m.RestoreSessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	restored := sessions[0]
	if len(restored.Events()) != 0 {
		t.Fatal("restore should not load replay backlog")
	}
	got, err := restored.CatchUpEvents(context.Background(), 1)
	if err != nil || len(got) != 1 || got[0].Sequence != 9 {
		t.Fatal(got, err)
	}
	if got[0].Scope.RunContext.Authority() != AuthorityUntrusted {
		t.Fatal("historical authority installed")
	}
}

func TestCatchupDetachedMaskedProjectionNeverInstallsHumanAuthority(t *testing.T) {
	store := newRuntimeStateStore()
	s, err := NewManagedSession(RuntimeConfig{ID: "masked-history", Owner: "tenant", Workspace: t.TempDir(), Provider: &FakeProvider{}, Bash: echoExecutor{}, Mode: ModeAuto, MaxRounds: 2, StateStore: store, Secrets: runtimeSecrets()})
	if err != nil {
		t.Fatal(err)
	}
	actor := ActorID("operator")
	run, err := ExplicitHumanRunContext(HumanRunConfig{ActorID: &actor, Channel: "test", StampedBy: "test", ApprovedCapabilities: []RunCapability{CapabilityWorkflowManage}})
	if err != nil {
		t.Fatal(err)
	}
	s.core.events.setScope(EventScope{RunContext: run})
	catchupStatus(s)
	s.core.events.append(SessionEvent{kind: EventTodo, todos: []protocol.TodoItem{{Content: runtimeCanary, Status: "pending", ActiveForm: "doing"}}})
	beforeLive, _ := json.Marshal(s.core.events.snapshot())
	got, err := s.CatchUpEvents(context.Background(), 1)
	if err != nil || len(got) != 1 {
		t.Fatal(got, err)
	}
	encoded, _ := json.Marshal(got)
	if strings.Contains(string(encoded), "0123456789") {
		t.Fatal("stored secret escaped", string(encoded))
	}
	scope := got[0].Scope.RunContext.Snapshot()
	if scope.Authority != AuthorityUntrusted || scope.ActorID != nil || len(scope.ApprovedCapabilities) != 0 {
		t.Fatal(scope)
	}
	got[0].Event.todos[0].Content = "changed caller copy"
	again, err := s.CatchUpEvents(context.Background(), 1)
	if err != nil || again[0].Event.todos[0].Content == "changed caller copy" {
		t.Fatal(again, err)
	}
	afterLive, _ := json.Marshal(s.core.events.snapshot())
	if string(afterLive) != string(beforeLive) {
		t.Fatal("reader changed live history")
	}
}
