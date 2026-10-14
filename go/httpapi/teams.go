package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/teams"
)

type TeamResponse struct {
	Session agent.SessionID   `json:"session"`
	Team    *teams.TeamID     `json:"team"`
	Name    *teams.MemberName `json:"name,omitempty"`
	Inbox   *[]teams.Message  `json:"inbox,omitempty"`
}

func (s *Server) sessionTeam(w http.ResponseWriter, r *http.Request) {
	session, ok := s.require(w, r)
	if !ok {
		return
	}
	view, err := s.manager.PeekTeam(r.Context(), principal(r.Context()).ID, session.ID())
	if err != nil {
		if errors.Is(err, agent.ErrSessionNotFound) {
			writeJSON(s, w, 404, ErrorResponse{fmt.Sprintf("No session '%s'", session.ID())})
		} else {
			writeTeamInternalFailure(w)
		}
		return
	}
	response := TeamResponse{Session: view.Session}
	if view.Identity != nil {
		response.Team = &view.Identity.Team
		response.Name = &view.Identity.Name
		response.Inbox = &view.Inbox
	}
	// Historical mailbox data is returned as in source, without a second mask.
	// Validate the complete projection before writing headers; peek never drains.
	data, err := json.Marshal(response)
	if err != nil {
		writeTeamInternalFailure(w)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// Source's uncaught mailbox/encoding failures reach the outer framework error
// response after security-header middleware unwinds. Keep this projection local
// to the team route; ordinary denials and successful views retain those headers.
func writeTeamInternalFailure(w http.ResponseWriter) {
	for _, name := range []string{"Content-Security-Policy", "X-Content-Type-Options", "X-Frame-Options", "Referrer-Policy"} {
		w.Header().Del(name)
	}
	writeSelfAuditFailure(w)
}
