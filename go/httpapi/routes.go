package httpapi

import (
	"errors"
	"github.com/luoyjx/mini-loop/go/agent"
	"net/http"
	"os"
	"strconv"
	"time"
)

func (s *Server) routes() {
	s.register("/sessions/{session_id}/workflows", routeMethod{routeGet, s.listWorkflows}, routeMethod{routePost, s.launchWorkflow})
	s.register("/sessions/{session_id}/workflows/{run_id}", routeMethod{routeGet, s.workflowDetail})
	s.register("/sessions/{session_id}/workflows/{run_id}/cancel", routeMethod{routePost, s.cancelWorkflow})
	s.register("/sessions/{session_id}/team", routeMethod{routeGet, s.sessionTeam})
	s.register("/sessions/{session_id}/propose-improvement", routeMethod{routePost, s.proposeImprovement})
	s.register("/improvements", routeMethod{routeGet, s.improvements})
	s.register("/self-audit", routeMethod{routeGet, s.selfAuditReport})
	s.register("/self-audit/suggestions", routeMethod{routeGet, s.selfAuditSuggestions})
	s.register("/self-audit/bench-task-drafts", routeMethod{routeGet, s.selfAuditDrafts})
	s.register("/benchmark", routeMethod{routePost, s.benchmark})
	s.register("/sessions/{session_id}/memory", routeMethod{routeGet, s.memoryList})
	s.register("/sessions/{session_id}/memory/{name}", routeMethod{routeGet, s.memoryBody})
	s.register("/sessions/{session_id}/skills", routeMethod{routeGet, s.skillCatalogue})
	s.register("/sessions/{session_id}/personal-skills/preview", routeMethod{routePost, s.previewPersonalSkill})
	s.register("/sessions/{session_id}/personal-skills/{draft_id}/commit", routeMethod{routePost, s.commitPersonalSkill})
	s.register("/sessions/{session_id}/goal", routeMethod{routeGet, s.goal})
	s.register("/sessions/{session_id}/cron", routeMethod{routeGet, s.listCron}, routeMethod{routePost, s.scheduleCron})
	s.register("/sessions/{session_id}/cron/{job_id}", routeMethod{routeDelete, s.cancelCron})
	s.register("/sessions/{session_id}/cron/{job_id}/arm", routeMethod{routePost, s.armCron})
	s.register("/sessions/{session_id}/tasks", routeMethod{routeGet, s.tasks})
	s.register("/", routeMethod{routeGet, s.console})
	s.register("/ui", routeMethod{routeGet, s.webUI})
	s.register("/trajectories/{trajectory_id}/view", routeMethod{routeGet, s.viewTrajectory})
	s.register("/sessions/{session_id}/trajectories", routeMethod{routeGet, s.listTrajectories})
	s.register("/trajectories", routeMethod{routeGet, s.listTrajectories})
	s.register("/trajectories/{trajectory_id}", routeMethod{routeGet, s.getTrajectory})
	s.register("/trajectories/{trajectory_id}/export", routeMethod{routeGet, s.exportTrajectory})
	s.register("/sessions/{session_id}/fork", routeMethod{routePost, s.fork})
	s.register("/sessions/{session_id}/mode", routeMethod{routePost, s.mode})
	s.register("/sessions/{session_id}/steer", routeMethod{routePost, s.steer})
	s.register("/healthz", routeMethod{routeGet, s.health})
	s.register("/sessions", routeMethod{routePost, s.create}, routeMethod{routeGet, s.list})
	s.register("/sessions/{session_id}", routeMethod{routeGet, s.get}, routeMethod{routeDelete, s.delete})
	s.register("/sessions/{session_id}/messages", routeMethod{routePost, s.message})
	s.register("/sessions/{session_id}/messages/stream", routeMethod{routePost, s.messageStream})
	s.register("/sessions/{session_id}/cancel", routeMethod{routePost, s.cancel})
	s.register("/sessions/{session_id}/approvals", routeMethod{routeGet, s.approvals})
	s.register("/sessions/{session_id}/approvals/{approval_id}", routeMethod{routePost, s.resolve})
	s.register("/sessions/{session_id}/events", routeMethod{routeGet, s.observe})
	s.register("/sessions/{session_id}/transcript", routeMethod{routeGet, s.transcript})
}

