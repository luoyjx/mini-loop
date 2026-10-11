package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/protocol"
)

type approvalModel struct {
	question bool
	observed string
}

func (provider *approvalModel) Complete(_ context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
	last := request.Messages[len(request.Messages)-1]
	if _, ok := last.Content.Plain(); ok {
		if provider.question {
			return fakeReply([]protocol.Block{protocol.NewToolUse("q", protocol.AskUserToolInput(protocol.AskUserInput{Question: "which?"}))}, protocol.StopToolUse), nil
		}
		return fakeReply([]protocol.Block{protocol.NewBashUse("u", "git reset --hard HEAD")}, protocol.StopToolUse), nil
	}
	blocks, _ := last.Content.Blocks()
	for _, block := range blocks {
		if result, ok := block.ToolResult(); ok {
			provider.observed = result.Content
		}
	}
	return fakeReply([]protocol.Block{protocol.NewTextBlock("done")}, protocol.StopEndTurn), nil
}
func waitBrokerPending(t *testing.T, broker *ApprovalBroker, session SessionID) ApprovalSnapshot {
	t.Helper()
	timeout := time.NewTimer(2 * time.Second)
	defer timeout.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		if values := broker.List(session); len(values) > 0 {
			return values[0]
		}
		select {
		case <-tick.C:
		case <-timeout.C:
			t.Fatal("no parked request")
		}
	}
}
func TestRuntimeApprovalParksBeforeActionBeginAndThenUsesSessionGrant(t *testing.T) {
	broker, _ := NewApprovalBroker(ApprovalBrokerConfig{})
	journal, _ := NewInMemoryActionJournal(4)
	provider, executor := &approvalModel{}, &subagentBashSpy{}
	config := runtimeConfig(t.TempDir(), provider)
	config.Bash, config.Approvals, config.ActionJournal = executor, broker, journal
	session, err := NewRuntimeSession(config)
	if err != nil {
		t.Fatal(err)
	}
	run := actionContext(t)
	call := ToolCall{"u", protocol.BashToolInput(protocol.BashInput{Command: "git reset --hard HEAD"})}
	id, _ := ToolActionID(config.ID, run, call)
	done := make(chan error, 1)
	go func() { _, err := session.RunWithContext(context.Background(), "perform", run); done <- err }()
	pending := waitBrokerPending(t, broker, config.ID)
	if executor.calls != 0 {
		t.Fatal("parked call executed")
	}
	if _, exists, _ := journal.Get(context.Background(), id); exists {
		t.Fatal("parked call marked dispatched")
	}
	if !broker.Resolve(pending.ApprovalID, ApprovalResolution{SessionID: config.ID, Allowed: true, Remember: true}) {
		t.Fatal("resolve failed")
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if executor.calls != 1 {
		t.Fatal(executor.calls)
	}
	if _, err := session.RunWithContext(context.Background(), "perform", run); err != nil {
		t.Fatal(err)
	}
	if executor.calls != 1 {
		t.Fatal("terminal replay executed")
	}
	required, used, recorded := 0, 0, 0
	for _, record := range session.Events() {
		if event, ok := record.Event.Approval(); ok {
			if record.Scope.RunContext.MessageID() != run.MessageID() || record.Scope.Label != string(config.ID) {
				t.Fatal("approval lost run scope")
			}
			switch event.Kind() {
			case ApprovalRequiredEvent:
				required++
			case ApprovalGrantUsedEvent:
				used++
			case ApprovalGrantRecordedEvent:
				recorded++
			}
		}
	}
	if required != 1 || used != 1 || recorded != 1 {
		t.Fatal(required, used, recorded)
	}
}
func TestRuntimeQuestionCarriesToolJoinIDAndEmptyText(t *testing.T) {
	store := &approvalStoreSpy{}
	broker, _ := NewApprovalBroker(ApprovalBrokerConfig{Store: store})
	provider := &approvalModel{question: true}
	config := runtimeConfig(t.TempDir(), provider)
	config.Approvals = broker
	session, err := NewRuntimeSession(config)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := session.Run(context.Background(), "ask"); done <- err }()
	pending := waitBrokerPending(t, broker, config.ID)
	if pending.ToolUseID != "q" || pending.Kind != ApprovalQuestion {
		t.Fatal(pending)
	}
	empty := ""
	broker.Resolve(pending.ApprovalID, ApprovalResolution{SessionID: config.ID, Allowed: true, Answer: &empty})
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if provider.observed != "The user answered: " {
		t.Fatal(provider.observed)
	}
	rows := store.snapshot()
	if len(rows) != 2 || rows[1].Status != ApprovalAnswered || rows[1].Answer == nil || *rows[1].Answer != "" {
		t.Fatal(rows)
	}
}
func TestContextCancellationRemovesPendingButKeepsSourcePendingRow(t *testing.T) {
	store, sink := &approvalStoreSpy{}, newApprovalSinkSpy()
	broker, _ := NewApprovalBroker(ApprovalBrokerConfig{Store: store})
	binding := gateTestAuthority(ModeInteractive)
	surface, _ := broker.ForSession(binding, sink)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := surface.Approve(ctx, ApprovalRequest{binding, gateTestCall("git reset --hard HEAD"), "r", "ask"})
		done <- err
	}()
	pending := <-sink.required
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if len(broker.List("s")) != 0 || broker.Resolve(pending.ApprovalID, ApprovalResolution{SessionID: "s", Allowed: true}) {
		t.Fatal("cancelled waiter resolvable")
	}
	rows := store.snapshot()
	if len(rows) != 1 || rows[0].Status != ApprovalPending {
		t.Fatal("context cancel rewrote the source pending row", rows)
	}
}
func TestBrokerSurfaceCannotBeBorrowedByChildOrForeignOwner(t *testing.T) {
	broker, _ := NewApprovalBroker(ApprovalBrokerConfig{})
	binding := gateTestAuthority(ModeInteractive)
	surface, _ := broker.ForSession(binding, nil)
	for _, foreign := range []ToolAuthority{{SessionID: "child", OwnerID: binding.OwnerID, Mode: ModeInteractive}, {SessionID: binding.SessionID, OwnerID: "foreign", Mode: ModeInteractive}} {
		allowed, err := surface.Approve(context.Background(), ApprovalRequest{foreign, gateTestCall("git reset --hard HEAD"), "r", "ask"})
		if allowed || err != nil {
			t.Fatal(allowed, err)
		}
		answer, err := surface.AskQuestion(context.Background(), QuestionRequest{foreign, "which?"})
		if answer.Kind() != QuestionUnanswered || err != nil {
			t.Fatal(answer, err)
		}
	}
}
func TestSharedBrokerKeepsConcurrentSessionsAndCancellationSeparate(t *testing.T) {
	broker, _ := NewApprovalBroker(ApprovalBrokerConfig{})
	var group sync.WaitGroup
	for i := 0; i < 8; i++ {
		id := SessionID(fmt.Sprintf("s%d", i))
		binding := ToolAuthority{SessionID: id, OwnerID: "owner", Mode: ModeInteractive}
		sink := newApprovalSinkSpy()
		surface, _ := broker.ForSession(binding, sink)
		group.Add(1)
		go func() {
			defer group.Done()
			done := make(chan bool, 1)
			go func() {
				allowed, err := surface.Approve(context.Background(), ApprovalRequest{binding, gateTestCall("git reset --hard HEAD"), "r", "ask"})
				if err != nil {
					t.Error(err)
				}
				done <- allowed
			}()
			pending := <-sink.required
			if broker.Resolve(pending.ApprovalID, ApprovalResolution{SessionID: "foreign", Allowed: true}) {
				t.Error("foreign resolution")
			}
			broker.CancelSession(id)
			if <-done {
				t.Error("cancel allowed")
			}
			if len(broker.List(id)) != 0 {
				t.Error("pending leaked")
			}
		}()
	}
	group.Wait()
}
func TestGrantsPrecedeReviewerAndDieWithSession(t *testing.T) {
	var reviews atomic.Int64
	broker, _ := NewApprovalBroker(ApprovalBrokerConfig{Reviewer: approvalReviewerFunc(func(context.Context, ApprovalRequest) (ReviewVerdict, error) {
		reviews.Add(1)
		return ReviewAbstain, nil
	})})
	sink := newApprovalSinkSpy()
	binding := gateTestAuthority(ModeInteractive)
	surface, _ := broker.ForSession(binding, sink)
	request := ApprovalRequest{binding, gateTestCall("git reset --hard HEAD"), "r", "ask"}
	done := make(chan bool, 1)
	go func() { value, _ := surface.Approve(context.Background(), request); done <- value }()
	pending := <-sink.required
	broker.Resolve(pending.ApprovalID, ApprovalResolution{SessionID: "s", Allowed: true, Remember: true})
	if !<-done {
		t.Fatal("first approval")
	}
	if allowed, err := surface.Approve(context.Background(), request); !allowed || err != nil || reviews.Load() != 1 {
		t.Fatal("grant lost precedence", allowed, err, reviews.Load())
	}
	broker.CancelSession("s")
	if _, granted := broker.Granted("s", request.Call.Input); granted {
		t.Fatal("grant survived deletion")
	}
}
func TestReviewerCannotWidenReadonlyOrImmutableDenial(t *testing.T) {
	var reviews atomic.Int64
	broker, _ := NewApprovalBroker(ApprovalBrokerConfig{Reviewer: approvalReviewerFunc(func(context.Context, ApprovalRequest) (ReviewVerdict, error) { reviews.Add(1); return ReviewAllow, nil })})
	binding := gateTestAuthority(ModeReadonly)
	surface, _ := broker.ForSession(binding, nil)
	handler := &gateHandler{}
	gate, _ := NewToolGate(gateTestCatalog(t, handler), DefaultPermissionPolicy(surface), GateHooks{})
	for _, call := range []ToolCall{gateTestCall("echo mutate"), gateTestCall("rm -rf /")} {
		outcome, err := gate.Dispatch(context.Background(), binding, call)
		if !outcome.Denied || err != nil {
			t.Fatal(outcome, err)
		}
	}
	if reviews.Load() != 0 || handler.calls != 0 || len(broker.List("s")) != 0 {
		t.Fatal("reviewer widened denial")
	}
}
func TestBrokerDefaultsAndConflictingRuntimeSeams(t *testing.T) {
	broker, err := NewApprovalBroker(ApprovalBrokerConfig{})
	if err != nil || broker.timeout != DefaultApprovalTimeout {
		t.Fatal(broker, err)
	}
	if _, err := NewApprovalBroker(ApprovalBrokerConfig{Timeout: -time.Second}); err == nil {
		t.Fatal("negative timeout")
	}
	config := runtimeConfig(t.TempDir(), &FakeProvider{})
	config.Approvals, config.Approver = broker, approverFunc(func(context.Context, ApprovalRequest) (bool, error) { return true, nil })
	if _, err := NewRuntimeSession(config); err == nil {
		t.Fatal("ambiguous broker/approver")
	}
}
func TestInvalidAndPanickingReviewerFallThroughWithoutImplicitAllow(t *testing.T) {
	for _, panics := range []bool{false, true} {
		broker, _ := NewApprovalBroker(ApprovalBrokerConfig{Timeout: time.Millisecond, Reviewer: approvalReviewerFunc(func(context.Context, ApprovalRequest) (ReviewVerdict, error) {
			if panics {
				panic("sensitive detail")
			}
			return "yes", nil
		})})
		binding := gateTestAuthority(ModeInteractive)
		surface, _ := broker.ForSession(binding, nil)
		allowed, err := surface.Approve(context.Background(), ApprovalRequest{binding, gateTestCall("echo x"), "r", "ask"})
		if allowed || err != nil || len(broker.Problems()) != 1 || strings.Contains(broker.Problems()[0], "sensitive detail") {
			t.Fatal(allowed, err, broker.Problems())
		}
	}
}

