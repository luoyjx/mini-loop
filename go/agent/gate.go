package agent

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/shell"
)

type BeforeDecisionKind string

const (
	BeforeKeep    BeforeDecisionKind = "keep"
	BeforeRewrite BeforeDecisionKind = "rewrite"
	BeforeDeny    BeforeDecisionKind = "deny"
)

type BeforeDecision struct {
	kind    BeforeDecisionKind
	input   protocol.ToolInput
	message string
}

func KeepToolCall() BeforeDecision { return BeforeDecision{kind: BeforeKeep} }
func RewriteToolCall(input protocol.ToolInput) BeforeDecision {
	return BeforeDecision{kind: BeforeRewrite, input: input}
}
func DenyToolCall(message string) BeforeDecision {
	return BeforeDecision{kind: BeforeDeny, message: message}
}

type BeforeHook interface {
	BeforeTool(context.Context, ToolAuthority, ToolCall) (BeforeDecision, error)
}

type GuardHook interface {
	// A guard can deny or abstain; there is no allow verdict.
	GuardTool(context.Context, ToolAuthority, ToolCall) (message string, denied bool, err error)
}

type AfterHook interface {
	AfterTool(context.Context, ToolAuthority, ToolCall, string) (string, error)
}

type ResultObserver interface {
	OnResult(context.Context, ToolAuthority, ToolCall, ToolOutcome) error
}

type ToolOutcome struct {
	Output         string
	Denied         bool
	Failed         bool
	events         []PermissionEvent
	ActionID       ActionID
	Replayed       bool
	reconciliation *ActionReconciliation
	commandResult  *shell.Metadata
	inputHash      StepHash
}

// CommandResult is present only for a newly executed structured Bash call.
// Replays have the stored text but no freshly observed process metadata.
func (outcome ToolOutcome) CommandResult() (shell.Metadata, bool) {
	if outcome.commandResult == nil {
		return shell.Metadata{}, false
	}
	return outcome.commandResult.Clone(), true
}

func (outcome ToolOutcome) Reconciliation() (ActionReconciliation, bool) {
	if outcome.reconciliation == nil {
		return ActionReconciliation{}, false
	}
	return *outcome.reconciliation, true
}

type ActionReconciliation struct {
	ActionID   ActionID
	Verdict    EffectVerdict
	Verifiable bool
}

func (outcome ToolOutcome) PermissionEvents() []PermissionEvent {
	return append([]PermissionEvent(nil), outcome.events...)
}

func (outcome ToolOutcome) IsError() bool { return outcome.Denied || outcome.Failed }

type GateHooks struct {
	Before    []BeforeHook
	Guards    []GuardHook
	After     []AfterHook
	Observers []ResultObserver
}

type ToolGate struct {
	catalog   *ToolCatalog
	policy    *PermissionPolicy
	before    []BeforeHook
	guards    []GuardHook
	after     []AfterHook
	observers []ResultObserver
	mu        sync.Mutex
	problems  []string
	journal   ActionJournal
	secrets   TextMasker
}

const maxGateProblems = 100

// NewJournaledToolGate binds replay/settlement to this gate instance. A nil
// journal preserves the bare-agent path; storage is an explicit caller choice.
func NewJournaledToolGate(catalog *ToolCatalog, policy *PermissionPolicy, hooks GateHooks, journal ActionJournal) (*ToolGate, error) {
	gate, err := NewToolGate(catalog, policy, hooks)
	if err != nil {
		return nil, err
	}
	gate.journal = journal
	return gate, nil
}

func NewToolGate(catalog *ToolCatalog, policy *PermissionPolicy, hooks GateHooks) (*ToolGate, error) {
	if catalog == nil || policy == nil {
		return nil, errors.New("tool gate requires a catalogue and permission policy")
	}
	return &ToolGate{
		catalog: catalog, policy: policy,
		before:    append([]BeforeHook(nil), hooks.Before...),
		guards:    append([]GuardHook(nil), hooks.Guards...),
		after:     append([]AfterHook(nil), hooks.After...),
		observers: append([]ResultObserver(nil), hooks.Observers...),
	}, nil
}

func (gate *ToolGate) CatalogNames() []protocol.ToolName { return gate.catalog.Names() }

func (gate *ToolGate) Problems() []string {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	return append([]string(nil), gate.problems...)
}

