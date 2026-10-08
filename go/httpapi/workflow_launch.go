package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/workflows"
)

type WorkflowLaunchResponse struct {
	agent.WorkflowLaunchResult
	ActionID agent.ActionID `json:"action_id"`
}

func (s *Server) workflowLaunchFailure(w http.ResponseWriter, err error) {
	status := 500
	var service *agent.WorkflowServiceError
	var conflict *agent.ActionJournalConflict
	var validation *workflows.ValidationError
	var store *workflows.StoreError
	switch {
	case errors.As(err, &service):
		switch service.Kind {
		case agent.WorkflowPermissionError:
			status = 403
		case agent.WorkflowLookupError:
			status = 404
		case agent.WorkflowValueError:
			status = 400
		}
	case errors.As(err, &conflict):
		status = 409
	case errors.As(err, &validation):
		status = 400
	case errors.As(err, &store):
		switch store.Kind {
		case workflows.StoreNotFound:
			status = 404
		case workflows.StoreValueFailure:
			status = 400
		}
	case errors.Is(err, jsonvalue.ErrNonfinite), errors.Is(err, jsonvalue.ErrSurrogate):
		status = 400
	case errors.Is(err, workflows.ErrDefinition):
		status = 400
	}
	detail := "Internal Server Error"
	if status != 500 {
		detail = err.Error()
		if status == 400 {
			chars := []rune(detail)
			if len(chars) > 500 {
				detail = string(chars[:500])
			}
		}
	}
	writeJSON(s, w, status, ErrorResponse{Detail: detail})
}

func (s *Server) launchWorkflow(w http.ResponseWriter, r *http.Request) {
	input, ok := decodeRequestBody(s, w, r)
	if !ok {
		return
	}
	body, details, err := workflowLaunchValidation(input)
	if len(details) > 0 {
		writeRequestValidation(s, w, RequestValidationResponse{Detail: details})
		return
	}
	if err != nil {
		writeRequestDiagnosticFailure(w)
		return
	}
	parent, ok := s.require(w, r)
	if !ok {
		return
	}
	service := s.manager.Workflows()
	if service == nil {
		writeJSON(s, w, 404, ErrorResponse{Detail: "workflows are not enabled"})
		return
	}
	if !s.auth.Configured() {
		writeJSON(s, w, 403, ErrorResponse{Detail: "workflow launch over HTTP requires an authenticated deployment; an anonymous bind cannot stamp explicit_human authority"})
		return
	}
	action := agent.ActionID("")
	if body.ActionID != nil {
		action = *body.ActionID
	}
	if action == "" {
		var uuid [16]byte
		if _, err := rand.Read(uuid[:]); err != nil {
			s.workflowLaunchFailure(w, err)
			return
		}
		uuid[6] = (uuid[6] & 0x0f) | 0x40
		action = agent.ActionID("wfhttp_" + hex.EncodeToString(uuid[:8]))
	}
	live, err := agent.WorkflowHTTPRunContext(agent.ActorID(principal(r.Context()).ID), action)
	if err != nil {
		s.workflowLaunchFailure(w, err)
		return
	}
	result, err := service.Launch(r.Context(), agent.WorkflowLaunchRequest{SessionID: parent.ID(), Input: protocol.WorkflowInput{Definition: body.Definition, Args: body.Args}, Context: live, ActionID: action})
	if err != nil {
		s.workflowLaunchFailure(w, err)
		return
	}
	writeJSON(s, w, 200, WorkflowLaunchResponse{WorkflowLaunchResult: result, ActionID: action})
}