type childApprovalModel struct{ denied, unavailable bool }

func (provider *childApprovalModel) Complete(_ context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
	child := request.System != nil && strings.Contains(*request.System, " subagent in ")
	last := request.Messages[len(request.Messages)-1]
	if _, plain := last.Content.Plain(); plain {
		if child {
			return fakeReply([]protocol.Block{
				protocol.NewBashUse("shell", "git reset --hard HEAD"),
				protocol.NewToolUse("question", protocol.AskUserToolInput(protocol.AskUserInput{Question: "child question"})),
			}, protocol.StopToolUse), nil
		}
		role := protocol.AgentGeneralPurpose
		return fakeReply([]protocol.Block{protocol.NewToolUse("task", protocol.TaskToolInput(protocol.TaskInput{Prompt: "work", AgentType: &role}))}, protocol.StopToolUse), nil
	}
	if child {
		blocks, _ := last.Content.Blocks()
		for _, block := range blocks {
			result, _ := block.ToolResult()
			if result.ToolUseID == "shell" {
				provider.denied = strings.Contains(result.Content, "Permission denied:")
			}
			if result.ToolUseID == "question" {
				provider.unavailable = result.Content == "[ask_user unavailable on this surface: no approval broker]"
			}
		}
	}
	return fakeReply([]protocol.Block{protocol.NewTextBlock("done")}, protocol.StopEndTurn), nil
}