func (gate *ToolGate) recordProblem(problem string) {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	if len(gate.problems) == maxGateProblems {
		copy(gate.problems, gate.problems[1:])
		gate.problems[len(gate.problems)-1] = problem
		return
	}
	gate.problems = append(gate.problems, problem)
}

func notifyObserver(observer ResultObserver, ctx context.Context, authority ToolAuthority, call ToolCall, outcome ToolOutcome) (err error) {
	defer func() {
		if failure := recover(); failure != nil {
			err = fmt.Errorf("observer panic: %T", failure)
		}
	}()
	return observer.OnResult(ctx, authority, call, outcome)
}

func (gate *ToolGate) finish(ctx context.Context, authority ToolAuthority, call ToolCall, outcome ToolOutcome) ToolOutcome {
	outcome.Output = maskedText(gate.secrets, outcome.Output)
	return gate.observe(ctx, authority, call, outcome)
}
func (gate *ToolGate) observe(ctx context.Context, authority ToolAuthority, call ToolCall, outcome ToolOutcome) ToolOutcome {
	for i := range outcome.events {
		outcome.events[i].Reason = maskedText(gate.secrets, outcome.events[i].Reason)
		outcome.events[i].Rule = maskedText(gate.secrets, outcome.events[i].Rule)
	}
	outcome.ActionID = authority.ActionID
	outcome.inputHash, _ = InputStepHash(call.Input)
	for _, observer := range gate.observers {
		if err := notifyObserver(observer, ctx, authority, call, outcome); err != nil {
			gate.recordProblem(fmt.Sprintf("result observer failed for %s: %T", call.Name(), err))
		}
	}
	return outcome
}

// Dispatch is the only path from a model tool_use to a handler. Guards and
// permissions see all before-hook rewrites; denials bypass replacement hooks.
func (gate *ToolGate) Dispatch(ctx context.Context, authority ToolAuthority, call ToolCall) (ToolOutcome, error) {
	return gate.dispatch(ctx, authority, call, nil)
}

