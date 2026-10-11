package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/protocol"
)

// Test backing only: exercises runtime ordering/faults, never SQL, persistence,
// transaction atomicity, or real restart behavior.
type runtimeStateStore struct {
	*testActionStore
	*approvalStoreSpy
	mu                                     sync.Mutex
	rows                                   map[SessionID]SessionRecord
	messages                               map[SessionID]map[TranscriptEpoch][]protocol.Message
	events                                 map[SessionID][]SessionEventRecord
	holders                                map[SessionID]LeaseOwner
	faults                                 map[string]error
	panicAppend                            bool
	countMismatch                          bool
	renewReject                            bool
	acquireCalls, renewCalls, releaseCalls int
	onEvent                                func(SessionEventRecord)
}

func newRuntimeStateStore() *runtimeStateStore {
	return &runtimeStateStore{testActionStore: newTestActionStore(), approvalStoreSpy: &approvalStoreSpy{}, rows: map[SessionID]SessionRecord{}, messages: map[SessionID]map[TranscriptEpoch][]protocol.Message{}, events: map[SessionID][]SessionEventRecord{}, holders: map[SessionID]LeaseOwner{}, faults: map[string]error{}}
}

var _ StateStore = (*runtimeStateStore)(nil)

func (s *runtimeStateStore) UpsertSession(_ context.Context, row SessionRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.faults["upsert"]; err != nil {
		return err
	}
	if old, ok := s.rows[row.SessionID]; ok {
		row.CreatedAt = old.CreatedAt
	}
	s.rows[row.SessionID] = row.Clone()
	return nil
}
func (s *runtimeStateStore) LoadSessions(context.Context) ([]SessionRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.faults["sessions"]; err != nil {
		return nil, err
	}
	rows := []SessionRecord{}
	for _, row := range s.rows {
		rows = append(rows, row.Clone())
	}
	return rows, nil
}
func (s *runtimeStateStore) DeleteSession(_ context.Context, id SessionID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.faults["delete"]; err != nil {
		return err
	}
	delete(s.rows, id)
	delete(s.messages, id)
	delete(s.events, id)
	delete(s.holders, id)
	return nil
}
func (s *runtimeStateStore) epochLocked(id SessionID) TranscriptEpoch {
	epoch := TranscriptEpoch(0)
	for value := range s.messages[id] {
		if len(s.messages[id][value]) > 0 && value > epoch {
			epoch = value
		}
	}
	return epoch
}
func (s *runtimeStateStore) AppendMessages(_ context.Context, id SessionID, messages []protocol.Message, epoch TranscriptEpoch) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.panicAppend {
		panic("state append canary")
	}
	if err := s.faults["append"]; err != nil {
		return 0, err
	}
	if s.messages[id] == nil {
		s.messages[id] = map[TranscriptEpoch][]protocol.Message{}
	}
	for _, m := range messages {
		s.messages[id][epoch] = append(s.messages[id][epoch], protocol.Message{Role: m.Role, Content: m.Content.Clone()})
	}
	return len(s.messages[id][epoch]), nil
}
func (s *runtimeStateStore) LoadMessages(_ context.Context, id SessionID, epoch *TranscriptEpoch) ([]protocol.Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.faults["messages"]; err != nil {
		return nil, err
	}
	value := s.epochLocked(id)
	if epoch != nil {
		value = *epoch
	}
	out := []protocol.Message{}
	for _, m := range s.messages[id][value] {
		out = append(out, protocol.Message{Role: m.Role, Content: m.Content.Clone()})
	}
	return out, nil
}
func (s *runtimeStateStore) MessageCount(_ context.Context, id SessionID, epoch *TranscriptEpoch) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.faults["count"]; err != nil {
		return 0, err
	}
	value := s.epochLocked(id)
	if epoch != nil {
		value = *epoch
	}
	n := len(s.messages[id][value])
	if s.countMismatch {
		n++
	}
	return n, nil
}
func (s *runtimeStateStore) TranscriptEpoch(_ context.Context, id SessionID) (TranscriptEpoch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.faults["epoch"]; err != nil {
		return 0, err
	}
	return s.epochLocked(id), nil
}
func (s *runtimeStateStore) AppendEvent(_ context.Context, id SessionID, record SessionEventRecord) (EventOrdinal, error) {
	s.mu.Lock()
	if err := s.faults["event"]; err != nil {
		s.mu.Unlock()
		return 0, err
	}
	s.events[id] = append(s.events[id], record.clone())
	ordinal := EventOrdinal(len(s.events[id]))
	hook := s.onEvent
	s.mu.Unlock()
	if hook != nil {
		hook(record.clone())
	}
	return ordinal, nil
}
func (s *runtimeStateStore) LoadEvents(_ context.Context, id SessionID, after EventOrdinal, limit *int) ([]SessionEventRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.faults["events"]; err != nil {
		return nil, err
	}
	out := []SessionEventRecord{}
	for i, row := range s.events[id] {
		if EventOrdinal(i+1) > after && (limit == nil || len(out) < *limit) {
			out = append(out, row.clone())
		}
	}
	return out, nil
}
func (s *runtimeStateStore) EventCursor(_ context.Context, id SessionID) (EventOrdinal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.faults["cursor"]; err != nil {
		return 0, err
	}
	return EventOrdinal(len(s.events[id])), nil
}
func (s *runtimeStateStore) AcquireLease(_ context.Context, id SessionID, owner LeaseOwner, _ time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.acquireCalls++
	if err := s.faults["acquire"]; err != nil {
		return false, err
	}
	if _, ok := s.rows[id]; !ok {
		return false, nil
	}
	if old := s.holders[id]; old != "" && old != owner {
		return false, nil
	}
	s.holders[id] = owner
	return true, nil
}
func (s *runtimeStateStore) RenewLease(_ context.Context, id SessionID, owner LeaseOwner, _ time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.renewCalls++
	if err := s.faults["renew"]; err != nil {
		return false, err
	}
	return !s.renewReject && s.holders[id] == owner, nil
}
func (s *runtimeStateStore) ReleaseLease(_ context.Context, id SessionID, owner LeaseOwner) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.releaseCalls++
	if err := s.faults["release"]; err != nil {
		return err
	}
	if s.holders[id] == owner {
		delete(s.holders, id)
	}
	return nil
}
func (s *runtimeStateStore) LeaseHolder(_ context.Context, id SessionID) (LeaseOwner, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	owner := s.holders[id]
	return owner, owner != "", nil
}
func (s *runtimeStateStore) ReadApprovals(_ context.Context, id SessionID, status *ApprovalStatus) ([]ApprovalRecord, error) {
	s.mu.Lock()
	err := s.faults["approvals"]
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	rows := s.approvalStoreSpy.snapshot()
	latest := map[ApprovalID]ApprovalRecord{}
	for _, row := range rows {
		latest[row.ApprovalID] = row
	}
	out := []ApprovalRecord{}
	for _, row := range latest {
		if row.SessionID == id && (status == nil || row.Status == *status) {
			out = append(out, row.Clone())
		}
	}
	return out, nil
}

