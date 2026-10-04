package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/trajectory"
)

const MaxTrajectoryJSONBytes int64 = 8 * 1024 * 1024

func (s *Server) trajectoryStore(w http.ResponseWriter) agent.TrajectoryReader {
	store := s.manager.Trajectories()
	if store == nil {
		writeJSON(s, w, 503, ErrorResponse{"Trajectory recording is disabled"})
	}
	return store
}
func (s *Server) ownsTrajectory(caller Principal, row agent.TrajectorySummary) bool {
	if row.Owner != nil {
		return *row.Owner == caller.ID
	}
	return s.manager.RememberedOwners()[row.Session] == caller.ID
}
func trajectoryLimit(w http.ResponseWriter, r *http.Request, s *Server) (int, bool) {
	limit := 100
	if value, ok := r.URL.Query()["limit"]; ok {
		parsed, err := strconv.Atoi(value[0])
		if err != nil {
			writeJSON(s, w, 422, ErrorResponse{"invalid limit"})
			return 0, false
		}
		limit = parsed
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 500 {
		limit = 500
	}
	return limit, true
}
func (s *Server) listTrajectories(w http.ResponseWriter, r *http.Request) {
	var session *agent.SessionID
	if r.PathValue("session_id") != "" {
		if _, ok := s.require(w, r); !ok {
			return
		}
		id := agent.SessionID(r.PathValue("session_id"))
		session = &id
	} else if value, ok := r.URL.Query()["session_id"]; ok {
		id := agent.SessionID(value[0])
		if _, err := s.manager.Get(principal(r.Context()).ID, id); err != nil {
			writeJSON(s, w, 404, ErrorResponse{"No such session"})
			return
		}
		session = &id
	}
	store := s.trajectoryStore(w)
	if store == nil {
		return
	}
	limit, ok := trajectoryLimit(w, r, s)
	if !ok {
		return
	}
	rows, err := store.List(agent.TrajectoryQuery{Session: session, Limit: limit})
	if err != nil {
		writeJSON(s, w, 500, ErrorResponse{"trajectory listing failed"})
		return
	}
	owned := make([]agent.TrajectorySummary, 0, len(rows))
	caller := principal(r.Context())
	for _, row := range rows {
		if s.ownsTrajectory(caller, row) {
			owned = append(owned, row)
		}
	}
	writeJSON(s, w, 200, owned)
}
func (s *Server) ownedTrajectory(w http.ResponseWriter, r *http.Request, store agent.TrajectoryReader) (agent.TrajectoryID, bool) {
	id := agent.TrajectoryID(r.PathValue("trajectory_id"))
	row, err := store.Summary(id)
	if err != nil {
		writeJSON(s, w, 404, ErrorResponse{fmt.Sprintf("No trajectory '%s'", id)})
		return "", false
	}
	if !s.ownsTrajectory(principal(r.Context()), row) {
		writeJSON(s, w, 404, ErrorResponse{"No such trajectory"})
		return "", false
	}
	return id, true
}
func (s *Server) getTrajectory(w http.ResponseWriter, r *http.Request) {
	s.trajectoryDocument(w, r, false)
}
func (s *Server) exportTrajectory(w http.ResponseWriter, r *http.Request) {
	s.trajectoryDocument(w, r, true)
}
func (s *Server) trajectoryDocument(w http.ResponseWriter, r *http.Request, export bool) {
	store := s.trajectoryStore(w)
	if store == nil {
		return
	}
	format := "json"
	if export {
		if value, ok := r.URL.Query()["format"]; ok {
			format = value[0]
		}
		if format != "json" && format != "jsonl" {
			writeJSON(s, w, 400, ErrorResponse{"format must be 'json' or 'jsonl'"})
			return
		}
	}
	id, ok := s.ownedTrajectory(w, r, store)
	if !ok {
		return
	}
	if export && format == "jsonl" {
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s.jsonl\"", id))
		// Store streaming uses one chunk and holds no append lock while a client waits.
		if err := store.Stream(r.Context(), id, w); err != nil && r.Context().Err() == nil {
			panic(http.ErrAbortHandler)
		}
		return
	}
	size, err := store.ByteSize(id)
	if err != nil {
		writeJSON(s, w, 404, ErrorResponse{fmt.Sprintf("No trajectory '%s'", id)})
		return
	}
	if size > MaxTrajectoryJSONBytes {
		action := "render"
		tail := " Use the JSONL export, which streams."
		if export {
			action = "export"
			tail = " Use ?format=jsonl, which streams."
		}
		writeJSON(s, w, 413, ErrorResponse{fmt.Sprintf("trajectory is %s bytes; too large to %s as one JSON document (limit %s).%s", commaBytes(size), action, commaBytes(MaxTrajectoryJSONBytes), tail)})
		return
	}
	data, err := store.JSON(id, MaxTrajectoryJSONBytes)
	if err != nil {
		if err == trajectory.ErrTooLarge {
			writeJSON(s, w, 413, ErrorResponse{"trajectory grew beyond JSON limit; use the JSONL export"})
		} else {
			writeJSON(s, w, 404, ErrorResponse{fmt.Sprintf("No trajectory '%s'", id)})
		}
		return
	}
	data, err = recordingJSON(s, json.RawMessage(data))
	if err != nil {
		writeJSON(s, w, 500, ErrorResponse{"recording projection failed"})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if export {
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s.json\"", id))
	}
	w.Write(data)
}
func commaBytes(size int64) string {
	text := strconv.FormatInt(size, 10)
	for i := len(text) - 3; i > 0; i -= 3 {
		text = text[:i] + "," + text[i:]
	}
	return text
}
