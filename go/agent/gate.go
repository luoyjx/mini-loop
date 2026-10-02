package agent

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/luoyjx/mini-loop/go/protocol"
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
	Output string
	Denied bool
	Failed bool
	events []PermissionEvent
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
}

const maxGateProblems = 100

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
	if err := authority.Validate(); err != nil {
		return ToolOutcome{}, err
	}
	if err := call.Validate(); err != nil {
		return ToolOutcome{}, err
	}
	if err := ctx.Err(); err != nil {
		return ToolOutcome{}, err
	}
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
	if !exists {
		outcome.Output = "Unknown tool: " + string(call.Name())
		outcome.Failed = true
	} else {
		if err := ctx.Err(); err != nil {
			return ToolOutcome{}, err
		}
		output, executeErr := definition.handler.ExecuteTool(ctx, authority, call.Input)
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
	return gate.finish(ctx, authority, call, outcome), nil
}