type stateProviderFunc func(context.Context, protocol.ModelRequest) (protocol.ModelReply, error)

func (f stateProviderFunc) Complete(ctx context.Context, req protocol.ModelRequest) (protocol.ModelReply, error) {
	return f(ctx, req)
}
func stateManaged(t *testing.T, store StateStore, p Provider, owner LeaseOwner) *ManagedSession {
	t.Helper()
	session, err := NewManagedSession(RuntimeConfig{ID: "state", Owner: "tenant", Workspace: t.TempDir(), Provider: p, Bash: echoExecutor{}, Mode: ModeAuto, MaxRounds: 3, StateStore: store, StateLeaseOwner: owner})
	if err != nil {
		t.Fatal(err)
	}
	return session
}
func stateFinal() protocol.ModelReply {
	return fakeReply([]protocol.Block{protocol.NewTextBlock("finished")}, protocol.StopEndTurn)
}
func TestStatePersistenceBeforeProviderAndFinalFlush(t *testing.T) {
	store := newRuntimeStateStore()
	calls := 0
	p := stateProviderFunc(func(_ context.Context, req protocol.ModelRequest) (protocol.ModelReply, error) {
		calls++
		rows, _ := store.LoadMessages(context.Background(), "state", nil)
		if len(rows) != len(req.Messages) {
			t.Fatalf("provider saw history before write: %d/%d", len(rows), len(req.Messages))
		}
		if calls == 1 {
			return fakeReply([]protocol.Block{protocol.NewBashUse("u", "echo test")}, protocol.StopToolUse), nil
		}
		return stateFinal(), nil
	})
	session := stateManaged(t, store, p, "process")
	out, err := session.Run(context.Background(), "start")
	if err != nil || out != "finished" {
		t.Fatal(out, err)
	}
	stored, _ := store.LoadMessages(context.Background(), session.ID(), nil)
	if len(stored) != len(session.Messages()) || len(stored) != 4 {
		t.Fatal(len(stored))
	}
	store.mu.Lock()
	row := store.rows[session.ID()]
	store.mu.Unlock()
	// Source metadata is refreshed only with a growing transcript. The final
	// assistant-text beat already flushed it while status was running.
	if row.Owner != "tenant" || row.Status != StatusRunning || row.RunCount != 1 {
		t.Fatal(row)
	}
	if state := session.PersistenceStatus(); !state.Configured || state.Error != nil || !state.LeaseConfirmed {
		t.Fatal(state)
	}
	records, _ := store.LoadEvents(context.Background(), session.ID(), 0, nil)
	last := records[len(records)-1]
	if last.Event.Kind() != EventDone || last.Terminal == nil || !last.Terminal.StatePersisted {
		t.Fatal(last)
	}
}
func TestStateRewriteEpochStampAndEphemeralOrdinal(t *testing.T) {
	store := newRuntimeStateStore()
	session := stateManaged(t, store, stateProviderFunc(func(context.Context, protocol.ModelRequest) (protocol.ModelReply, error) { return stateFinal(), nil }), "")
	if _, err := session.Run(context.Background(), "old"); err != nil {
		t.Fatal(err)
	}
	session.core.mu.Lock()
	session.core.messages = []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent("summary")}}
	session.core.publishLive()
	session.core.mu.Unlock()
	before, _ := store.EventCursor(context.Background(), session.ID())
	session.core.events.append(SessionEvent{kind: EventAssistantDelta, delta: AssistantDeltaEvent{Text: "piece"}})
	session.core.events.append(SessionEvent{kind: EventStatus, status: StatusEvent{Status: StatusIdle}})
	tail, _ := store.LoadEvents(context.Background(), session.ID(), before, nil)
	if len(tail) != 1 || tail[0].TranscriptEpoch != 1 {
		t.Fatal(tail)
	}
	if epoch, _ := store.TranscriptEpoch(context.Background(), session.ID()); epoch != 2 {
		t.Fatal(epoch)
	}
	if tail[0].Sequence <= EventSequence(before)+1 {
		t.Fatal("ephemeral did not consume event sequence")
	}
	session.core.events.append(SessionEvent{kind: EventStatus, status: StatusEvent{Status: StatusIdle}})
	tail, _ = store.LoadEvents(context.Background(), session.ID(), before, nil)
	if tail[1].TranscriptEpoch != 2 {
		t.Fatal(tail)
	}
	old := TranscriptEpoch(1)
	rows, _ := store.LoadMessages(context.Background(), session.ID(), &old)
	if len(rows) != 2 {
		t.Fatal("old epoch changed", rows)
	}
}
func TestStateMaskingWritesCopiesAndParksSteering(t *testing.T) {
	store := newRuntimeStateStore()
	rawSeen := false
	p := stateProviderFunc(func(_ context.Context, r protocol.ModelRequest) (protocol.ModelReply, error) {
		encoded, _ := json.Marshal(r.Messages)
		rawSeen = strings.Contains(string(encoded), "0123456789")
		return stateFinal(), nil
	})
	session, err := NewManagedSession(RuntimeConfig{ID: "masked", Owner: "tenant", Workspace: t.TempDir(), Provider: p, Bash: echoExecutor{}, Mode: ModeAuto, MaxRounds: 2, StateStore: store, Secrets: runtimeSecrets()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = session.Steer(runtimeCanary); err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	steer := store.rows[session.ID()].PendingSteering
	store.mu.Unlock()
	if len(steer) != 1 || strings.Contains(steer[0], "0123456789") {
		t.Fatal(steer)
	}
	if _, err = session.Run(context.Background(), runtimeCanary); err != nil {
		t.Fatal(err)
	}
	if !rawSeen {
		t.Fatal("live model lost raw prompt")
	}
	messages, _ := store.LoadMessages(context.Background(), session.ID(), nil)
	encoded, _ := json.Marshal(messages)
	if strings.Contains(string(encoded), "0123456789") {
		t.Fatal("stored transcript leaked")
	}
	events, _ := store.LoadEvents(context.Background(), session.ID(), 0, nil)
	encoded, _ = json.Marshal(events)
	if strings.Contains(string(encoded), "0123456789") {
		t.Fatal("stored event leaked")
	}
	for _, event := range events {
		if event.Scope.RunContext.Authority() != AuthorityUntrusted {
			t.Fatal("stored event retained authority")
		}
	}
	store.mu.Lock()
	steer = store.rows[session.ID()].PendingSteering
	store.mu.Unlock()
	if len(steer) != 0 {
		t.Fatal(steer)
	}
}
func TestStateOrdinaryWriteFaultsDegradeAndGuardFailuresStop(t *testing.T) {
	for _, kind := range []string{"event", "append", "upsert", "renew", "panic", "count", "mismatch"} {
		t.Run(kind, func(t *testing.T) {
			store := newRuntimeStateStore()
			calls := 0
			session := stateManaged(t, store, stateProviderFunc(func(context.Context, protocol.ModelRequest) (protocol.ModelReply, error) {
				calls++
				return stateFinal(), nil
			}), "process")
			store.mu.Lock()
			switch kind {
			case "panic":
				store.panicAppend = true
			case "mismatch":
				store.countMismatch = true
			default:
				store.faults[kind] = errors.New("store failed")
			}
			store.mu.Unlock()
			_, err := session.Run(context.Background(), "start")
			fatal := kind == "count" || kind == "mismatch"
			if fatal {
				if !errors.Is(err, ErrStateTranscript) || calls != 0 {
					t.Fatal(err, calls)
				}
			} else {
				if err != nil || calls != 1 || session.PersistenceStatus().Error == nil {
					t.Fatal(err, calls, session.PersistenceStatus())
				}
			}
		})
	}
}
func TestStateConfirmedLeaseLossStopsBeforeProviderOrTool(t *testing.T) {
	for _, boundary := range []string{"model", "tool", "terminal"} {
		t.Run(boundary, func(t *testing.T) {
			store := newRuntimeStateStore()
			calls := 0
			bash := &countedBashExecutor{}
			p := stateProviderFunc(func(context.Context, protocol.ModelRequest) (protocol.ModelReply, error) {
				calls++
				if boundary == "tool" {
					return fakeReply([]protocol.Block{protocol.NewBashUse("u", "echo effect")}, protocol.StopToolUse), nil
				}
				return stateFinal(), nil
			})
			session, err := NewManagedSession(RuntimeConfig{ID: "state", Owner: "tenant", Workspace: t.TempDir(), Provider: p, Bash: bash, Mode: ModeAuto, MaxRounds: 3, StateStore: store, StateLeaseOwner: "process"})
			if err != nil {
				t.Fatal(err)
			}
			store.onEvent = func(record SessionEventRecord) {
				if (boundary == "tool" && record.Event.Kind() == EventToolUse) || (boundary == "terminal" && record.Event.Kind() == EventDone) {
					store.mu.Lock()
					store.holders[session.ID()] = "foreign"
					store.mu.Unlock()
					if boundary == "terminal" {
						// Renewal is growth-triggered, not a heartbeat. Supply a
						// terminal tail so this beat actually needs a flush.
						session.core.mu.Lock()
						session.core.appendMessages(protocol.Message{Role: protocol.RoleUser, Content: protocol.PlainContent("late tail")})
						session.core.mu.Unlock()
					}
				}
			}
			if boundary == "model" {
				store.renewReject = true
			}
			_, err = session.Run(context.Background(), "start")
			if !errors.Is(err, ErrSessionLeaseLost) || session.Info().Status != StatusError || bash.calls != 0 {
				t.Fatal(err, session.Info().Status, bash.calls)
			}
			if boundary == "model" && calls != 0 {
				t.Fatal(calls)
			}
			for _, event := range session.Events() {
				if event.Event.Kind() == EventDone {
					t.Fatal("published done after lease loss")
				}
			}
		})
	}
}
func TestStateUnconfirmedRenewalAndTurnAcquisition(t *testing.T) {
	store := newRuntimeStateStore()
	store.holders["state"] = "foreign"
	calls := 0
	session := stateManaged(t, store, stateProviderFunc(func(context.Context, protocol.ModelRequest) (protocol.ModelReply, error) {
		calls++
		return stateFinal(), nil
	}), "process")
	if session.PersistenceStatus().LeaseConfirmed {
		t.Fatal("foreign lease confirmed")
	}
	// Mirrors a source bare-agent embedding: no confirmed hold, failed renewal is
	// non-fatal. The public ManagedSession admission must still reject that hold.
	session.core.mu.Lock()
	session.core.messages = []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent("seed")}}
	session.core.publishLive()
	session.core.mu.Unlock()
	session.core.events.append(SessionEvent{kind: EventStatus, status: StatusEvent{Status: StatusIdle}})
	if _, err := session.Run(context.Background(), "start"); !errors.Is(err, ErrSessionLeaseLost) || calls != 0 {
		t.Fatal(err, calls)
	}
	store.mu.Lock()
	delete(store.holders, "state")
	store.mu.Unlock()
	if _, err := session.Run(context.Background(), "start"); err != nil {
		t.Fatal(err)
	}
	if !session.PersistenceStatus().LeaseConfirmed {
		t.Fatal("new claim not confirmed")
	}
}

