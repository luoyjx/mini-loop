package agent

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/luoyjx/mini-loop/go/protocol"
)

type SessionStatus string
type SessionActivity string

const (
	StatusIdle               SessionStatus    = "idle"
	StatusRunning            SessionStatus    = "running"
	StatusError              SessionStatus    = "error"
	ActivityIdle             SessionActivity  = "idle"
	ActivityRunning          SessionActivity  = "running"
	ActivityError            SessionActivity  = "error"
	ActivityAwaitingApproval SessionActivity  = "awaiting_approval"
	ActivityStuck            SessionActivity  = "stuck"
	EventStatus              SessionEventKind = "status"
	EventDone                SessionEventKind = "done"
	EventCancelled           SessionEventKind = "cancelled"
)

type StatusEvent struct {
	Status    SessionStatus
	Cancelled bool
}
type DoneEvent struct {
	Text  string
	Phase TextPhase
}
type CancelledEvent struct {
	Reason           string
	RepairedToolUses []string
}

func (event SessionEvent) Status() (StatusEvent, bool) {
	return event.status, event.kind == EventStatus
}
func (event SessionEvent) Done() (DoneEvent, bool) { return event.done, event.kind == EventDone }
func (event SessionEvent) Cancelled() (CancelledEvent, bool) {
	v := event.cancelled
	v.RepairedToolUses = append([]string{}, v.RepairedToolUses...)
	return v, event.kind == EventCancelled
}

type SessionInfo struct {
	ID             SessionID           `json:"id"`
	Status         SessionStatus       `json:"status"`
	Activity       SessionActivity     `json:"activity"`
	Busy           bool                `json:"busy"`
	CancelReason   *string             `json:"cancel_reason"`
	CreatedAt      float64             `json:"created_at"`
	RunCount       int                 `json:"run_count"`
	PermissionMode PermissionMode      `json:"permission_mode"`
	Workspace      string              `json:"workspace"`
	WorkspaceBound bool                `json:"workspace_bound"`
	Model          string              `json:"model"`
	MessageCount   int                 `json:"message_count"`
	Todos          []protocol.TodoItem `json:"todos"`
	Subscribers    int                 `json:"subscribers"`
	SinkError      *string             `json:"sink_error"`
}
type liveRuntime struct {
	MessageCount int
	Stuck        StuckState
	Messages     []protocol.Message
}

func (s *Session) publishLive() {
	s.live.Store(&liveRuntime{len(s.messages), s.stuckState(), append([]protocol.Message(nil), s.messages...)})
}
func (s *Session) appendMessages(messages ...protocol.Message) {
	s.messages = append(s.messages, messages...)
	s.publishLive()
}
func (s *Session) bindEventHistory() {
	if s.events.parent != nil {
		s.events.parent.mu.Lock()
		s.events.sessionID, s.events.epoch, s.events.history = s.events.parent.sessionID, s.events.parent.epoch, s.events.parent.history
		s.events.parent.mu.Unlock()
		return
	}
	s.events.sessionID, s.events.epoch = s.id, 1
	s.events.history = func() []protocol.Message {
		view := s.live.Load()
		if view == nil {
			return nil
		}
		return view.Messages
	}
}

type activeTurn struct {
	cancel         context.CancelFunc
	done           chan struct{}
	operatorReason *string
	finishing      bool
}

// ManagedSession owns admission, active-turn cancellation and terminal events.
// Its underlying core is private: children use Session directly and do not
// fabricate outer session status/done events.
type ManagedSession struct {
	core         *Session
	mu           sync.Mutex
	admission    chan struct{}
	accepting    bool
	closedReason string
	status       SessionStatus
	active       *activeTurn
	cancelReason *string
	createdAt    float64
	runCount     int
	approvals    *ApprovalBroker
}

