package httpapi

import (
	"encoding/json"
	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/improvement"
	"net/http"
)

type ImprovementsResponse struct {
	Proposals []improvement.ArchiveValue `json:"proposals"`
}

// Source GET ignores body/query overrides, spends no rate budget and returns
// historical records without remasking. All serialization finishes before headers;
// source read/UTF-8/nonfinite/shape errors return the same private plain 500.
func (s *Server) improvements(w http.ResponseWriter, r *http.Request) {
	var owner *agent.OwnerID
	if s.auth.Configured() {
		caller := principal(r.Context()).ID
		owner = &caller
	}
	rows, err := s.manager.ListImprovements(r.Context(), owner)
	if err != nil {
		writeSelfAuditFailure(w)
		return
	}
	body, err := json.Marshal(ImprovementsResponse{Proposals: rows})
	if err != nil {
		writeSelfAuditFailure(w)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}