func TestStateManagerCompositionForkAndOwnerBoundaries(t *testing.T) {
	store := newRuntimeStateStore()
	_ = store.WriteAction(context.Background(), ActionRecord{ActionID: "started", SessionID: "old", Status: ActionStarted})
	config := managerTestConfig(t.TempDir(), stateProviderFunc(func(context.Context, protocol.ModelRequest) (protocol.ModelReply, error) { return stateFinal(), nil }))
	config.Services.StateStore = store
	manager := makeManager(t, config)
	journal, ok := manager.config.Services.ActionJournal.(*StoredActionJournal)
	if !ok || journal.store != store || manager.Approvals().store != store {
		t.Fatal("default services missed state store")
	}
	action, _, _ := store.ReadAction(context.Background(), "started")
	if action.Status != ActionUnknown {
		t.Fatal("manager did not explicitly mark unknown", action)
	}
	system := "explicit system"
	session := createManaged(t, manager, CreateSessionRequest{Owner: "tenant", System: &system})
	holder, held, _ := store.LeaseHolder(context.Background(), session.ID())
	if !held || holder == LeaseOwner(session.Owner()) {
		t.Fatal("tenant became process identity", holder)
	}
	if _, err := session.Run(context.Background(), "start"); err != nil {
		t.Fatal(err)
	}
	fork, err := manager.Fork(context.Background(), "tenant", session.ID())
	if err != nil {
		t.Fatal(err)
	}
	messages, _ := store.LoadMessages(context.Background(), fork.ID(), nil)
	if len(messages) != len(session.Messages()) {
		t.Fatal("fork published before seed flush")
	}
	store.mu.Lock()
	row := store.rows[fork.ID()]
	store.mu.Unlock()
	if row.WorkspaceBound || row.System == nil || *row.System != system || row.Owner != "tenant" {
		t.Fatal(row)
	}
	if deleted, err := manager.Delete("foreign", session.ID(), DeleteSessionOptions{}); deleted || !errors.Is(err, ErrSessionNotFound) {
		t.Fatal("foreign deletion allowed", err)
	}
	if _, held, _ := store.LeaseHolder(context.Background(), session.ID()); !held {
		t.Fatal("foreign deletion touched lease")
	}
	if err := manager.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, held, _ := store.LeaseHolder(context.Background(), session.ID()); held {
		t.Fatal("stop retained lease")
	}
	rows, _ := store.LoadSessions(context.Background())
	if len(rows) != 2 {
		t.Fatal("stop deleted state", rows)
	}
	// Explicit services keep their own stores; composing them must not retarget
	// caller-owned objects or apply the default global unknown policy.
	other := newRuntimeStateStore()
	_ = other.WriteAction(context.Background(), ActionRecord{ActionID: "untouched", Status: ActionStarted})
	broker, _ := NewApprovalBroker(ApprovalBrokerConfig{})
	memory, _ := NewInMemoryActionJournal(DefaultResultsRetained)
	config = managerTestConfig(t.TempDir(), &FakeProvider{})
	config.Services.StateStore = other
	config.Services.Approvals = broker
	config.Services.ActionJournal = memory
	custom := makeManager(t, config)
	if custom.Approvals() != broker || broker.store != nil || custom.config.Services.ActionJournal != memory {
		t.Fatal("shared service mutated")
	}
	action, _, _ = other.ReadAction(context.Background(), "untouched")
	if action.Status != ActionStarted {
		t.Fatal(action)
	}
}
func TestStateDeleteDisablesLateWritesAndPreservesAudit(t *testing.T) {
	store := newRuntimeStateStore()
	provider := newDrainingProvider()
	config := managerTestConfig(t.TempDir(), provider)
	config.Services.StateStore = store
	manager := makeManager(t, config)
	session := createManaged(t, manager, CreateSessionRequest{Owner: "tenant"})
	_ = store.WriteAction(context.Background(), ActionRecord{ActionID: "audit", SessionID: session.ID(), Status: ActionCompleted})
	done := make(chan error, 1)
	go func() { _, err := session.Run(context.Background(), "parked"); done <- err }()
	receiveSignal(t, provider.entered)
	if _, err := manager.Delete("tenant", session.ID(), DeleteSessionOptions{}); err != nil {
		t.Fatal(err)
	}
	receiveSignal(t, provider.cancelled)
	rows, _ := store.LoadSessions(context.Background())
	if len(rows) != 0 {
		t.Fatal("row retained during cancellation")
	}
	close(provider.release)
	if err := receiveRun(t, done); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := manager.WaitCleanup(context.Background()); err != nil {
		t.Fatal(err)
	}
	session.core.events.append(SessionEvent{kind: EventStatus, status: StatusEvent{Status: StatusIdle}})
	rows, _ = store.LoadSessions(context.Background())
	events, _ := store.LoadEvents(context.Background(), session.ID(), 0, nil)
	if len(rows) != 0 || len(events) != 0 {
		t.Fatal("late event resurrected deleted state")
	}
	if _, ok, _ := store.ReadAction(context.Background(), "audit"); !ok {
		t.Fatal("audit removed")
	}
}
func TestStateQueuedLeaseIsAcquiredAfterAdmission(t *testing.T) {
	store := newRuntimeStateStore()
	provider := &parkedProvider{started: make(chan struct{})}
	session := stateManaged(t, store, provider, "process")
	first := make(chan error, 1)
	go func() { _, err := session.Run(context.Background(), "first"); first <- err }()
	receiveSignal(t, provider.started)
	ctx := &admissionProbeContext{Context: context.Background(), waiting: make(chan struct{})}
	second := make(chan error, 1)
	go func() { _, err := session.Run(ctx, "queued"); second <- err }()
	receiveSignal(t, ctx.waiting)
	store.mu.Lock()
	claims := store.acquireCalls
	store.holders[session.ID()] = "foreign"
	store.mu.Unlock()
	if claims != 2 {
		t.Fatal("queued call claimed before admission", claims)
	}
	if _, err := session.Cancel(context.Background(), "stop"); err != nil {
		t.Fatal(err)
	}
	if err := receiveRun(t, first); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := receiveRun(t, second); !errors.Is(err, ErrSessionLeaseLost) {
		t.Fatal(err)
	}
	provider.mu.Lock()
	calls := provider.calls
	provider.mu.Unlock()
	if calls != 1 {
		t.Fatal("queued caller reached model", calls)
	}
}
func TestStateStopReleasesLeaseAfterDrainAndReportsCleanupFaults(t *testing.T) {
	store := newRuntimeStateStore()
	provider := newDrainingProvider()
	config := managerTestConfig(t.TempDir(), provider)
	config.Services.StateStore = store
	manager := makeManager(t, config)
	session := createManaged(t, manager, CreateSessionRequest{Owner: "tenant"})
	done := make(chan error, 1)
	go func() { _, err := session.Run(context.Background(), "parked"); done <- err }()
	receiveSignal(t, provider.entered)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := manager.Stop(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	receiveSignal(t, provider.cancelled)
	if _, held, _ := store.LeaseHolder(context.Background(), session.ID()); !held {
		t.Fatal("lease released before provider drain")
	}
	store.mu.Lock()
	store.faults["release"] = errors.New("release failed")
	store.mu.Unlock()
	close(provider.release)
	_ = receiveRun(t, done)
	if err := manager.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(manager.CleanupErrors()) != 1 {
		t.Fatal(manager.CleanupErrors())
	}
	if session.PersistenceStatus().LeaseConfirmed {
		t.Fatal("stopped handle retained confirmation")
	}
}
func TestStateDeleteFailureStillDisownsAndDoesNotResurrect(t *testing.T) {
	store := newRuntimeStateStore()
	config := managerTestConfig(t.TempDir(), &FakeProvider{})
	config.Services.StateStore = store
	manager := makeManager(t, config)
	session := createManaged(t, manager, CreateSessionRequest{Owner: "tenant"})
	store.mu.Lock()
	store.faults["delete"] = errors.New("delete failed")
	store.faults["release"] = errors.New("release failed")
	store.mu.Unlock()
	if deleted, err := manager.Delete("tenant", session.ID(), DeleteSessionOptions{}); !deleted || err != nil {
		t.Fatal(err)
	}
	problems := manager.CleanupErrors()
	if len(problems) != 1 || !strings.Contains(problems[0].Error, "delete failed") || !strings.Contains(problems[0].Error, "release failed") {
		t.Fatal(problems)
	}
	store.mu.Lock()
	delete(store.faults, "delete")
	delete(store.faults, "release")
	before := len(store.events[session.ID()])
	store.mu.Unlock()
	session.core.events.append(SessionEvent{kind: EventStatus, status: StatusEvent{Status: StatusIdle}})
	store.mu.Lock()
	after := len(store.events[session.ID()])
	store.mu.Unlock()
	if before != after {
		t.Fatal("disabled handle continued storage")
	}
}

type stateSessionContract struct {
	CountBeforeProvider  int           `json:"count_before_provider"`
	CursorAfterEphemeral EventOrdinal  `json:"cursor_after_ephemeral"`
	Queued               []string      `json:"queued"`
	LiveRaw              bool          `json:"live_raw"`
	StoredRaw            bool          `json:"stored_raw"`
	FinalStatus          SessionStatus `json:"final_status_without_growth"`
	TerminalPersisted    bool          `json:"terminal_state_persisted"`
	Rewrite              struct {
		Epoch        TranscriptEpoch `json:"epoch"`
		OldCount     int             `json:"old_count"`
		CurrentCount int             `json:"current_count"`
		Epochs       []int           `json:"event_epochs"`
		Seqs         []EventSequence `json:"event_seqs"`
		Cursor       EventOrdinal    `json:"physical_cursor"`
	} `json:"rewrite"`
	ConfirmedStopped   bool `json:"confirmed_loss_stopped"`
	UnconfirmedStopped bool `json:"unconfirmed_loss_stopped"`
	Failures           []struct {
		Name     string `json:"name"`
		Stopped  bool   `json:"stopped"`
		Reported bool   `json:"reported"`
	} `json:"failures"`
}

func TestStateActualPythonSessionRecipe(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-state-session.json")
	if err != nil {
		t.Fatal(err)
	}
	var source stateSessionContract
	if err = json.Unmarshal(data, &source); err != nil {
		t.Fatal(err)
	}
	store := newRuntimeStateStore()
	session, err := NewManagedSession(RuntimeConfig{ID: "state", Owner: "tenant", Workspace: t.TempDir(), Provider: &FakeProvider{}, Bash: echoExecutor{}, Mode: ModeAuto, MaxRounds: 2, StateStore: store, StateLeaseOwner: "process", Secrets: runtimeSecrets()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = session.Steer("queued " + runtimeCanary); err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	queue := store.rows[session.ID()].PendingSteering
	store.mu.Unlock()
	if !reflect.DeepEqual(queue, source.Queued) {
		t.Fatal(queue, source.Queued)
	}
	session.status, session.runCount = StatusRunning, 1
	session.core.messages = []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent(runtimeCanary)}}
	session.core.publishLive()
	if err = session.core.persistence.guard(session.core.messages); err != nil {
		t.Fatal(err)
	}
	count, _ := store.MessageCount(context.Background(), session.ID(), nil)
	if count != source.CountBeforeProvider {
		t.Fatal(count)
	}
	session.core.events.append(SessionEvent{kind: EventAssistantDelta, delta: AssistantDeltaEvent{Text: "piece"}})
	cursor, _ := store.EventCursor(context.Background(), session.ID())
	if cursor != source.CursorAfterEphemeral {
		t.Fatal(cursor)
	}
	session.core.events.append(SessionEvent{kind: EventStatus, status: StatusEvent{Status: StatusRunning}})
	session.core.appendMessages(protocol.Message{Role: protocol.RoleAssistant, Content: protocol.BlockContent(protocol.NewThinkingBlock("reason "+runtimeCanary, "opaque-signature"), protocol.NewTextBlock(runtimeCanary))})
	session.core.events.append(SessionEvent{kind: EventAssistantText, assistantText: AssistantTextEvent{Text: runtimeCanary, Phase: PhaseFinalAnswer}})
	session.status = StatusIdle
	session.finishTrajectory(RunContext{}, SessionEvent{kind: EventDone, done: DoneEvent{Text: "done", Phase: PhaseFinalAnswer}}, TrajectoryCompleted, nil, nil)
	store.mu.Lock()
	row := store.rows[session.ID()]
	store.mu.Unlock()
	if row.Status != source.FinalStatus {
		t.Fatal(row.Status, source.FinalStatus)
	}
	messages, _ := store.LoadMessages(context.Background(), session.ID(), nil)
	encoded, _ := json.Marshal(messages)
	live, _ := session.core.messages[0].Content.Plain()
	if (live == runtimeCanary) != source.LiveRaw || strings.Contains(string(encoded), "0123456789") != source.StoredRaw {
		t.Fatal("source masking mismatch")
	}
	records, _ := store.LoadEvents(context.Background(), session.ID(), 0, nil)
	if records[len(records)-1].Terminal.StatePersisted != source.TerminalPersisted {
		t.Fatal("terminal projection differs")
	}
	session.core.messages = []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent("summary")}}
	session.core.publishLive()
	session.core.control.steering = nil
	session.core.events.append(SessionEvent{kind: EventStatus, status: StatusEvent{Status: StatusIdle}})
	session.core.events.append(SessionEvent{kind: EventStatus, status: StatusEvent{Status: StatusIdle}})
	epoch, _ := store.TranscriptEpoch(context.Background(), session.ID())
	old := TranscriptEpoch(1)
	oldCount, _ := store.MessageCount(context.Background(), session.ID(), &old)
	currentCount, _ := store.MessageCount(context.Background(), session.ID(), nil)
	cursor, _ = store.EventCursor(context.Background(), session.ID())
	if epoch != source.Rewrite.Epoch || oldCount != source.Rewrite.OldCount || currentCount != source.Rewrite.CurrentCount || cursor != source.Rewrite.Cursor {
		t.Fatal(epoch, oldCount, currentCount, cursor)
	}
	records, _ = store.LoadEvents(context.Background(), session.ID(), 0, nil)
	epochs := []int{}
	seqs := []EventSequence{}
	for _, event := range records {
		epochs = append(epochs, event.TranscriptEpoch)
		seqs = append(seqs, event.Sequence)
	}
	if !reflect.DeepEqual(epochs, source.Rewrite.Epochs) || !reflect.DeepEqual(seqs, source.Rewrite.Seqs) {
		t.Fatal(epochs, seqs)
	}
	for _, confirmed := range []bool{true, false} {
		backing := newRuntimeStateStore()
		s := stateManaged(t, backing, &FakeProvider{}, "process")
		s.core.persistence.confirmed = confirmed
		backing.renewReject = true
		s.core.messages = []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent("start")}}
		s.core.publishLive()
		err := s.core.persistence.capture(&SessionEventRecord{Sequence: 1, SessionID: s.ID(), TranscriptEpoch: 1, Event: SessionEvent{kind: EventStatus, status: StatusEvent{Status: StatusRunning}}})
		expected := source.UnconfirmedStopped
		if confirmed {
			expected = source.ConfirmedStopped
		}
		if errors.Is(err, ErrSessionLeaseLost) != expected {
			t.Fatal(confirmed, err)
		}
	}
	for _, failure := range source.Failures {
		t.Run(failure.Name, func(t *testing.T) {
			backing := newRuntimeStateStore()
			s := stateManaged(t, backing, &FakeProvider{}, "")
			s.core.messages = []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent("start")}}
			s.core.publishLive()
			if failure.Name == "count" {
				if err := s.core.persistence.guard(s.core.messages); err != nil {
					t.Fatal(err)
				}
			}
			backing.faults[failure.Name] = errors.New("fixture write fault")
			var err error
			if failure.Name == "event" {
				err = s.core.persistence.capture(&SessionEventRecord{Sequence: 1, SessionID: s.ID(), TranscriptEpoch: 1, Event: SessionEvent{kind: EventStatus, status: StatusEvent{Status: StatusRunning}}})
			} else {
				err = s.core.persistence.guard(s.core.messages)
			}
			if (err != nil) != failure.Stopped || (s.PersistenceStatus().Error != nil) != failure.Reported {
				t.Fatal(failure, err, s.PersistenceStatus())
			}
		})
	}
}