func (s *Server) fork(w http.ResponseWriter, r *http.Request) {
	session, ok := s.require(w, r)
	if !ok || !s.rate(w, principal(r.Context())) {
		return
	}
	child, err := s.manager.Fork(r.Context(), principal(r.Context()).ID, session.ID())
	if err != nil {
		switch {
		case errors.Is(err, agent.ErrForkBusy):
			writeJSON(s, w, 409, ErrorResponse{err.Error()})
		case errors.Is(err, agent.ErrSessionNotFound):
			writeJSON(s, w, 404, ErrorResponse{"No session '" + string(session.ID()) + "'"})
		default:
			writeJSON(s, w, 503, ErrorResponse{"session fork unavailable"})
		}
		return
	}
	writeJSON(s, w, 200, info(child.Info()))
}
func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	posture, err := s.manager.ObserveRuntimePosture(s.auth.Configured(), nil)
	if err != nil {
		writeJSON(s, w, 500, ErrorResponse{"runtime posture observation failed"})
		return
	}
	v := s.manager.Summary()
	uptime := s.now().Sub(s.started).Seconds()
	writeJSON(s, w, 200, HealthResponse{Posture: posture, Features: s.features, ExperimentalWorkflows: s.manager.Workflows() != nil, Trajectories: s.manager.Trajectories() != nil, Status: "ok", Model: v.Model, FakeLLM: s.fake, ModelConcurrency: v.ModelConcurrency, ToolConcurrency: v.ToolConcurrency, Authenticated: s.auth.Configured(), Build: s.build, PID: os.Getpid(), Started: float64(s.started.UnixMicro()) / 1e6, Uptime: uptime, WorkspaceBinding: v.WorkspaceBinding, Sessions: v.Sessions})
}
func (s *Server) create(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeBody[CreateRequest](s, w, r)
	if !ok {
		return
	}
	if req.Mode != nil && !req.Mode.Valid() {
		writeJSON(s, w, 422, ErrorResponse{"invalid permission mode"})
		return
	}
	mode := agent.ModeInteractive
	if req.Mode != nil {
		mode = *req.Mode
	}
	session, err := s.manager.Create(r.Context(), agent.CreateSessionRequest{Owner: principal(r.Context()).ID, Model: req.Model, System: req.System, PermissionMode: mode, Workspace: req.Workspace})
	if err != nil {
		var binding *agent.WorkspaceBindingError
		if errors.As(err, &binding) {
			writeJSON(s, w, int(binding.Status), ErrorResponse{binding.Detail})
		} else if errors.Is(err, agent.ErrManagerStopped) {
			writeJSON(s, w, 503, ErrorResponse{err.Error()})
		} else {
			writeJSON(s, w, 500, ErrorResponse{"session creation failed"})
		}
		return
	}
	writeJSON(s, w, 200, info(session.Info()))
}
func (s *Server) list(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if value := r.URL.Query().Get("limit"); value != "" {
		v, err := strconv.Atoi(value)
		if err != nil {
			writeJSON(s, w, 422, ErrorResponse{"invalid limit"})
			return
		}
		limit = v
	}
	rows := s.manager.ListRecent(principal(r.Context()).ID, limit)
	result := make([]SessionInfo, 0, len(rows))
	for _, v := range rows {
		result = append(result, info(v))
	}
	writeJSON(s, w, 200, result)
}
func (s *Server) get(w http.ResponseWriter, r *http.Request) {
	if session, ok := s.require(w, r); ok {
		writeJSON(s, w, 200, info(session.Info()))
	}
}
func (s *Server) delete(w http.ResponseWriter, r *http.Request) {
	session, ok := s.require(w, r)
	if !ok {
		return
	}
	if deleted, err := s.manager.Delete(principal(r.Context()).ID, session.ID(), agent.DeleteSessionOptions{}); err != nil || !deleted {
		writeJSON(s, w, 404, ErrorResponse{"No session '" + string(session.ID()) + "'"})
		return
	}
	writeJSON(s, w, 200, DeleteResponse{session.ID()})
}
func (s *Server) message(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeBody[MessageRequest](s, w, r)
	if !ok {
		return
	}
	if req.Message == nil {
		writeJSON(s, w, 422, ErrorResponse{"message is required"})
		return
	}
	session, ok := s.require(w, r)
	if !ok {
		return
	}
	caller := principal(r.Context())
	key := cacheKey{caller.ID, session.ID(), r.Header.Get("Idempotency-Key")}
	now := time.Time{}
	if s.rateLimit > 0 {
		now = s.now()
	}
	s.mu.Lock()
	if cached, found := s.cache[key]; key.Key != "" && found {
		s.mu.Unlock()
		writeJSON(s, w, 200, cached)
		return
	}
	if retry := s.rateLocked(caller, now); retry > 0 {
		s.mu.Unlock()
		s.rateDenial(w, retry)
		return
	}
	if s.running[session.ID()] {
		s.mu.Unlock()
		writeJSON(s, w, 409, ErrorResponse{busyDetail(session.ID())})
		return
	}
	// Cache lookup and this claim form one transition; retain the claim through
	// publication of the completed result, then release before network output.
	s.running[session.ID()] = true
	s.mu.Unlock()
	released := false
	release := func() {
		if released {
			return
		}
		s.mu.Lock()
		delete(s.running, session.ID())
		s.mu.Unlock()
		released = true
	}
	defer release()
	run, err := agent.AuthenticatedHTTPRunContext(agent.ActorID(caller.ID))
	if err != nil {
		release()
		writeJSON(s, w, 500, ErrorResponse{"message identity failed"})
		return
	}
	result, err := session.TryRunWithSnapshot(r.Context(), *req.Message, run)
	if err != nil {
		release()
		if errors.Is(err, agent.ErrSessionBusy) {
			writeJSON(s, w, 409, ErrorResponse{busyDetail(session.ID())})
		} else {
			writeJSON(s, w, 500, ErrorResponse{"turn failed"})
		}
		return
	}
	response := MessageResponse{session.ID(), result.Final, info(result.Info)}
	if key.Key != "" {
		s.mu.Lock()
		if len(s.cache) >= MaxIdempotencyKeys {
			clear(s.cache)
		}
		s.cache[key] = response
		s.mu.Unlock()
	}
	release()
	writeJSON(s, w, 200, response)
}
func (s *Server) cancel(w http.ResponseWriter, r *http.Request) {
	session, ok := s.require(w, r)
	if !ok {
		return
	}
	cancelled, err := session.Cancel(r.Context(), "cancelled over HTTP")
	if err != nil {
		writeJSON(s, w, 500, ErrorResponse{"cancellation wait interrupted"})
		return
	}
	writeJSON(s, w, 200, CancelResponse{session.ID(), cancelled, info(session.Info())})
}
func (s *Server) approvals(w http.ResponseWriter, r *http.Request) {
	if session, ok := s.require(w, r); ok {
		writeJSON(s, w, 200, ApprovalsResponse{session.ID(), s.manager.Approvals().List(session.ID())})
	}
}
func (s *Server) resolve(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeBody[ApprovalRequest](s, w, r)
	if !ok {
		return
	}
	if req.Decision != DecisionAllow && req.Decision != DecisionDeny {
		writeJSON(s, w, 422, ErrorResponse{"invalid approval decision"})
		return
	}
	session, ok := s.require(w, r)
	if !ok {
		return
	}
	id := agent.ApprovalID(r.PathValue("approval_id"))
	if !s.manager.Approvals().Resolve(id, agent.ApprovalResolution{SessionID: session.ID(), Allowed: req.Decision == DecisionAllow, Answer: req.Answer, Remember: req.Remember}) {
		writeJSON(s, w, 404, ErrorResponse{"No pending approval '" + string(id) + "'"})
		return
	}
	writeJSON(s, w, 200, ApprovalResponse{id, req.Decision})
}

