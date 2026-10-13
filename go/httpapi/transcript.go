package httpapi

import (
	"errors"
	"net/http"
	"net/url"

	"github.com/luoyjx/mini-loop/go/agent"
)

func transcriptSelection(query url.Values) (agent.TranscriptSelection, *scalarQueryIssue) {
	value := selectScalarQuery(query, "epoch")
	if !value.Present {
		return agent.TranscriptSelection{}, nil
	}
	number, issue := scalarQueryInteger("epoch", value.Text)
	if issue != nil {
		return agent.TranscriptSelection{}, issue
	}
	return agent.SelectTranscriptEpochNumber(number), nil
}

func (s *Server) transcript(w http.ResponseWriter, r *http.Request) {
	selection, issue := transcriptSelection(r.URL.Query())
	if issue != nil {
		writeJSON(s, w, 422, scalarQueryError{Detail: []scalarQueryIssue{*issue}})
		return
	}
	session, ok := s.require(w, r)
	if !ok {
		return
	}
	transcript, err := session.ReadTranscript(r.Context(), selection)
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		var missing *agent.TranscriptEpochNotFound
		if errors.As(err, &missing) {
			writeJSON(s, w, 404, ErrorResponse{missing.Error()})
		} else {
			writeJSON(s, w, 503, ErrorResponse{"stored transcript read failed"})
		}
		return
	}
	writeJSON(s, w, 200, transcript)
}
