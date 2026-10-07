package httpapi

import (
	"net/http"
	"strings"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/internal/pytext"
	"github.com/luoyjx/mini-loop/go/memory"
)

type MemorySummary struct {
	Name        string        `json:"name"`
	Type        memory.Type   `json:"type"`
	Description string        `json:"description"`
	Origin      memory.Origin `json:"origin"`
}
type MemoryListResponse struct {
	Session  agent.SessionID `json:"session"`
	Memories []MemorySummary `json:"memories"`
}
type MemoryBodyResponse struct {
	Session     agent.SessionID `json:"session"`
	Name        string          `json:"name"`
	Type        memory.Type     `json:"type"`
	Description string          `json:"description"`
	Body        string          `json:"body"`
}

func (s *Server) memoryList(w http.ResponseWriter, r *http.Request) {
	session, ok := s.require(w, r)
	if !ok {
		return
	}
	records, err := session.MemoryRecords(r.Context())
	if err != nil {
		writeRequestDiagnosticFailure(w)
		return
	}
	items := make([]MemorySummary, 0, len(records))
	for _, record := range records {
		items = append(items, MemorySummary{Name: record.Name, Type: record.Type, Description: record.Description, Origin: record.Origin})
	}
	writeJSON(s, w, 200, MemoryListResponse{Session: session.ID(), Memories: items})
}

func (s *Server) memoryBody(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	// Starlette routes against the decoded path and cannot match an encoded
	// slash within this single segment; ServeMux otherwise would match it.
	if strings.Contains(name, "/") {
		writeJSON(s, w, 404, ErrorResponse{"Not Found"})
		return
	}
	session, ok := s.require(w, r)
	if !ok {
		return
	}
	records, err := session.MemoryRecords(r.Context())
	if err != nil {
		writeRequestDiagnosticFailure(w)
		return
	}
	for _, record := range records {
		if record.Name == name {
			writeJSON(s, w, 200, MemoryBodyResponse{Session: session.ID(), Name: name, Type: record.Type, Description: record.Description, Body: record.Body})
			return
		}
	}
	writeJSON(s, w, 404, ErrorResponse{"No memory " + pytext.Repr(name)})
}
