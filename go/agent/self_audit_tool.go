package agent

import (
	"context"
	"errors"

	"github.com/luoyjx/mini-loop/go/selfaudit"
)

// SelfAuditView is trusted construction policy, never a model argument.
// The zero value confines collection to the session's immutable owner.
type SelfAuditView uint8

const (
	SelfAuditOwnerView SelfAuditView = iota
	SelfAuditOperatorView
)

func (view SelfAuditView) valid() bool {
	return view == SelfAuditOwnerView || view == SelfAuditOperatorView
}

// SelfAuditObserver collects detached observations without reentering the
// running session's serialized turn lock. Implementations must be concurrency-safe.
type SelfAuditObserver interface {
	ObserveSelfAudit(context.Context, selfaudit.Scope) selfaudit.Observations
}

type selfAuditBinding struct {
	observer SelfAuditObserver
	owner    OwnerID
	view     SelfAuditView
}

func (binding selfAuditBinding) report(ctx context.Context) (report string, err error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if binding.observer == nil {
		return "Error: no manager in scope; self_audit runs inside a managed session", nil
	}
	defer func() {
		if recover() != nil {
			report = ""
			err = errors.New("self-audit observation failed")
		}
	}()
	scope := selfaudit.Scope{IncludeGlobal: binding.view == SelfAuditOperatorView}
	if binding.view == SelfAuditOwnerView {
		owner := string(binding.owner)
		scope.Owner = &owner
	}
	observations := binding.observer.ObserveSelfAudit(ctx, scope)
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return selfaudit.BuildReport(observations, scope), nil
}
