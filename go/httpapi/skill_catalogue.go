package httpapi

import (
	"net/http"

	"github.com/luoyjx/mini-loop/go/agent"
)

type SkillCatalogueResponse struct {
	Session   agent.SessionID `json:"session"`
	Catalogue string          `json:"catalogue"`
}

func (s *Server) skillCatalogue(w http.ResponseWriter, r *http.Request) {
	session, ok := s.require(w, r)
	if !ok {
		return
	}
	catalogue, err := session.SkillCatalogue()
	if err != nil {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(500)
		w.Write([]byte("Internal Server Error"))
		return
	}
	writeJSON(s, w, 200, SkillCatalogueResponse{Session: session.ID(), Catalogue: catalogue})
}
