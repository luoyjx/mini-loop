package agent

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/luoyjx/mini-loop/go/userresources"
)

type PersonalSkillCode string

const (
	SkillSessionNotFound            PersonalSkillCode = "session_not_found"
	SkillAuthenticatedOwnerRequired PersonalSkillCode = "authenticated_owner_required"
	SkillDisabled                   PersonalSkillCode = "personal_skills_disabled"
	SkillSessionNotReady            PersonalSkillCode = "session_not_ready"
	SkillLeaseLost                  PersonalSkillCode = "session_lease_lost"
)

// PersonalSkillError contains only stable public policy facts.
type PersonalSkillError struct {
	code    PersonalSkillCode
	status  int
	message string
}

func (e *PersonalSkillError) Error() string {
	if e.message != "" {
		return e.message
	}
	return strings.ReplaceAll(string(e.code), "_", " ")
}
func (e *PersonalSkillError) Code() PersonalSkillCode { return e.code }
func (e *PersonalSkillError) StatusCode() int         { return e.status }
func skillPolicyError(code PersonalSkillCode, status int) error {
	return &PersonalSkillError{code: code, status: status}
}

// A skill operation owns admission without becoming a conversation turn.
// Delete/Stop join its lifetime; publication success has a separate commit point.
type personalSkillOperation struct {
	ctx     context.Context
	session *ManagedSession
	cancel  context.CancelCauseFunc
	done    chan struct{}
	unbind  func()
}

func (operation *personalSkillOperation) finish() {
	operation.unbind()
	operation.cancel(nil)
	session := operation.session
	session.mu.Lock()
	session.skillOperation = nil
	close(operation.done)
	session.mu.Unlock()
	session.admission <- struct{}{}
}
func (operation *personalSkillOperation) failure(err error) error {
	if errors.Is(context.Cause(operation.ctx), ErrSessionLeaseLost) || errors.Is(err, ErrSessionLeaseLost) {
		return skillPolicyError(SkillLeaseLost, 409)
	}
	if operation.ctx.Err() != nil {
		return operation.ctx.Err()
	}
	return err
}

func (manager *SessionManager) personalSkillTarget(owner OwnerID, id SessionID) (*ManagedSession, error) {
	session, err := manager.Get(owner, id)
	if err != nil {
		return nil, skillPolicyError(SkillSessionNotFound, 404)
	}
	if owner == "anonymous" {
		return nil, skillPolicyError(SkillAuthenticatedOwnerRequired, 403)
	}
	if manager.config.Services.UserResources == nil {
		return nil, skillPolicyError(SkillDisabled, 404)
	}
	if session.core == nil {
		return nil, skillPolicyError(SkillSessionNotReady, 409)
	}
	return session, nil
}

func (manager *SessionManager) startPersonalSkillOperation(ctx context.Context, owner OwnerID, id SessionID) (*personalSkillOperation, error) {
	session, err := manager.personalSkillTarget(owner, id)
	if err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-session.admission:
	}
	if err = ctx.Err(); err != nil {
		session.admission <- struct{}{}
		return nil, err
	}
	manager.mu.Lock()
	session.mu.Lock()
	if manager.sessions[id] != session || !session.accepting {
		session.mu.Unlock()
		manager.mu.Unlock()
		session.admission <- struct{}{}
		return nil, skillPolicyError(SkillSessionNotFound, 404)
	}
	operationCtx, cancel := context.WithCancelCause(ctx)
	operation := &personalSkillOperation{ctx: operationCtx, session: session, cancel: cancel, done: make(chan struct{})}
	session.skillOperation = operation
	session.mu.Unlock()
	manager.mu.Unlock()
	operation.unbind = session.core.persistence.bindTurn(cancel)
	if err = session.core.persistence.requireLease(operationCtx); err != nil {
		err = operation.failure(err)
		operation.finish()
		return nil, err
	}
	return operation, nil
}

// PreviewPersonalSkill supplies the admitted ledger even when empty.
func (manager *SessionManager) PreviewPersonalSkill(ctx context.Context, owner OwnerID, id SessionID, name, focus string) (preview userresources.DraftPreview, err error) {
	operation, err := manager.startPersonalSkillOperation(ctx, owner, id)
	if err != nil {
		return preview, err
	}
	defer operation.finish()
	session := operation.session
	session.core.mu.Lock()
	defer session.core.mu.Unlock()
	draft, err := session.core.previewPersonalSkillLocked(operation.ctx, name, focus, &session.skillCapture)
	err = operation.failure(err)
	if err != nil {
		manager.skillDrafts.DiscardCommitted(draft)
		return preview, err
	}
	return draft.Preview(), nil
}

func (session *ManagedSession) hasPersonalSkillOperation() bool {
	session.mu.Lock()
	defer session.mu.Unlock()
	return session.skillOperation != nil
}
func (session *ManagedSession) drainPersonalSkillOperation(grace time.Duration) {
	session.mu.Lock()
	operation := session.skillOperation
	session.mu.Unlock()
	if operation == nil {
		return
	}
	timer := time.NewTimer(grace)
	defer timer.Stop()
	select {
	case <-operation.done:
		return
	case <-timer.C:
		operation.cancel(context.Canceled)
	}
	<-operation.done
}