func TestStateConstructorsRejectInvalidConfigurationAndReportClaimCleanup(t *testing.T) {
	store := newRuntimeStateStore()
	for _, config := range []RuntimeConfig{{StateLeaseTTL: time.Second}, {StateLeaseOwner: "process"}, {StateStore: store, StateLeaseTTL: -time.Second}} {
		if _, err := NewManagedSession(config); err == nil {
			t.Fatal("invalid lease configuration accepted")
		}
	}
	if _, err := NewRuntimeSession(RuntimeConfig{StateStore: store}); err == nil {
		t.Fatal("bare runtime accepted managed state configuration")
	}
	config := managerTestConfig(t.TempDir(), &FakeProvider{})
	config.StateLeaseTTL = time.Second
	if _, err := NewSessionManager(config); err == nil {
		t.Fatal("manager accepted lease TTL without store")
	}
	store.faults["acquire"] = errors.New("claim failed")
	store.faults["release"] = errors.New("release failed")
	_, err := NewManagedSession(RuntimeConfig{ID: "state", Owner: "tenant", Workspace: t.TempDir(), Provider: &FakeProvider{}, Bash: echoExecutor{}, Mode: ModeAuto, MaxRounds: 2, StateStore: store, StateLeaseOwner: "process"})
	if err == nil || !strings.Contains(err.Error(), "claim failed") || !strings.Contains(err.Error(), "release failed") {
		t.Fatal(err)
	}
}

