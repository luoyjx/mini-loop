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
	code   PersonalSkillCode
	status int
}

func (e *PersonalSkillError) Error() string           { return strings.ReplaceAll(string(e.code), "_", " ") }
func (e *PersonalSkillError) Code() PersonalSkillCode { return e.code }
func (e *PersonalSkillError) StatusCode() int         { return e.status }
func skillPolicyError(code PersonalSkillCode, status int) error {
	return &PersonalSkillError{code, status}
}

// A preview owns admission without becoming a conversation turn. Operator turn
// cancellation/status remain unchanged; Delete/Stop explicitly join this work.
type skillPreviewOperation struct {
	cancel context.CancelCauseFunc
	done   chan struct{}
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

// PreviewPersonalSkill is an owner-scoped operator operation. It supplies the
// admitted-turn ledger even when empty and grants no publication permission.
func (manager *SessionManager) PreviewPersonalSkill(ctx context.Context, owner OwnerID, id SessionID, name, focus string) (preview userresources.DraftPreview, err error) {
	session, err := manager.personalSkillTarget(owner, id)
	if err != nil {
		return preview, err
	}
	select {
	case <-ctx.Done():
		return preview, ctx.Err()
	case <-session.admission:
	}
	defer func() { session.admission <- struct{}{} }()
	if err = ctx.Err(); err != nil {
		return preview, err
	}
	manager.mu.Lock()
	session.mu.Lock()
	if manager.sessions[id] != session || !session.accepting {
		session.mu.Unlock()
		manager.mu.Unlock()
		return preview, skillPolicyError(SkillSessionNotFound, 404)
	}
	operationCtx, cancel := context.WithCancelCause(ctx)
	operation := &skillPreviewOperation{cancel: cancel, done: make(chan struct{})}
	session.skillPreviewOperation = operation
	session.mu.Unlock()
	manager.mu.Unlock()
	unbind := session.core.persistence.bindTurn(cancel)
	var draft userresources.Draft
	defer func() {
		if errors.Is(context.Cause(operationCtx), ErrSessionLeaseLost) || errors.Is(err, ErrSessionLeaseLost) {
			err = skillPolicyError(SkillLeaseLost, 409)
		} else if operationCtx.Err() != nil {
			err = operationCtx.Err()
		}
		if err != nil {
			manager.skillDrafts.DiscardCommitted(draft)
			preview = userresources.DraftPreview{}
		}
		unbind()
		cancel(nil)
		session.mu.Lock()
		session.skillPreviewOperation = nil
		close(operation.done)
		session.mu.Unlock()
	}()
	if err = session.core.persistence.requireLease(operationCtx); err != nil {
		return preview, err
	}
	session.core.mu.Lock()
	defer session.core.mu.Unlock()
	draft, err = session.core.previewPersonalSkillLocked(operationCtx, name, focus, &session.skillCapture)
	if err == nil {
		preview = draft.Preview()
	}
	return preview, err
}

func (session *ManagedSession) hasSkillPreview() bool {
	session.mu.Lock()
	defer session.mu.Unlock()
	return session.skillPreviewOperation != nil
}
func (session *ManagedSession) drainSkillPreview(grace time.Duration) {
	session.mu.Lock()
	operation := session.skillPreviewOperation
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
