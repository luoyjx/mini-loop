package httpapi

import (
	"errors"
	"math/big"
	"net/http"
	"net/url"
	"strings"

	"github.com/luoyjx/mini-loop/go/agent"
)

type epochQueryErrorKind string

const (
	epochIntegerParsing  epochQueryErrorKind = "int_parsing"
	epochIntegerTooLarge epochQueryErrorKind = "int_parsing_size"
)

type epochQueryIssue struct {
	Type     epochQueryErrorKind `json:"type"`
	Location [2]string           `json:"loc"`
	Message  string              `json:"msg"`
	Input    string              `json:"input"`
}
type epochQueryError struct {
	Detail []epochQueryIssue `json:"detail"`
}

// Parse at the HTTP boundary. Pydantic accepts ASCII decimal integers with
// underscores and a zero-only fractional suffix; it does not accept exponents
// or Unicode digits. Preserve large integers rather than overflowing Go int.
func transcriptSelection(query url.Values) (agent.TranscriptSelection, *epochQueryIssue) {
	values, present := query["epoch"]
	if !present {
		return agent.TranscriptSelection{}, nil
	}
	raw := values[len(values)-1] // Starlette QueryParams selects the last repeated value.
	invalid := func(kind epochQueryErrorKind) (agent.TranscriptSelection, *epochQueryIssue) {
		message := "Input should be a valid integer, unable to parse string as an integer"
		if kind == epochIntegerTooLarge {
			message = "Unable to parse input string as an integer, exceeded maximum size"
		}
		return agent.TranscriptSelection{}, &epochQueryIssue{Type: kind, Location: [2]string{"query", "epoch"}, Message: message, Input: raw}
	}
	value := strings.TrimSpace(raw)
	sign := ""
	if strings.HasPrefix(value, "+") || strings.HasPrefix(value, "-") {
		sign = value[:1]
		value = value[1:]
	}
	if dot := strings.IndexByte(value, '.'); dot >= 0 {
		fraction := value[dot+1:]
		if fraction == "" {
			return invalid(epochIntegerParsing)
		}
		for _, r := range fraction {
			if r != '0' {
				return invalid(epochIntegerParsing)
			}
		}
		value = value[:dot]
	}
	var digits strings.Builder
	digitBefore := false
	for _, r := range value {
		if r == '_' {
			if !digitBefore {
				return invalid(epochIntegerParsing)
			}
			digitBefore = false
			continue
		}
		if r < '0' || r > '9' {
			return invalid(epochIntegerParsing)
		}
		digitBefore = true
		digits.WriteByte(byte(r))
	}
	if !digitBefore {
		return invalid(epochIntegerParsing)
	}
	decimal := strings.TrimLeft(digits.String(), "0")
	if len(decimal) > 4300 {
		return invalid(epochIntegerTooLarge)
	}
	if decimal == "" {
		decimal = "0"
	}
	number, ok := new(big.Int).SetString(sign+decimal, 10)
	if !ok {
		return invalid(epochIntegerParsing)
	}
	return agent.SelectTranscriptEpochNumber(number), nil
}

func (s *Server) transcript(w http.ResponseWriter, r *http.Request) {
	selection, issue := transcriptSelection(r.URL.Query())
	if issue != nil {
		writeJSON(s, w, 422, epochQueryError{Detail: []epochQueryIssue{*issue}})
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
