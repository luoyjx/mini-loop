package agent

import (
	"context"
	"strings"

	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/userresources"
)

type skillPreviewModel struct{ session *Session }

func (model skillPreviewModel) CompletePersonalSkillPreview(ctx context.Context, request userresources.SkillPreviewRequest) (string, error) {
	system := request.System()
	reply, err := model.session.completeSideModel(ctx, protocol.ModelRequest{
		Model: model.session.model, MaxTokens: request.MaxTokens(), Purpose: protocol.PurposePersonalSkillPreview,
		System: &system, Tools: []protocol.ToolSchema{},
		Messages: []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent(request.Prompt())}},
	})
	if err != nil {
		return "", err
	}
	var text strings.Builder
	for _, block := range reply.Content {
		if value, ok := block.Text(); ok {
			text.WriteString(value.Text)
		}
	}
	return text.String(), nil
}

// PreviewPersonalSkill is an explicit standalone operation. It serializes with
// core turns and binds draft identity to this session; it neither publishes a
// skill nor grants HTTP/model authority. Managed callers use their own admission
// and provenance ledger through the private locked helper when wired.
func (s *Session) PreviewPersonalSkill(ctx context.Context, name, focus string) (userresources.Draft, error) {
	select {
	case <-ctx.Done():
		return userresources.Draft{}, ctx.Err()
	case <-s.turn:
	}
	defer func() { s.turn <- struct{}{} }()
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return userresources.Draft{}, err
	}
	return s.previewPersonalSkillLocked(ctx, name, focus, nil)
}

// Caller owns the core turn mutex; the model side request retains non-live
// history ownership even though normal cache/recovery/limiter/events are used.
func (s *Session) previewPersonalSkillLocked(ctx context.Context, name, focus string, ledger *userresources.CaptureLedger) (userresources.Draft, error) {
	if s.skillPreview == nil {
		var err error
		s.skillPreview, err = userresources.NewSkillPreviewer(userresources.SkillPreviewConfig{Model: skillPreviewModel{s}, Secrets: s.secrets, Drafts: s.skillDrafts})
		if err != nil {
			return userresources.Draft{}, err
		}
	}
	return s.skillPreview.Preview(ctx, userresources.SkillPreviewInput{Owner: userresources.OwnerID(s.owner), Session: userresources.DraftSessionID(s.id), Name: name, Focus: focus, Ledger: ledger, History: s.messages})
}