func NewManagedSession(config RuntimeConfig) (*ManagedSession, error) {
	core, err := NewRuntimeSession(config)
	if err != nil {
		return nil, err
	}
	session := &ManagedSession{core: core, admission: make(chan struct{}, 1), accepting: true, status: StatusIdle, createdAt: float64(time.Now().UnixMicro()) / 1e6, approvals: config.Approvals}
	session.admission <- struct{}{}
	return session, nil
}
func (session *ManagedSession) ID() SessionID                { return session.core.ID() }
func (session *ManagedSession) Owner() OwnerID               { return session.core.Owner() }
func (session *ManagedSession) Messages() []protocol.Message { return session.core.Messages() }
func (session *ManagedSession) Events() []SessionEventRecord { return session.core.Events() }
func (session *ManagedSession) EventsAfter(cursor EventSequence) []SessionEventRecord {
	return session.core.EventsAfter(cursor)
}
func (session *ManagedSession) Subscribe(replay bool) *EventSubscription {
	return session.core.Subscribe(replay)
}
func (session *ManagedSession) StopAccepting(reason string) {
	session.mu.Lock()
	defer session.mu.Unlock()
	session.accepting = false
	session.closedReason = reason
}
func (session *ManagedSession) admissionError() error {
	if session.accepting {
		return nil
	}
	if session.closedReason != "" {
		return errors.New(session.closedReason)
	}
	return errors.New("session is not accepting turns")
}
func (session *ManagedSession) Run(ctx context.Context, prompt string) (string, error) {
	run, err := DefaultRunContext()
	if err != nil {
		return "", err
	}
	return session.RunWithContext(ctx, prompt, run)
}
func (session *ManagedSession) emitFor(run RunContext, event SessionEvent) {
	// Python's outer session events carry session framing, while the inner
	// Agent events own label/depth and message provenance.
	scope := EventScope{}
	event, scope = maskedEvent(session.core.secrets, event), maskedScope(session.core.secrets, scope)
	session.core.events.appendScoped(event, scope)
}
func (session *ManagedSession) RunWithContext(ctx context.Context, prompt string, run RunContext) (output string, err error) {
	if err = run.Validate(); err != nil {
		return "", err
	}
	session.mu.Lock()
	err = session.admissionError()
	session.mu.Unlock()
	if err != nil {
		return "", err
	}
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-session.admission:
	}
	defer func() { session.admission <- struct{}{} }()
	if err = ctx.Err(); err != nil {
		return "", err
	}
	session.mu.Lock()
	if err = session.admissionError(); err != nil {
		session.mu.Unlock()
		return "", err
	}
	turnCtx, cancel := context.WithCancel(ctx)
	active := &activeTurn{cancel: cancel, done: make(chan struct{})}
	session.active = active
	session.status = StatusRunning
	session.runCount++
	session.mu.Unlock()
	defer cancel()
	defer func() {
		if fault := recover(); fault != nil {
			err = fmt.Errorf("runtime panicked: %T", fault)
		}
		session.mu.Lock()
		active.finishing = true
		reason := clonePointer(active.operatorReason)
		if reason != nil && err == nil {
			// Cancellation acknowledged before terminal commitment must not
			// publish a successful done event, even if the core just returned.
			output, err = "", context.Canceled
		}
		if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			session.status = StatusError
		} else {
			session.status = StatusIdle
		}
		status := session.status
		session.mu.Unlock()
		if err != nil {
			if status == StatusIdle {
				session.emitFor(run, SessionEvent{kind: EventStatus, status: StatusEvent{StatusIdle, true}})
			} else {
				session.emitFor(run, SessionEvent{kind: EventError, runError: RunErrorEvent{kind: ErrorRuntime, detail: boundedError(err)}})
			}
		} else {
			session.emitFor(run, SessionEvent{kind: EventDone, done: DoneEvent{output, PhaseFinalAnswer}})
		}
		if reason != nil {
			repaired := session.core.recordInterruption(*reason)
			session.emitFor(run, SessionEvent{kind: EventCancelled, cancelled: CancelledEvent{*reason, repaired}})
		}
		session.mu.Lock()
		session.active = nil
		close(active.done)
		session.mu.Unlock()
	}()
	session.emitFor(run, SessionEvent{kind: EventStatus, status: StatusEvent{StatusRunning, false}})
	return session.core.RunWithContext(turnCtx, prompt, run)
}
func (session *ManagedSession) Cancel(ctx context.Context, reason string) (bool, error) {
	session.mu.Lock()
	active := session.active
	if active == nil || active.finishing {
		session.mu.Unlock()
		return false, nil
	}
	active.operatorReason = &reason
	session.cancelReason = &reason
	active.cancel()
	done := active.done
	session.mu.Unlock()
	if session.approvals != nil {
		session.approvals.CancelSession(session.ID())
	}
	select {
	case <-done:
		return true, nil
	case <-ctx.Done():
		return true, ctx.Err()
	}
}
func (session *ManagedSession) Info() SessionInfo {
	session.mu.Lock()
	status, busy, count, reason := session.status, session.active != nil, session.runCount, clonePointer(session.cancelReason)
	session.mu.Unlock()
	activity := SessionActivity(status)
	view := session.core.live.Load()
	messageCount := 0
	if view != nil {
		messageCount = view.MessageCount
	}
	if status == StatusRunning {
		if session.approvals != nil && len(session.approvals.List(session.ID())) > 0 {
			activity = ActivityAwaitingApproval
		} else if view != nil && hasStuckSignal(session.core.stuckDetector, view.Stuck) {
			activity = ActivityStuck
		}
	}
	if reason != nil {
		*reason = maskedText(session.core.secrets, *reason)
	}
	todos := session.core.Todos()
	for i := range todos {
		todos[i].Content = maskedText(session.core.secrets, todos[i].Content)
		todos[i].ActiveForm = maskedText(session.core.secrets, todos[i].ActiveForm)
	}
	var sink *string
	if value := session.core.SinkError(); value != "" {
		sink = &value
	}
	return SessionInfo{session.ID(), status, activity, busy, reason, session.createdAt, count, session.core.mode, session.core.workspace, session.core.workspace != "", session.core.model, messageCount, todos, session.core.SubscriberCount(), sink}
}
func hasStuckSignal(detector StuckDetector, state StuckState) (stuck bool) {
	defer func() {
		if recover() != nil {
			stuck = false
		}
	}()
	state.Steps = append([]ToolStep(nil), state.Steps...)
	signal, err := detector.Inspect(state)
	return err == nil && signal != nil
}
func (s *Session) recordInterruption(reason string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	repaired := append([]string{}, s.repairedToolUses...)
	if len(s.messages) > 0 {
		last := s.messages[len(s.messages)-1]
		if last.Role == protocol.RoleAssistant {
			blocks, _ := last.Content.Blocks()
			results := []protocol.Block{}
			for _, block := range blocks {
				if use, ok := block.ToolUse(); ok {
					repaired = append(repaired, use.ID)
					results = append(results, protocol.NewToolResult(use.ID, unknownToolResult, false))
				}
			}
			if len(results) > 0 {
				s.appendMessages(protocol.Message{Role: protocol.RoleUser, Content: protocol.BlockContent(results...)})
			}
		}
	}
	if len(repaired) == 0 {
		s.appendMessages(protocol.Message{Role: protocol.RoleAssistant, Content: protocol.BlockContent(protocol.NewTextBlock("[Turn interrupted: " + reason + "]"))})
	}
	return repaired
}
