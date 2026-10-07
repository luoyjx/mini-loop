package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/userresources"
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
	ActiveTrajectoryID       *TrajectoryID       `json:"active_trajectory_id"`
	TrajectoryCount          int                 `json:"trajectory_count"`
	TrajectoryRecordingError *string             `json:"trajectory_recording_error"`
	ID                       SessionID           `json:"id"`
	Status                   SessionStatus       `json:"status"`
	Activity                 SessionActivity     `json:"activity"`
	Busy                     bool                `json:"busy"`
	CancelReason             *string             `json:"cancel_reason"`
	CreatedAt                float64             `json:"created_at"`
	RunCount                 int                 `json:"run_count"`
	PermissionMode           PermissionMode      `json:"permission_mode"`
	PendingSteering          int                 `json:"pending_steering"`
	ForkedFrom               *ForkLineage        `json:"forked_from"`
	Workspace                string              `json:"workspace"`
	WorkspaceBound           bool                `json:"workspace_bound"`
	Model                    string              `json:"model"`
	MessageCount             int                 `json:"message_count"`
	Todos                    []protocol.TodoItem `json:"todos"`
	Subscribers              int                 `json:"subscribers"`
	SinkError                *string             `json:"sink_error"`
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
	skillOperation *personalSkillOperation
	skillCapture   userresources.CaptureLedger
	build          string
	core           *Session
	mu             sync.Mutex
	admission      chan struct{}
	accepting      bool
	closedReason   string
	status         SessionStatus
	active         *activeTurn
	cancelReason   *string
	createdAt      float64
	runCount       int
	approvals      *ApprovalBroker
	workspaceBound bool
}

func NewManagedSession(config RuntimeConfig) (*ManagedSession, error) {
	return newManagedSession(config, false)
}

// Manager construction defers the first write until workspace and fork metadata
// are installed. The caller owns the backend and its close lifecycle.
func newManagedSession(config RuntimeConfig, deferState bool) (*ManagedSession, error) {
	store, owner, ttl := config.StateStore, config.StateLeaseOwner, config.StateLeaseTTL
	if ttl < 0 || (store == nil && (owner != "" || ttl != 0)) {
		return nil, errors.New("state lease configuration requires a store and non-negative TTL")
	}
	if ttl == 0 {
		ttl = DefaultStateLeaseTTL
	}
	config.StateStore, config.StateLeaseOwner, config.StateLeaseTTL = nil, "", 0
	core, err := NewRuntimeSession(config)
	if err != nil {
		return nil, err
	}
	core.control = &sessionControl{mode: core.mode}
	core.gate.modeSource = core.control
	session := &ManagedSession{core: core, admission: make(chan struct{}, 1), accepting: true, status: StatusIdle, createdAt: float64(time.Now().UnixMicro()) / 1e6, approvals: config.Approvals, workspaceBound: config.Workspace != ""}
	session.admission <- struct{}{}
	run := &trajectoryRun{store: config.Trajectories, masker: config.Secrets}
	if config.Trajectories != nil {
		run.fail(trajectoryFault(func() error { var err error; run.count, err = config.Trajectories.Count(config.ID); return err }))
	}
	session.core.events.trajectory = run
	session.build = config.Build
	if store != nil {
		p := &sessionPersistence{store: store, session: session, owner: owner, ttl: ttl, epoch: 1}
		core.persistence, core.events.persistence = p, p
		if !deferState {
			if err := p.initialize(); err != nil {
				return nil, errors.Join(err, p.release())
			}
		}
	}
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
	if operation := session.skillOperation; operation != nil {
		operation.cancel(context.Canceled)
	}
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

var ErrSessionBusy = errors.New("session is running a turn")

func (session *ManagedSession) RunWithContext(ctx context.Context, prompt string, run RunContext) (string, error) {
	return session.runWithContext(ctx, prompt, run, false, nil)
}

// TryRunWithContext atomically refuses occupied admission instead of queuing.
func (session *ManagedSession) TryRunWithContext(ctx context.Context, prompt string, run RunContext) (string, error) {
	return session.runWithContext(ctx, prompt, run, true, nil)
}

// ManagedTurnResult captures completion before another holder takes admission.
type ManagedTurnResult struct {
	Final string
	Info  SessionInfo
}

func (session *ManagedSession) TryRunWithSnapshot(ctx context.Context, prompt string, run RunContext) (ManagedTurnResult, error) {
	var result ManagedTurnResult
	var err error
	result.Final, err = session.runWithContext(ctx, prompt, run, true, &result.Info)
	return result, err
}

func (session *ManagedSession) runWithContext(ctx context.Context, prompt string, run RunContext, try bool, snapshot *SessionInfo) (output string, err error) {
	if err = run.Validate(); err != nil {
		return "", err
	}
	session.mu.Lock()
	err = session.admissionError()
	session.mu.Unlock()
	if err != nil {
		return "", err
	}
	if try {
		select {
		case <-session.admission:
		default:
			return "", ErrSessionBusy
		}
	} else {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-session.admission:
		}
	}
	defer func() { session.admission <- struct{}{} }()
	if err = ctx.Err(); err != nil {
		return "", err
	}
	// Claim after owning admission: a queued caller cannot carry a stale claim
	// into a later turn. No manager/session mutex is held during backend calls.
	if err = session.core.persistence.requireLease(ctx); err != nil {
		return "", err
	}
	session.mu.Lock()
	if err = session.admissionError(); err != nil {
		session.mu.Unlock()
		return "", err
	}
	turnCtx, active := session.beginTurnLocked(ctx)
	session.mu.Unlock()
	output, err = session.runActive(turnCtx, prompt, run, active)
	if snapshot != nil && err == nil {
		*snapshot = session.Info()
	}
	return output, err
}

