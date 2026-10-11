package httpapi

import (
	"errors"
	"net/http"

	"github.com/luoyjx/mini-loop/go/selfaudit"
)

type SelfAuditSuggestionsResponse struct {
	Suggestions []selfaudit.Suggestion `json:"suggestions"`
}
type SelfAuditDraftsResponse struct {
	Drafts []selfaudit.BenchTaskDraft `json:"drafts"`
}

func (s *Server) selfAuditScope(r *http.Request) selfaudit.Scope {
	if !s.auth.Configured() {
		return selfaudit.Scope{IncludeGlobal: true}
	}
	owner := string(principal(r.Context()).ID)
	return selfaudit.Scope{Owner: &owner}
}

// These source GETs ignore bodies/query overrides and spend no rate budget.
func (s *Server) selfAuditReport(w http.ResponseWriter, r *http.Request) {
	scope := s.selfAuditScope(r)
	report := selfaudit.BuildReport(s.manager.ObserveSelfAudit(r.Context(), scope), scope)
	projected, err := s.selfAuditText(report)
	if err != nil {
		writeSelfAuditFailure(w)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte(projected))
}
func (s *Server) selfAuditSuggestions(w http.ResponseWriter, r *http.Request) {
	scope := s.selfAuditScope(r)
	suggestions, err := selfaudit.SuggestObjectives(s.manager.ObserveSelfAuditProblems(scope), scope.Owner, selfaudit.MaxSuggestions)
	if err != nil {
		writeSelfAuditFailure(w)
		return
	}
	writeJSON(s, w, http.StatusOK, SelfAuditSuggestionsResponse{Suggestions: suggestions})
}
func (s *Server) selfAuditDrafts(w http.ResponseWriter, r *http.Request) {
	scope := s.selfAuditScope(r)
	drafts, err := selfaudit.SuggestBenchTasks(s.manager.ObserveSelfAuditProblems(scope), scope.Owner, selfaudit.MaxSuggestions)
	if err != nil {
		writeSelfAuditFailure(w)
		return
	}
	writeJSON(s, w, http.StatusOK, SelfAuditDraftsResponse{Drafts: drafts})
}

// Plain text is projected before writing headers/body. A failed masker cannot
// fall back to the raw report; JSON routes use the existing typed projection.
func (s *Server) selfAuditText(report string) (projected string, err error) {
	defer func() {
		if recover() != nil {
			projected = ""
			err = errors.New("self-audit projection failed")
		}
	}()
	projected = report
	if masker := s.manager.RecordingMasker(); masker != nil {
		projected = masker.MaskText(report)
	}
	// Mask substitutions may expand text. Reapply the source character cap and
	// marker after projection; the operation is idempotent on an already capped report.
	characters := []rune(projected)
	if len(characters) > selfaudit.MaxReportCharacters {
		projected = string(characters[:selfaudit.MaxReportCharacters]) + "\n[report truncated at the cap]"
	}
	return projected, nil
}

func writeSelfAuditFailure(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusInternalServerError)
	w.Write([]byte("Internal Server Error"))
}
