package verifiedloop

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"unicode/utf8"

	"github.com/luoyjx/mini-loop/go/improvement"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/shell"
)

const AcceptanceRequirement RequirementID = "acceptance"
const DefaultMaxRounds int64 = 3
const integrityFeedback = "[integrity] the acceptance instruments changed after the task began; a passing exit code from a changed auditor does not verify. Restore the instruments, or make changing them the explicit objective.\n"

type TaskStatus string

const (
	TaskComplete   TaskStatus = "complete"
	TaskUnverified TaskStatus = "unverified"
)

// Worker runs an actual executor. A summary carries no completion authority.
// Session binding must retain its ordinary owner/capability/approval pipeline.
type Worker interface {
	RunWorker(context.Context, string) (string, error)
}
type AcceptanceRunner interface {
	RunAcceptance(context.Context, string) (shell.Result, error)
}

// Nil fingerprint disables subsequent comparisons, matching source None baseline.
type IntegrityProbe interface {
	ObserveIntegrity(context.Context) (*improvement.InstrumentFingerprint, error)
}
type VerifiedEventSink interface {
	EmitVerifiedEvent(context.Context, VerifiedEvent) error
}
type ServiceConfig struct {
	RunID      RunID
	Worker     Worker
	Acceptance AcceptanceRunner
	Probe      IntegrityProbe
	Events     VerifiedEventSink
}
type Service struct{ config ServiceConfig }

func NewService(config ServiceConfig) (*Service, error) {
	if config.Worker == nil || config.Acceptance == nil {
		return nil, errors.New("verified loop requires worker and acceptance runner")
	}
	return &Service{config}, nil
}

type TaskOptions struct {
	AcceptanceCommand string
	MaxRounds         *int64
}
type TaskOutcome struct {
	Status     TaskStatus
	Rounds     int64
	Checkpoint Checkpoint
	Receipts   []Receipt
	Summary    string
	Integrity  Integrity
}

type VerifiedEventKind string

const (
	EventRound      VerifiedEventKind = "verified_round"
	EventReceipt    VerifiedEventKind = "verified_receipt"
	EventCheckpoint VerifiedEventKind = "verified_checkpoint"
)

type RoundEvent struct {
	Round     int64  `json:"round"`
	Objective string `json:"objective"`
}
type ReceiptEvent struct {
	Round     int64     `json:"round"`
	Verdict   Verdict   `json:"verdict"`
	Integrity Integrity `json:"integrity"`
	ExitCode  *int      `json:"exit_code"`
}
type CheckpointEvent struct {
	StateRevision Revision   `json:"state_revision"`
	Status        TaskStatus `json:"status"`
}

// VerifiedEvent is a closed typed telemetry union, not a string-keyed payload.
type VerifiedEvent struct {
	kind       VerifiedEventKind
	round      RoundEvent
	receipt    ReceiptEvent
	checkpoint CheckpointEvent
}

func (e VerifiedEvent) Kind() VerifiedEventKind   { return e.kind }
func (e VerifiedEvent) Round() (RoundEvent, bool) { return e.round, e.kind == EventRound }
func (e VerifiedEvent) Receipt() (ReceiptEvent, bool) {
	out := e.receipt
	out.ExitCode = copyExit(out.ExitCode)
	return out, e.kind == EventReceipt
}
func (e VerifiedEvent) Checkpoint() (CheckpointEvent, bool) {
	return e.checkpoint, e.kind == EventCheckpoint
}
func (e VerifiedEvent) MarshalJSON() ([]byte, error) {
	switch e.kind {
	case EventRound:
		return json.Marshal(struct {
			Type VerifiedEventKind `json:"type"`
			RoundEvent
		}{e.kind, e.round})
	case EventReceipt:
		return json.Marshal(struct {
			Type VerifiedEventKind `json:"type"`
			ReceiptEvent
		}{e.kind, e.receipt})
	case EventCheckpoint:
		return json.Marshal(struct {
			Type VerifiedEventKind `json:"type"`
			CheckpointEvent
		}{e.kind, e.checkpoint})
	}
	return nil, errors.New("unknown verified event variant")
}
func copyExit(in *int) *int {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}
func prefix(text string, limit int) string {
	runes := []rune(text)
	if len(runes) > limit {
		runes = runes[:limit]
	}
	return string(runes)
}
func suffix(text string, limit int) string {
	runes := []rune(text)
	if len(runes) > limit {
		runes = runes[len(runes)-limit:]
	}
	return string(runes)
}

