package agent

import (
	"context"
	"errors"

	"github.com/luoyjx/mini-loop/go/memory"
	"github.com/luoyjx/mini-loop/go/userresources"
)

const SkillReadonlySession PersonalSkillCode = "readonly_session"
const SkillPublicationFailed PersonalSkillCode = "publication_failed"

type PersonalSkillCommitReceipt struct {
	Session       SessionID                 `json:"session"`
	DraftID       userresources.DraftID     `json:"draft_id"`
	Source        memory.Scope              `json:"source"`
	Name          string                    `json:"name"`
	Digest        userresources.DraftDigest `json:"digest"`
	ContentDigest string                    `json:"content_digest"`
	Warning       *string                   `json:"warning"`
	Idempotent    bool                      `json:"idempotent"`
	Activation    userresources.Activation  `json:"activation"`
}

func skillPublicationFailure(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var failure *userresources.PublicationError
	if !errors.As(err, &failure) {
		return skillPolicyError(SkillPublicationFailed, 500)
	}
	status := 500
	switch failure.Code() {
	case userresources.UserSkillExists:
		status = 409
	case userresources.InvalidOwner, userresources.SecretDetected, userresources.PublicationCode(userresources.InvalidName), userresources.PublicationCode(userresources.InvalidDescription), userresources.PublicationCode(userresources.InvalidBody), userresources.PublicationCode(userresources.UnsafeContent):
		status = 422
	}
	return &PersonalSkillError{code: PersonalSkillCode(failure.Code()), status: status, message: failure.Error()}
}

// CommitPersonalSkill publishes the exact reviewed draft. Peek precedes durable
// publication; only that identity is discarded after success, regardless of TTL.
func (manager *SessionManager) CommitPersonalSkill(ctx context.Context, owner OwnerID, id SessionID, draftID userresources.DraftID, digest userresources.DraftDigest) (receipt PersonalSkillCommitReceipt, err error) {
	operation, err := manager.startPersonalSkillOperation(ctx, owner, id)
	if err != nil {
		return receipt, err
	}
	defer operation.finish()
	defer func() {
		if recover() != nil {
			receipt = PersonalSkillCommitReceipt{}
			err = skillPolicyError(SkillPublicationFailed, 500)
		}
	}()
	session := operation.session
	mode, _ := session.core.control.snapshot()
	if mode == ModeReadonly {
		return receipt, skillPolicyError(SkillReadonlySession, 403)
	}
	draft, err := manager.skillDrafts.Peek(userresources.DraftQuery{ID: draftID, Owner: userresources.OwnerID(owner), Session: userresources.DraftSessionID(id), Digest: &digest})
	if err != nil {
		return receipt, err
	}
	publication, err := manager.config.Services.UserResources.PublishSkill(operation.ctx, userresources.OwnerID(owner), draft.Preview().SkillFields)
	if err != nil {
		return receipt, operation.failure(skillPublicationFailure(err))
	}
	value := publication.Receipt()
	receipt = PersonalSkillCommitReceipt{Session: id, DraftID: draftID, Source: publication.Source(), Name: value.Name, Digest: draft.Preview().Digest, ContentDigest: value.ContentDigest, Warning: value.CollisionWarning, Idempotent: value.Idempotent, Activation: userresources.NextSession}
	// The file has committed. Do not replace success with cancellation/expiry.
	manager.skillDrafts.DiscardCommitted(draft)
	return receipt, nil
}