type stateTraceWriter struct{}

func (stateTraceWriter) Start(TrajectoryStart) (TrajectoryID, error) { return "trace", nil }
func (stateTraceWriter) Append(TrajectoryID, TrajectoryRecord) error { return nil }
func (stateTraceWriter) Finish(TrajectoryID, TrajectoryFinish) error { return nil }
func (stateTraceWriter) Count(SessionID) (int, error)                { return 0, nil }
func TestStateTerminalStoresTrajectoryJoinBeforeFinalization(t *testing.T) {
	store := newRuntimeStateStore()
	session := stateManaged(t, store, stateProviderFunc(func(context.Context, protocol.ModelRequest) (protocol.ModelReply, error) { return stateFinal(), nil }), "")
	session.core.events.trajectory.store = stateTraceWriter{}
	if _, err := session.Run(context.Background(), "start"); err != nil {
		t.Fatal(err)
	}
	rows, _ := store.LoadEvents(context.Background(), session.ID(), 0, nil)
	last := rows[len(rows)-1]
	if last.Event.Kind() != EventDone || last.Trajectory == nil || last.Trajectory.ID == nil || last.Trajectory.TraceID == nil || last.Trajectory.GroupID == nil || *last.Trajectory.ID != "trace" || *last.Trajectory.TraceID != "trace" || *last.Trajectory.GroupID != session.ID() {
		t.Fatal("terminal lost pre-finalization join fields", last)
	}
	if last.Terminal == nil || last.Terminal.Persisted != nil {
		t.Fatal("state row claimed later trajectory finalization", last.Terminal)
	}
	// Non-terminal trace IDs are attached only after state capture in the source.
	for _, row := range rows[:len(rows)-1] {
		if row.Trajectory != nil && row.Trajectory.ID != nil {
			t.Fatal("ordinary state row includes later trajectory stamp", row)
		}
	}
}