func (s *Server) mode(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeBody[ModeRequest](s, w, r)
	if !ok {
		return
	}
	if !req.Mode.Valid() {
		writeJSON(s, w, 422, ErrorResponse{"invalid permission mode"})
		return
	}
	session, ok := s.require(w, r)
	if !ok {
		return
	}
	mode, err := session.ChangePermissionMode(req.Mode)
	if err != nil {
		writeJSON(s, w, 503, ErrorResponse{"session is closed"})
		return
	}
	writeJSON(s, w, 200, ModeResponse{session.ID(), mode})
}
func (s *Server) steer(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeBody[MessageRequest](s, w, r)
	if !ok {
		return
	}
	if req.Message == nil {
		writeJSON(s, w, 422, ErrorResponse{"message is required"})
		return
	}
	session, ok := s.require(w, r)
	if !ok {
		return
	}
	if !s.rate(w, principal(r.Context())) {
		return
	}
	receipt, err := s.manager.Steer(principal(r.Context()).ID, session.ID(), *req.Message)
	if err != nil {
		if errors.Is(err, agent.ErrSessionNotFound) {
			writeJSON(s, w, 404, ErrorResponse{"No session '" + string(session.ID()) + "'"})
		} else {
			writeJSON(s, w, 503, ErrorResponse{"session is closed"})
		}
		return
	}
	writeJSON(s, w, 200, receipt)
}