func TestFreshChildDoesNotInheritParentBrokerReviewerOrQuestions(t *testing.T) {
	store := &approvalStoreSpy{}
	var reviews atomic.Int64
	broker, _ := NewApprovalBroker(ApprovalBrokerConfig{Store: store, Reviewer: approvalReviewerFunc(func(context.Context, ApprovalRequest) (ReviewVerdict, error) {
		reviews.Add(1)
		return ReviewAllow, nil
	})})
	provider, executor := &childApprovalModel{}, &subagentBashSpy{}
	config := runtimeConfig(t.TempDir(), provider)
	config.Approvals, config.Bash, config.RoleToolPolicy = broker, executor, allRoleTools{}
	session, err := NewRuntimeSession(config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Run(context.Background(), "delegate"); err != nil {
		t.Fatal(err)
	}
	if !provider.denied || !provider.unavailable || executor.calls != 0 || reviews.Load() != 0 || len(store.snapshot()) != 0 {
		t.Fatal("child inherited parent broker authority", provider.denied, provider.unavailable, executor.calls, reviews.Load(), store.snapshot())
	}
}

func TestCancelAllSettlesPermissionAndQuestionAsCancelled(t *testing.T) {
	store := &approvalStoreSpy{}
	broker, _ := NewApprovalBroker(ApprovalBrokerConfig{Store: store})
	done := make(chan error, 2)
	for i, id := range []SessionID{"permission", "question"} {
		binding := ToolAuthority{SessionID: id, OwnerID: "owner", Mode: ModeInteractive}
		sink := newApprovalSinkSpy()
		surface, _ := broker.ForSession(binding, sink)
		go func(question bool) {
			if question {
				answer, err := surface.AskQuestion(context.Background(), QuestionRequest{binding, "q"})
				if _, answered := answer.Text(); answered {
					done <- errors.New("cancelled question answered")
					return
				}
				done <- err
			} else {
				allowed, err := surface.Approve(context.Background(), ApprovalRequest{binding, gateTestCall("git reset --hard HEAD"), "r", "ask"})
				if allowed {
					done <- errors.New("cancelled permission allowed")
					return
				}
				done <- err
			}
		}(i == 1)
		<-sink.required
	}
	if broker.CancelAll() != 2 {
		t.Fatal("did not cancel both")
	}
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	rows := store.snapshot()
	if len(rows) != 4 || rows[2].Status != ApprovalCancelled || rows[3].Status != ApprovalCancelled {
		t.Fatal("cancellation collapsed to denial", rows)
	}
	if len(broker.List("permission")) != 0 || len(broker.List("question")) != 0 || broker.CancelAll() != 0 {
		t.Fatal("pending cancellation leaked")
	}
}