// Caller holds mu and owns admission; publication precedes an async response.
func (session *ManagedSession) beginTurnLocked(ctx context.Context) (context.Context, *activeTurn) {
	turnCtx, cancel := context.WithCancel(ctx)
	active := &activeTurn{cancel: cancel, done: make(chan struct{})}
	session.active = active
	session.status = StatusRunning
	session.runCount++
	return turnCtx, active
}
func (session *ManagedSession) runActive(turnCtx context.Context, prompt string, run RunContext, active *activeTurn) (output string, err error) {
	turnCtx, cancelCause := context.WithCancelCause(turnCtx)
	unbind := session.core.persistence.bindTurn(cancelCause)
	defer func() { unbind(); cancelCause(nil) }()
	defer active.cancel()
	defer func() {
		if fault := recover(); fault != nil {
			err = fmt.Errorf("runtime panicked: %T", fault)
		}
		if errors.Is(context.Cause(turnCtx), ErrSessionLeaseLost) {
			output, err = "", ErrSessionLeaseLost
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
				session.finishTrajectory(run, SessionEvent{kind: EventStatus, status: StatusEvent{StatusIdle, true}}, TrajectoryCancelled, nil, nil)
			} else {
				detail := boundedError(err)
				session.finishTrajectory(run, SessionEvent{kind: EventError, runError: RunErrorEvent{kind: ErrorRuntime, detail: detail}}, TrajectoryError, nil, &detail)
			}
		} else {
			session.finishTrajectory(run, SessionEvent{kind: EventDone, done: DoneEvent{output, PhaseFinalAnswer}}, TrajectoryCompleted, &output, nil)
		}
		// The terminal flush can itself detect a lost lease.
		if errors.Is(context.Cause(turnCtx), ErrSessionLeaseLost) {
			output, err = "", ErrSessionLeaseLost
			session.mu.Lock()
			session.status = StatusError
			session.mu.Unlock()
		}
		if err == nil && run.Allows(CapabilityPersonalSkillCaptureSource) {
			session.skillCapture.Record(prompt, output, session.core.Messages(), session.core.secrets)
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
	session.beginTrajectory(prompt)
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
	status, busy, count, createdAt, reason := session.status, session.active != nil, session.runCount, session.createdAt, clonePointer(session.cancelReason)
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
	mode, queued := session.core.control.snapshot()
	trajectoryID, trajectoryCount, trajectoryError := session.core.events.trajectory.snapshot()
	return SessionInfo{trajectoryID, trajectoryCount, trajectoryError, session.ID(), status, activity, busy, reason, createdAt, count, mode, queued, clonePointer(session.core.forkedFrom), session.core.workspace, session.workspaceBound, session.core.model, messageCount, todos, session.core.SubscriberCount(), sink}
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
	partial := strings.TrimSpace(s.streamedText)
	s.streamedText = ""
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
		note := "[Turn interrupted: " + reason + "]"
		if live := s.backgroundLive(); live > 0 {
			note += fmt.Sprintf("\n[%d background task(s) kept running through this interruption and may have already changed files; check_background shows their state.]", live)
		}
		if partial != "" {
			note = partial + "\n" + note
		}
		s.appendMessages(protocol.Message{Role: protocol.RoleAssistant, Content: protocol.BlockContent(protocol.NewTextBlock(note))})
	}
	return repaired
}
