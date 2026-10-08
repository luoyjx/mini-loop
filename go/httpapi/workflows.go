package httpapi

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/luoyjx/mini-loop/go/workflows"
)

type WorkflowListResponse struct {
	Enabled bool                   `json:"enabled"`
	Runs    []workflows.RunSummary `json:"runs"`
}
type WorkflowCancelResponse struct {
	RunID  workflows.RunID     `json:"run_id"`
	Status workflows.RunStatus `json:"status"`
}

func (s *Server) workflowFailure(w http.ResponseWriter, id workflows.RunID, err error) {
	var failure *workflows.StoreError
	if errors.As(err, &failure) && failure.Kind == workflows.StoreNotFound {
		writeJSON(s, w, 404, ErrorResponse{fmt.Sprintf("No workflow run '%s'", id)})
		return
	}
	writeJSON(s, w, 500, ErrorResponse{"Internal Server Error"})
}

func (s *Server) listWorkflows(w http.ResponseWriter, r *http.Request) {
	parent, ok := s.require(w, r)
	if !ok {
		return
	}
	service := s.manager.Workflows()
	if service == nil {
		writeJSON(s, w, 200, WorkflowListResponse{Enabled: false, Runs: []workflows.RunSummary{}})
		return
	}
	runs, err := service.Summaries(workflows.SessionID(parent.ID()))
	if err != nil {
		s.workflowFailure(w, "", err)
		return
	}
	writeJSON(s, w, 200, WorkflowListResponse{Enabled: true, Runs: runs})
}

func (s *Server) workflowDetail(w http.ResponseWriter, r *http.Request) {
	parent, ok := s.require(w, r)
	if !ok {
		return
	}
	service := s.manager.Workflows()
	if service == nil {
		writeJSON(s, w, 404, ErrorResponse{"workflows are not enabled"})
		return
	}
	id := workflows.RunID(r.PathValue("run_id"))
	scope := workflows.SessionID(parent.ID())
	result, err := service.Status(id, &scope)
	if err != nil {
		s.workflowFailure(w, id, err)
		return
	}
	writeJSON(s, w, 200, result)
}

func (s *Server) cancelWorkflow(w http.ResponseWriter, r *http.Request) {
	// FastAPI validates the required request model before entering owner lookup.
	input, ok := decodeRequestBody(s, w, r)
	if !ok {
		return
	}
	issue := func(code RequestValidationCode, field, message string, value ValidationInput) {
		loc := []ValidationLocation{{field: "body"}}
		if field != "" {
			loc = append(loc, ValidationLocation{field: field})
		}
		writeRequestValidation(s, w, RequestValidationResponse{Detail: []RequestValidationDetail{{Type: code, Location: loc, Message: message, Input: value}}})
	}
	if input.kind == validationNull {
		issue(validationMissing, "", "Field required", input)
		return
	}
	if input.kind != validationObject {
		issue(validationModel, "", "Input should be a valid dictionary or object to extract fields from", input)
		return
	}
	reason := "requested by operator"
	for _, member := range input.members {
		if member.key != "reason" {
			continue
		}
		if member.value.kind != validationText {
			issue(validationString, "reason", "Input should be a valid string", member.value)
			return
		}
		reason = member.value.text
	}
	parent, ok := s.require(w, r)
	if !ok {
		return
	}
	service := s.manager.Workflows()
	if service == nil {
		writeJSON(s, w, 404, ErrorResponse{"workflows are not enabled"})
		return
	}
	id := workflows.RunID(r.PathValue("run_id"))
	scope := workflows.SessionID(parent.ID())
	result, err := service.Cancel(r.Context(), id, &scope, reason)
	if err != nil {
		s.workflowFailure(w, id, err)
		return
	}
	writeJSON(s, w, 200, WorkflowCancelResponse{RunID: id, Status: result.Status})
}
