package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/protocol"
)

type forkFixture struct {
	Initial              SessionInfo
	History              []protocol.Message
	ChildRequestModel    string          `json:"child_request_model"`
	ChildRequestSystem   json.RawMessage `json:"child_request_system"`
	ChildRequestMessages json.RawMessage `json:"child_request_messages"`
	SourceEvents         []struct {
		Type         SessionEventKind
		Child        SessionID
		MessageCount int `json:"message_count"`
	} `json:"source_events"`
	SourcePending     int  `json:"source_pending"`
	RowsIndependent   bool `json:"rows_independent"`
	SourceUnchanged   bool `json:"source_unchanged"`
	WorkspaceDistinct bool `json:"workspace_distinct"`
	ChildMarkerExists bool `json:"child_marker_exists"`
	Empty             struct {
		MessageCount int         `json:"message_count"`
		ForkedFrom   ForkLineage `json:"forked_from"`
	}
}

type forkProvider struct{ requests []protocol.ModelRequest }

func (p *forkProvider) Complete(_ context.Context, r protocol.ModelRequest) (protocol.ModelReply, error) {
	p.requests = append(p.requests, r.Clone())
	if len(p.requests) == 1 {
		return fakeReply([]protocol.Block{protocol.NewToolUseWithoutCaller("todo", protocol.TodoWriteToolInput(protocol.TodoWriteInput{Items: []protocol.TodoItem{{Content: "source task", Status: protocol.TodoPending, ActiveForm: "working"}}}))}, protocol.StopToolUse), nil
	}
	return fakeReply([]protocol.Block{protocol.NewTextBlock("noted")}, protocol.StopEndTurn), nil
}
func TestForkMatchesActualPythonHistoryAndFreshState(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-forks.json")
	if err != nil {
		t.Fatal(err)
	}
	var want forkFixture
	if err = json.Unmarshal(data, &want); err != nil {
		t.Fatal(err)
	}
	p := &forkProvider{}
	config := managerTestConfig(filepath.Join(t.TempDir(), "root"), p)
	config.Defaults.Model = "default-model"
	m := makeManager(t, config)
	system, model := "fixed source system", "source-model"
	source := createManaged(t, m, CreateSessionRequest{Owner: "alice", System: &system, Model: &model, PermissionMode: ModeAuto})
	if err = os.WriteFile(filepath.Join(source.Info().Workspace, "source-only.txt"), []byte("source"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = source.Run(context.Background(), "the codeword is xyzzy"); err != nil {
		t.Fatal(err)
	}
	source.Steer("parked source input")
	source.ChangePermissionMode(ModeReadonly)
	child, err := m.Fork(context.Background(), "alice", source.ID())
	if err != nil {
		t.Fatal(err)
	}
	got := child.Info()
	got.ID = ""
	got.CreatedAt = 0
	got.Workspace = ""
	got.ForkedFrom.Session = "source"
	if !reflect.DeepEqual(got, want.Initial) {
		t.Fatalf("initial got %+v want %+v", got, want.Initial)
	}
	if !reflect.DeepEqual(child.Messages(), want.History) {
		a, _ := json.Marshal(child.Messages())
		b, _ := json.Marshal(want.History)
		t.Fatalf("history %s != %s", a, b)
	}
	for i, msg := range child.Messages() {
		if msg.Content.SameStorage(source.Messages()[i].Content) {
			t.Fatal("fork shares transcript storage")
		}
	}
	if (child.Info().Workspace != source.Info().Workspace) != want.WorkspaceDistinct {
		t.Fatal("workspace was copied")
	}
	_, err = os.Stat(filepath.Join(child.Info().Workspace, "source-only.txt"))
	if (!os.IsNotExist(err)) != want.ChildMarkerExists {
		t.Fatal("source file copied")
	}
	if _, err = child.Run(context.Background(), "what was the codeword?"); err != nil {
		t.Fatal(err)
	}
	r := p.requests[len(p.requests)-1]
	wire, err := r.Wire()
	if err != nil {
		t.Fatal(err)
	}
	wdata, _ := json.Marshal(wire)
	var projection struct {
		System   json.RawMessage
		Messages json.RawMessage
	}
	json.Unmarshal(wdata, &projection)
	compare := func(a, b []byte) {
		t.Helper()
		var x, y bytes.Buffer
		json.Compact(&x, a)
		json.Compact(&y, b)
		var xv, yv interface{}
		json.Unmarshal(x.Bytes(), &xv)
		json.Unmarshal(y.Bytes(), &yv)
		if !reflect.DeepEqual(xv, yv) {
			t.Fatalf("wire differs: %s != %s", a, b)
		}
	}
	// Open JSON comparison lives only at this external fixture boundary.
	compare(projection.System, want.ChildRequestSystem)
	compare(projection.Messages, want.ChildRequestMessages)
	if r.Model != want.ChildRequestModel || reflect.DeepEqual(source.Messages(), want.History) != want.SourceUnchanged || source.Info().PendingSteering != want.SourcePending {
		t.Fatal("source state or default model changed")
	}
	eventCount := 0
	for _, record := range source.Events() {
		if event, ok := record.Event.SessionForked(); ok {
			eventCount++
			eventData, _ := json.Marshal(record)
			var eventWire struct {
				Type         SessionEventKind
				Child        SessionID
				MessageCount int `json:"message_count"`
				Agent        *string
			}
			json.Unmarshal(eventData, &eventWire)
			if eventWire.Agent != nil || eventWire.Type != want.SourceEvents[0].Type || event.Child != child.ID() || event.MessageCount != want.SourceEvents[0].MessageCount {
				t.Fatal("source event differs")
			}
		}
	}
	if eventCount != len(want.SourceEvents) {
		t.Fatal("source event missing")
	}
	child.core.mu.Lock()
	child.core.messages[0] = protocol.Message{Role: protocol.RoleUser, Content: protocol.PlainContent("EDITED-IN-CHILD")}
	child.core.publishLive()
	child.core.mu.Unlock()
	if reflect.DeepEqual(source.Messages(), want.History) != want.RowsIndependent {
		t.Fatal("rows share state")
	}
	empty := createManaged(t, m, CreateSessionRequest{Owner: "alice"})
	emptyChild, err := m.Fork(context.Background(), "alice", empty.ID())
	if err != nil {
		t.Fatal(err)
	}
	lineage := emptyChild.Info().ForkedFrom
	lineage.Session = "empty"
	if emptyChild.Info().MessageCount != want.Empty.MessageCount || *lineage != want.Empty.ForkedFrom {
		t.Fatal("empty fork differs")
	}
	if child.Info().ForkedFrom.Session != source.ID() {
		t.Fatal("Info shares lineage pointer")
	}
}

func TestForkRefusesOpenOrInvalidBoundaryAndCopiesCancelledPairs(t *testing.T) {
	p := newDrainingProvider()
	m := makeManager(t, managerTestConfig(t.TempDir(), p))
	s := createManaged(t, m, CreateSessionRequest{Owner: "alice"})
	done := make(chan error, 1)
	go func() { _, err := s.Run(context.Background(), "go"); done <- err }()
	receiveSignal(t, p.entered)
	if _, err := m.Fork(context.Background(), "alice", s.ID()); !errors.Is(err, ErrForkBusy) {
		t.Fatal(err)
	}
	for _, owner := range []OwnerID{"bob", ""} {
		if _, err := m.Fork(context.Background(), owner, s.ID()); !errors.Is(err, ErrSessionNotFound) {
			t.Fatal(err)
		}
	}
	cancelled := make(chan struct{})
	go func() { s.Cancel(context.Background(), "cancel"); close(cancelled) }()
	receiveSignal(t, p.cancelled)
	close(p.release)
	<-cancelled
	<-done
	child, err := m.Fork(context.Background(), "alice", s.ID())
	if err != nil {
		t.Fatal(err)
	}
	if err = protocol.ValidateTranscript(child.Messages()); err != nil {
		t.Fatal(err)
	}
	s.core.mu.Lock()
	s.core.messages = append(s.core.messages, protocol.Message{Role: protocol.RoleAssistant, Content: protocol.BlockContent(protocol.NewBashUse("open", "true"))})
	s.core.publishLive()
	s.core.mu.Unlock()
	before := len(m.List("alice"))
	if _, err = m.Fork(context.Background(), "alice", s.ID()); err == nil {
		t.Fatal("open tool call forked")
	}
	if len(m.List("alice")) != before {
		t.Fatal("invalid fork published")
	}
}

func TestForkPublishesInitializedHistoryAndShutdownJoinsCreation(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	config := managerTestConfig(t.TempDir(), &fakeSequenceProvider{replies: []protocol.ModelReply{fakeReply([]protocol.Block{protocol.NewTextBlock("done")}, protocol.StopEndTurn), fakeReply([]protocol.Block{protocol.NewTextBlock("done")}, protocol.StopEndTurn)}})
	config.Services.BashFactory = bashFactoryFunc(func(_ context.Context, _ SessionBinding) (BashExecutor, error) {
		if calls.Add(1) > 1 {
			close(entered)
			<-release
		}
		return echoExecutor{}, nil
	})
	m := makeManager(t, config)
	s := createManaged(t, m, CreateSessionRequest{Owner: "alice"})
	s.Run(context.Background(), "go")
	result := make(chan error, 1)
	go func() { _, err := m.Fork(context.Background(), "alice", s.ID()); result <- err }()
	receiveSignal(t, entered)
	if len(m.List("alice")) != 1 {
		t.Fatal("partially initialized child exposed")
	}
	receipt, err := s.SubmitSteering("racing turn")
	if err != nil || receipt.Delivered != DeliveryNewTurn {
		t.Fatal("fork stranded idle wakeup", receipt, err)
	}
	deadline := time.Now().Add(time.Second)
	for s.Info().Busy {
		if time.Now().After(deadline) {
			t.Fatal("source wakeup did not finish")
		}
		time.Sleep(time.Millisecond)
	}
	if s.Info().RunCount != 2 || s.Info().PendingSteering != 0 {
		t.Fatal("source wakeup parked behind fork")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := m.Stop(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	close(release)
	if err := <-result; !errors.Is(err, ErrManagerStopped) {
		t.Fatal(err)
	}
	if err := m.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(m.List("alice")) != 1 {
		t.Fatal("fork published after stop")
	}
	for _, e := range s.Events() {
		if e.Event.Kind() == EventSessionForked {
			t.Fatal("failed fork left source event")
		}
	}
	entries, err := os.ReadDir(config.WorkspaceRoot)
	if err != nil || len(entries) != 1 {
		t.Fatal("unpublished scratch leaked", err, entries)
	}
}

func forkTestRun(t *testing.T) RunContext {
	t.Helper()
	run, err := DefaultRunContext()
	if err != nil {
		t.Fatal(err)
	}
	return run
}

func TestForkAfterToolCancellationRetainsPairedCompletedAndUnknownResults(t *testing.T) {
	executor := &blockingExecutor{entered: make(chan struct{}), completeFirst: true}
	config := managerTestConfig(t.TempDir(), cancelledBatchProvider{})
	config.Services.BashFactory = bashFactoryFunc(func(context.Context, SessionBinding) (BashExecutor, error) { return executor, nil })
	m := makeManager(t, config)
	source := createManaged(t, m, CreateSessionRequest{Owner: "alice"})
	done := make(chan error, 1)
	go func() { _, err := source.Run(context.Background(), "go"); done <- err }()
	receiveSignal(t, executor.entered)
	if _, err := m.Fork(context.Background(), "alice", source.ID()); !errors.Is(err, ErrForkBusy) {
		t.Fatal("fork accepted unmatched dispatched tools", err)
	}
	source.Cancel(context.Background(), "cancel")
	<-done
	child, err := m.Fork(context.Background(), "alice", source.ID())
	if err != nil {
		t.Fatal(err)
	}
	if err = protocol.ValidateTranscript(child.Messages()); err != nil {
		t.Fatal(err)
	}
	messages := child.Messages()
	blocks, _ := messages[len(messages)-1].Content.Blocks()
	if len(blocks) != 2 {
		t.Fatal("results missing")
	}
	first, _ := blocks[0].ToolResult()
	second, _ := blocks[1].ToolResult()
	if first.Content != "first completed" || second.Content != unknownToolResult {
		t.Fatal("cancelled effects rewritten", first, second)
	}
	if child.Info().CancelReason != nil || child.Info().Status != StatusIdle {
		t.Fatal("child inherited cancelled holder")
	}
}

func TestForkConstructionFailureReleasesSourceAdmissionAndReportsNoEvent(t *testing.T) {
	var calls atomic.Int32
	config := managerTestConfig(t.TempDir(), &fakeSequenceProvider{replies: []protocol.ModelReply{fakeReply([]protocol.Block{protocol.NewTextBlock("done")}, protocol.StopEndTurn)}})
	config.Services.BashFactory = bashFactoryFunc(func(context.Context, SessionBinding) (BashExecutor, error) {
		if calls.Add(1) == 2 {
			return nil, errors.New("factory failed")
		}
		return echoExecutor{}, nil
	})
	m := makeManager(t, config)
	source := createManaged(t, m, CreateSessionRequest{Owner: "alice"})
	if _, err := m.Fork(context.Background(), "alice", source.ID()); err == nil {
		t.Fatal("factory fault ignored")
	}
	if _, err := source.TryRunWithContext(context.Background(), "still usable", forkTestRun(t)); err != nil {
		t.Fatal("source admission leaked", err)
	}
	for _, e := range source.Events() {
		if e.Event.Kind() == EventSessionForked {
			t.Fatal("failed fork emitted success")
		}
	}
	entries, err := os.ReadDir(config.WorkspaceRoot)
	if err != nil || len(entries) != 1 {
		t.Fatal("scratch leaked", err)
	}
}

func TestForkSnapshotDoesNotFollowLaterSourceTurnDuringProvisioning(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	config := managerTestConfig(t.TempDir(), &fakeSequenceProvider{replies: []protocol.ModelReply{fakeReply([]protocol.Block{protocol.NewTextBlock("done")}, protocol.StopEndTurn), fakeReply([]protocol.Block{protocol.NewTextBlock("done")}, protocol.StopEndTurn)}})
	config.Services.BashFactory = bashFactoryFunc(func(context.Context, SessionBinding) (BashExecutor, error) {
		if calls.Add(1) == 2 {
			close(entered)
			<-release
		}
		return echoExecutor{}, nil
	})
	m := makeManager(t, config)
	s := createManaged(t, m, CreateSessionRequest{Owner: "alice"})
	if _, err := s.Run(context.Background(), "original"); err != nil {
		t.Fatal(err)
	}
	result := make(chan *ManagedSession, 1)
	fault := make(chan error, 1)
	go func() { child, err := m.Fork(context.Background(), "alice", s.ID()); result <- child; fault <- err }()
	receiveSignal(t, entered)
	if _, err := s.Run(context.Background(), "later"); err != nil {
		t.Fatal(err)
	}
	close(release)
	child := <-result
	if err := <-fault; err != nil {
		t.Fatal(err)
	}
	if child.Info().MessageCount != 2 || child.Info().ForkedFrom.MessageCount != 2 || s.Info().MessageCount != 4 {
		t.Fatal("fork followed later turn", child.Info(), s.Info())
	}
	first, _ := child.Messages()[0].Content.Plain()
	if first != "original" {
		t.Fatal("wrong cut")
	}
	for _, record := range s.Events() {
		if event, ok := record.Event.SessionForked(); ok && event.MessageCount != 2 {
			t.Fatal("source event uses later history count")
		}
	}
}

func TestIdleSteeringDuringForkCopyDoesNotParkWithoutAHolder(t *testing.T) {
	p := &fakeSequenceProvider{replies: []protocol.ModelReply{fakeReply([]protocol.Block{protocol.NewTextBlock("done")}, protocol.StopEndTurn)}}
	m := makeManager(t, managerTestConfig(t.TempDir(), p))
	s := createManaged(t, m, CreateSessionRequest{Owner: "alice"})
	s.core.mu.Lock()
	forked := make(chan error, 1)
	go func() { _, err := m.Fork(context.Background(), "alice", s.ID()); forked <- err }()
	deadline := time.Now().Add(time.Second)
	for len(s.admission) != 0 {
		if time.Now().After(deadline) {
			s.core.mu.Unlock()
			t.Fatal("fork did not acquire source")
		}
		time.Sleep(time.Millisecond)
	}
	wake := make(chan SteeringReceipt, 1)
	fault := make(chan error, 1)
	go func() { receipt, err := s.SubmitSteering("during copy"); wake <- receipt; fault <- err }()
	s.core.mu.Unlock()
	receipt := <-wake
	if err := <-fault; err != nil {
		t.Fatal(err)
	}
	if receipt.Delivered != DeliveryNewTurn {
		t.Fatal("steering parked without active holder", receipt)
	}
	if err := <-forked; err != nil {
		t.Fatal(err)
	}
	for s.Info().Busy {
		if time.Now().After(deadline) {
			t.Fatal("wake did not finish")
		}
		time.Sleep(time.Millisecond)
	}
	if s.Info().PendingSteering != 0 || s.Info().RunCount != 1 {
		t.Fatal("wake lost")
	}
}