// Callback errors and cancellation abort, never become a successful receipt.
func effect[T any](ctx context.Context, run func() (T, error)) (out T, err error) {
	if err = ctx.Err(); err != nil {
		return out, err
	}
	defer func() {
		if recover() != nil {
			var zero T
			out = zero
			err = errors.New("verified-loop callback failed")
		}
	}()
	out, err = run()
	if canceled := ctx.Err(); canceled != nil {
		var zero T
		return zero, canceled
	}
	return out, err
}
func (s *Service) emit(ctx context.Context, event VerifiedEvent) error {
	if s.config.Events == nil {
		return ctx.Err()
	}
	_, err := effect(ctx, func() (bool, error) { return true, s.config.Events.EmitVerifiedEvent(ctx, event) })
	return err
}
func (s *Service) probe(ctx context.Context) (*improvement.InstrumentFingerprint, error) {
	if s.config.Probe == nil {
		return nil, nil
	}
	observed, err := effect(ctx, func() (*improvement.InstrumentFingerprint, error) { return s.config.Probe.ObserveIntegrity(ctx) })
	if err != nil || observed == nil {
		return nil, err
	}
	copy := *observed
	return &copy, nil
}

// RunTask performs execute -> pre-acceptance integrity -> command -> receipt ->
// fold. State and feedback are local to this call; no checkpoint is committed by
// an executor's summary or a passing command evaluated with changed instruments.
func (s *Service) RunTask(ctx context.Context, request string, options TaskOptions) (TaskOutcome, error) {
	if s == nil || s.config.Worker == nil || s.config.Acceptance == nil {
		return TaskOutcome{}, errors.New("verified loop is not initialized")
	}
	if err := ctx.Err(); err != nil {
		return TaskOutcome{}, err
	}
	maximum := DefaultMaxRounds
	if options.MaxRounds != nil {
		maximum = *options.MaxRounds
	}
	baseline, err := s.probe(ctx)
	if err != nil {
		return TaskOutcome{}, err
	}
	if !utf8.ValidString(request) || !utf8.ValidString(options.AcceptanceCommand) {
		return TaskOutcome{}, errors.New("verified task requires scalar UTF-8 text")
	}
	sum := sha256.Sum256([]byte(request))
	requirement, _ := NewRequirement(AcceptanceRequirement, prefix(request, 500))
	requirement.Acceptance = options.AcceptanceCommand
	contract, err := NewTask(TaskSpec{RunID: s.config.RunID, Revision: 1, OriginalRequestHash: hex.EncodeToString(sum[:8]), Requirements: []Requirement{requirement}})
	if err != nil {
		return TaskOutcome{}, err
	}
	checkpoint, err := NewCheckpoint(CheckpointSpec{ContractRevision: 1, Requirements: []RequirementState{{AcceptanceRequirement, Pending}}})
	if err != nil {
		return TaskOutcome{}, err
	}
	hash, err := contract.Hash()
	if err != nil {
		return TaskOutcome{}, err
	}
	receipts := []Receipt{}
	feedback := ""
	for round := int64(1); round <= maximum; round++ {
		objective := request
		if feedback != "" {
			objective += "\n\nPrevious round's verification failed. Evidence:\n" + feedback
		}
		if err := s.emit(ctx, VerifiedEvent{kind: EventRound, round: RoundEvent{Round: round, Objective: prefix(objective, 2000)}}); err != nil {
			return TaskOutcome{}, err
		}
		summary, err := effect(ctx, func() (string, error) { return s.config.Worker.RunWorker(ctx, objective) })
		if err != nil {
			return TaskOutcome{}, err
		}
		tampered := false
		if baseline != nil {
			current, err := s.probe(ctx)
			if err != nil {
				return TaskOutcome{}, err
			}
			tampered = current == nil || *current != *baseline
		}
		result, err := effect(ctx, func() (shell.Result, error) { return s.config.Acceptance.RunAcceptance(ctx, options.AcceptanceCommand) })
		if err != nil {
			return TaskOutcome{}, err
		}
		result.ExitCode = copyExit(result.ExitCode)
		if result.Error != nil {
			v := *result.Error
			result.Error = &v
		}
		if result.Projection != nil {
			v := *result.Projection
			result.Projection = &v
		}
		passed := result.ExitCode != nil && *result.ExitCode == 0 && !result.TimedOut && !tampered
		exit := "None"
		if result.ExitCode != nil {
			exit = strconv.Itoa(*result.ExitCode)
		}
		evidence := []string{"exit:" + exit, "command:" + prefix(options.AcceptanceCommand, 120)}
		if tampered {
			evidence = append(evidence, "instruments:changed-since-baseline")
		}
		verdict := Incomplete
		if passed {
			verdict = Complete
		}
		integrity := Clean
		if result.TimedOut || tampered {
			integrity = Suspect
		}
		receipt, err := NewReceipt(ReceiptSpec{ContractHash: hash, RoundID: RoundID(fmt.Sprintf("round-%d", round)), Verdict: verdict, Integrity: integrity, Coverage: []RequirementID{AcceptanceRequirement}, EvidenceRefs: evidence, VerifierIDs: []string{"command"}})
		if err != nil {
			return TaskOutcome{}, err
		}
		receipts = append(receipts, receipt)
		event := VerifiedEvent{kind: EventReceipt, receipt: ReceiptEvent{Round: round, Verdict: verdict, Integrity: integrity, ExitCode: copyExit(result.ExitCode)}}
		if err := s.emit(ctx, event); err != nil {
			return TaskOutcome{}, err
		}
		if passed {
			checkpoint, err = ApplyPatch(contract, checkpoint, NewPatch(checkpoint.spec.StateRevision, []Operation{SetStatus(AcceptanceRequirement, Verified)}, []Receipt{receipt}))
			if err != nil {
				return TaskOutcome{}, err
			}
			if err := s.emit(ctx, VerifiedEvent{kind: EventCheckpoint, checkpoint: CheckpointEvent{StateRevision: checkpoint.spec.StateRevision, Status: TaskComplete}}); err != nil {
				return TaskOutcome{}, err
			}
			return TaskOutcome{Status: TaskComplete, Rounds: round, Checkpoint: checkpoint, Receipts: receipts, Summary: summary, Integrity: Clean}, nil
		}
		feedback = suffix(result.Render(), 2000)
		if tampered {
			feedback = integrityFeedback + feedback
		}
		// Stop before incrementing the maximum signed revision/round counter.
		if round == maximum {
			break
		}
	}
	if err := s.emit(ctx, VerifiedEvent{kind: EventCheckpoint, checkpoint: CheckpointEvent{StateRevision: checkpoint.spec.StateRevision, Status: TaskUnverified}}); err != nil {
		return TaskOutcome{}, err
	}
	integrity := Clean
	for _, r := range receipts {
		if r.spec.Integrity == Suspect {
			integrity = Suspect
			break
		}
	}
	return TaskOutcome{Status: TaskUnverified, Rounds: maximum, Checkpoint: checkpoint, Receipts: receipts, Summary: fmt.Sprintf("[stopped after %d rounds without verification]\nLast evidence:\n%s", maximum, feedback), Integrity: integrity}, nil
}

// ShellAcceptance uses the caller's already configured workspace/credential/
// sandbox/process policy. Its presence is not a claim of OS confinement.
type ShellAcceptance struct{ Executor *shell.Executor }

func (a ShellAcceptance) RunAcceptance(ctx context.Context, command string) (shell.Result, error) {
	if a.Executor == nil {
		return shell.Result{}, errors.New("acceptance executor is not initialized")
	}
	return a.Executor.ExecuteBashResult(ctx, protocol.BashInput{Command: command})
}

type WorkspaceIntegrity struct{ Workspace string }

func (p WorkspaceIntegrity) ObserveIntegrity(ctx context.Context) (*improvement.InstrumentFingerprint, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	value, err := improvement.VerifierFingerprint(p.Workspace)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &value, nil
}