func (gate *ToolGate) dispatch(ctx context.Context, authority ToolAuthority, call ToolCall, reconciled func(ActionReconciliation)) (returned ToolOutcome, dispatchError error) {
	if err := authority.Validate(); err != nil {
		return ToolOutcome{}, err
	}
	if err := call.Validate(); err != nil {
		return ToolOutcome{}, err
	}
	if err := ctx.Err(); err != nil {
		return ToolOutcome{}, err
	}
	if authority.RunContext.MessageID() == "" {
		run, err := DefaultRunContext()
		if err != nil {
			return ToolOutcome{}, err
		}
		authority.RunContext = run
	}
	id, err := ToolActionID(authority.SessionID, authority.RunContext, call)
	if err != nil {
		return ToolOutcome{}, err
	}
	authority.ActionID = id
	authority.ToolUseID = call.ID
	journalStarted := false
	defer func() {
		if ctx.Err() != nil && journalStarted {
			if _, err := gate.journal.Finish(context.WithoutCancel(ctx), ActionSettlement{ActionID: id, Status: ActionCancelled}); err != nil {
				dispatchError = err
			}
		}
	}()
	failed := func(err error) ToolOutcome {
		return gate.finish(ctx, authority, call, ToolOutcome{Output: "Error: " + err.Error(), Failed: true})
	}
	for _, hook := range gate.before {
		decision, err := hook.BeforeTool(ctx, authority, call)
		if ctx.Err() != nil {
			return ToolOutcome{}, ctx.Err()
		}
		if err != nil {
			return failed(err), nil
		}
		switch decision.kind {
		case BeforeKeep:
		case BeforeRewrite:
			if err := decision.input.Validate(); err != nil || decision.input.Name() != call.Name() {
				return failed(errors.New("before hook returned an invalid tool-input rewrite")), nil
			}
			call.Input = decision.input
		case BeforeDeny:
			return gate.finish(ctx, authority, call, ToolOutcome{Output: decision.message, Denied: true}), nil
		default:
			return failed(fmt.Errorf("before hook returned invalid decision %q", decision.kind)), nil
		}
	}
	for _, hook := range gate.guards {
		message, denied, err := hook.GuardTool(ctx, authority, call)
		if ctx.Err() != nil {
			return ToolOutcome{}, ctx.Err()
		}
		if err != nil {
			return failed(err), nil
		}
		if denied {
			return gate.finish(ctx, authority, call, ToolOutcome{Output: message, Denied: true}), nil
		}
	}
	definition, exists := gate.catalog.Lookup(call.Name())
	risk := RiskUnclassified
	if exists {
		risk = definition.risk
	}
	permission, err := gate.policy.Evaluate(ctx, authority, call, risk, exists)
	if ctx.Err() != nil {
		return ToolOutcome{}, ctx.Err()
	}
	if err != nil {
		return failed(err), nil
	}
	if permission.denied {
		return gate.finish(ctx, authority, call, ToolOutcome{Output: permission.message, Denied: true, events: permission.Events()}), nil
	}
	outcome := ToolOutcome{events: permission.Events()}
	if gate.journal != nil {
		prior, err := gate.journal.Begin(ctx, ActionRequest{ActionID: id, SessionID: authority.SessionID, MessageID: authority.RunContext.MessageID(), ToolUseID: call.ID, Input: call.Input})
		if err == nil && prior.Status == ActionStarted {
			journalStarted = true
		}
		if ctx.Err() != nil {
			return ToolOutcome{}, ctx.Err()
		}
		if err != nil {
			return failed(err), nil
		}
		switch {
		case prior.Status.Terminal():
			outcome.Replayed = true
			if prior.Result != nil {
				outcome.Output = *prior.Result
			}
		case prior.Status == ActionUnknown:
			verdict := verifyEffect(ctx, definition, authority, call)
			if ctx.Err() != nil {
				return ToolOutcome{}, ctx.Err()
			}
			outcome.reconciliation = &ActionReconciliation{id, verdict, definition.verifier != nil}
			if reconciled != nil {
				reconciled(*outcome.reconciliation)
			}
			switch verdict {
			case EffectAlreadyApplied:
				outcome.Output, outcome.Replayed = ReconciledActionResult, true
				if reconciler, ok := gate.journal.(ActionReconciler); ok {
					if _, err := reconciler.Reconcile(ctx, ActionSettlement{id, ActionCompleted, &outcome.Output}); err != nil {
						return failed(err), nil
					}
				}
			case EffectNotApplied:
				journalStarted = true
			default:
				outcome.Output, outcome.Replayed = unknownToolResult, true
			}
		default:
			journalStarted = true
		}
	}
	if outcome.Replayed {
		// Post hooks still transform a replay, as in the Python tool boundary.
	} else if !exists {
		outcome.Output = "Unknown tool: " + string(call.Name())
		outcome.Failed = true
	} else {
		if err := ctx.Err(); err != nil {
			return ToolOutcome{}, err
		}
		var output string
		var executeErr error
		if handler, ok := definition.handler.(detailedToolHandler); ok {
			var command *shell.Result
			output, command, executeErr = handler.executeDetailedTool(ctx, authority, call.Input)
			if command != nil {
				metadata := command.Metadata()
				outcome.commandResult = &metadata
				outcome.Failed = command.Failed()
			}
		} else {
			output, executeErr = definition.handler.ExecuteTool(ctx, authority, call.Input)
		}
		if ctx.Err() != nil {
			return ToolOutcome{}, ctx.Err()
		}
		if executeErr != nil {
			if ctx.Err() != nil {
				return ToolOutcome{}, ctx.Err()
			}
			outcome.Output = "Error: " + executeErr.Error()
			outcome.Failed = true
		} else {
			outcome.Output = output
		}
	}
	for _, hook := range gate.after {
		output, err := hook.AfterTool(ctx, authority, call, outcome.Output)
		if err != nil {
			outcome.Output = "Error: " + err.Error()
			outcome.Failed = true
			break
		}
		outcome.Output = output
	}
	outcome.Output = maskedText(gate.secrets, outcome.Output)
	if journalStarted {
		status := ActionCompleted
		if outcome.Denied {
			status = ActionDenied
		} else if outcome.Failed {
			status = ActionFailed
		}
		if _, err := gate.journal.Finish(ctx, ActionSettlement{id, status, &outcome.Output}); err != nil {
			return ToolOutcome{}, err
		}
		journalStarted = false
	}
	return gate.observe(ctx, authority, call, outcome), nil
}
