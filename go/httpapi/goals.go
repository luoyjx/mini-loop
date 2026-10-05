package httpapi

import (
	"github.com/luoyjx/mini-loop/go/agent"
	"net/http"
)

type GoalResponse struct {
	Session   agent.SessionID   `json:"session"`
	Goal      *agent.GoalRecord `json:"goal"`
	GoalArmed bool              `json:"goal_armed"`
	PlanMode  bool              `json:"plan_mode"`
}

func (s *Server) goal(w http.ResponseWriter, r *http.Request) {
	session, ok := s.require(w, r)
	if !ok {
		return
	}
	value := session.GoalSnapshot()
	writeJSON(s, w, 200, GoalResponse{session.ID(), value.Goal, value.Armed, session.PlanModeActive()})
}
