package agent

import (
	"context"
	"errors"

	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/shell"
	"github.com/luoyjx/mini-loop/go/verifiedloop"
)

const (
	EventVerifiedRound      SessionEventKind = "verified_round"
	EventVerifiedReceipt    SessionEventKind = "verified_receipt"
	EventVerifiedCheckpoint SessionEventKind = "verified_checkpoint"
)

func (e SessionEvent) VerifiedRound() (verifiedloop.RoundEvent, bool) {
	return e.verifiedRound, e.kind == EventVerifiedRound
}
func (e SessionEvent) VerifiedReceipt() (verifiedloop.ReceiptEvent, bool) {
	v := e.verifiedReceipt
	v.ExitCode = clonePointer(v.ExitCode)
	return v, e.kind == EventVerifiedReceipt
}
func (e SessionEvent) VerifiedCheckpoint() (verifiedloop.CheckpointEvent, bool) {
	return e.verifiedCheckpoint, e.kind == EventVerifiedCheckpoint
}

// VerifiedRunOptions is supplied by a trusted operator, never by model output.
// Acceptance runs directly on the configured command executor, as in Python;
// worker tool effects retain their ordinary gate, approval and role restrictions.
type VerifiedRunOptions struct {
	AcceptanceCommand string
	MaxRounds         *int64
	CheckInstruments  bool
}

// RunVerifiedWithContext owns one managed turn for the entire verification task.
// It adds no HTTP route or model tool. The caller owns external owner admission.
func (session *ManagedSession) RunVerifiedWithContext(ctx context.Context, request string, options VerifiedRunOptions, run RunContext) (verifiedloop.TaskOutcome, error) {
	if session == nil || session.core == nil {
		return verifiedloop.TaskOutcome{}, errors.New("verified session is not initialized")
	}
	options.MaxRounds = clonePointer(options.MaxRounds)
	var outcome verifiedloop.TaskOutcome
	_, err := session.runOperation(ctx, request, run, false, nil, func(ctx context.Context, request string, run RunContext) (string, error) {
		var err error
		outcome, err = session.core.runVerified(ctx, request, options, run)
		return outcome.Summary, err
	})
	if err != nil {
		return verifiedloop.TaskOutcome{}, err
	}
	return outcome, nil
}

type sessionVerifiedEffects struct {
	session  *Session
	run      RunContext
	executor BashResultExecutor
}

func (effects sessionVerifiedEffects) RunWorker(ctx context.Context, objective string) (string, error) {
	if err := effects.guard(ctx); err != nil {
		return "", err
	}
	return effects.session.runSubagent(ctx, objective, RoleWorker, effects.run)
}
func (effects sessionVerifiedEffects) RunAcceptance(ctx context.Context, command string) (shell.Result, error) {
	if err := effects.guard(ctx); err != nil {
		return shell.Result{}, err
	}
	return effects.executor.ExecuteBashResult(ctx, protocol.BashInput{Command: command})
}
func (effects sessionVerifiedEffects) guard(ctx context.Context) error {
	if err := effects.session.persistence.guardVerifiedLease(ctx); err != nil {
		return err
	}
	return effects.session.persistence.guard(effects.session.messages)
}
func (effects sessionVerifiedEffects) EmitVerifiedEvent(ctx context.Context, event verifiedloop.VerifiedEvent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := effects.guard(ctx); err != nil {
		return err
	}
	v := SessionEvent{kind: SessionEventKind(event.Kind())}
	switch event.Kind() {
	case verifiedloop.EventRound:
		v.verifiedRound, _ = event.Round()
	case verifiedloop.EventReceipt:
		v.verifiedReceipt, _ = event.Receipt()
	case verifiedloop.EventCheckpoint:
		v.verifiedCheckpoint, _ = event.Checkpoint()
	default:
		return errors.New("unsupported verified event")
	}
	effects.session.events.append(v)
	return ctx.Err()
}

func (s *Session) runVerified(ctx context.Context, request string, options VerifiedRunOptions, run RunContext) (verifiedloop.TaskOutcome, error) {
	select {
	case <-ctx.Done():
		return verifiedloop.TaskOutcome{}, ctx.Err()
	case <-s.turn:
	}
	defer func() { s.turn <- struct{}{} }()
	s.mu.Lock()
	defer s.mu.Unlock()
	defer s.publishLive()
	if err := ctx.Err(); err != nil {
		return verifiedloop.TaskOutcome{}, err
	}
	// Never infer a process exit code from rendered prose or a string executor.
	executor, ok := s.bash.(BashResultExecutor)
	if !ok {
		return verifiedloop.TaskOutcome{}, errors.New("verified acceptance requires structured command results")
	}
	if bound, ok := s.bash.(interface{ Workspace() string }); ok && bound.Workspace() != s.executionRoot() {
		return verifiedloop.TaskOutcome{}, errors.New("verified command executor is bound to a different workspace")
	}
	s.currentRun = run.clone()
	s.events.setScope(EventScope{s.label, s.depth, run.clone()})
	defer func() { s.currentRun = RunContext{} }()
	effects := sessionVerifiedEffects{s, run.clone(), executor}
	config := verifiedloop.ServiceConfig{RunID: verifiedloop.RunID(s.id), Worker: effects, Acceptance: effects, Events: effects}
	if options.CheckInstruments {
		config.Probe = verifiedloop.WorkspaceIntegrity{Workspace: s.executionRoot()}
	}
	service, err := verifiedloop.NewService(config)
	if err != nil {
		return verifiedloop.TaskOutcome{}, err
	}
	outcome, err := service.RunTask(ctx, request, verifiedloop.TaskOptions{AcceptanceCommand: options.AcceptanceCommand, MaxRounds: options.MaxRounds})
	if err == nil {
		err = effects.guard(ctx)
	}
	if err != nil {
		return verifiedloop.TaskOutcome{}, err
	}
	return outcome, nil
}
