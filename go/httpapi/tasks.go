package httpapi

import (
	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/tasks"
	"net/http"
)

type TaskBoardRow struct {
	ID        tasks.ID     `json:"id"`
	Subject   string       `json:"subject"`
	Status    tasks.Status `json:"status"`
	Owner     *tasks.Owner `json:"owner"`
	BlockedBy []tasks.ID   `json:"blockedBy"`
	Worktree  *string      `json:"worktree"`
}
type TaskBoardResponse struct {
	Session agent.SessionID `json:"session"`
	Tasks   []TaskBoardRow  `json:"tasks"`
}

func (s *Server) tasks(w http.ResponseWriter, r *http.Request) {
	session, ok := s.require(w, r)
	if !ok {
		return
	}
	store, err := tasks.New(tasks.Config{Workspace: session.Info().Workspace})
	if err != nil {
		writeJSON(s, w, 500, ErrorResponse{"task board could not be read"})
		return
	}
	board, err := store.List()
	if err != nil {
		writeJSON(s, w, 500, ErrorResponse{"task board could not be read"})
		return
	}
	response := TaskBoardResponse{Session: session.ID(), Tasks: []TaskBoardRow{}}
	for _, task := range board {
		response.Tasks = append(response.Tasks, TaskBoardRow{task.ID, task.Subject, task.Status, task.Owner, task.BlockedBy, task.Worktree})
	}
	writeJSON(s, w, 200, response)
}
