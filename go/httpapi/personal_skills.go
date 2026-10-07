package httpapi

import (
	"errors"
	"net/http"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/userresources"
)

type PersonalSkillErrorResponse struct {
	Detail PersonalSkillErrorDetail `json:"detail"`
}

type PersonalSkillErrorDetail struct {
	Code    agent.PersonalSkillCode `json:"code"`
	Message string                  `json:"message"`
}

func (s *Server) personalSkillFailure(w http.ResponseWriter, err error) {
	var policy *agent.PersonalSkillError
	var draft *userresources.DraftError
	status := http.StatusInternalServerError
	detail := PersonalSkillErrorDetail{Code: "personal_skill_failed", Message: "personal skill operation failed"}
	if errors.As(err, &policy) {
		status = policy.StatusCode()
		detail = PersonalSkillErrorDetail{Code: policy.Code(), Message: policy.Error()}
	} else if errors.As(err, &draft) {
		status = draft.StatusCode()
		detail = PersonalSkillErrorDetail{Code: agent.PersonalSkillCode(draft.Code()), Message: draft.Error()}
	}
	writeJSON(s, w, status, PersonalSkillErrorResponse{Detail: detail})
}

func (s *Server) previewPersonalSkill(w http.ResponseWriter, r *http.Request) {
	req, ok := decodePersonalSkillBody[PersonalSkillPreviewRequest](s, w, r, true)
	if !ok {
		return
	}
	session, ok := s.require(w, r)
	if !ok {
		return
	}
	preview, err := s.manager.PreviewPersonalSkill(r.Context(), principal(r.Context()).ID, session.ID(), req.Name, req.Focus)
	if err != nil {
		s.personalSkillFailure(w, err)
		return
	}
	writeJSON(s, w, http.StatusOK, preview)
}

func (s *Server) commitPersonalSkill(w http.ResponseWriter, r *http.Request) {
	req, ok := decodePersonalSkillBody[PersonalSkillCommitRequest](s, w, r, false)
	if !ok {
		return
	}
	session, ok := s.require(w, r)
	if !ok {
		return
	}
	receipt, err := s.manager.CommitPersonalSkill(r.Context(), principal(r.Context()).ID, session.ID(), userresources.DraftID(r.PathValue("draft_id")), req.Digest)
	if err != nil {
		s.personalSkillFailure(w, err)
		return
	}
	writeJSON(s, w, http.StatusOK, receipt)
}
