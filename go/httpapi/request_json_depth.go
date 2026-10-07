package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/luoyjx/mini-loop/go/protocol"
)

// Pinned CPython 3.11 / default Uvicorn HTTP stack with recursion limit 1000.
// Parsing and framework diagnostic encoding exhaust different call budgets.
// TestClient has a different stack; its incidental thresholds are not used here.
const requestMaxContainerDepth = 985
const requestMaxDiagnosticDepth = 978

func (v ValidationInput) diagnosticDepth() int {
	if v.kind != validationArray && v.kind != validationObject {
		return 0
	}
	depth := 0
	for _, item := range v.items {
		depth = max(depth, item.diagnosticDepth())
	}
	for _, member := range v.members {
		depth = max(depth, member.value.diagnosticDepth())
	}
	return depth + 1
}

func writeRequestDiagnosticFailure(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(500)
	w.Write([]byte("Internal Server Error"))
}

// Only retained values actually echoed in validation details consume the source
// response budget. Deep discarded duplicate values cannot poison admission.
func writeRequestValidation(s *Server, w http.ResponseWriter, value RequestValidationResponse) {
	for _, detail := range value.Detail {
		if detail.Input.diagnosticDepth() > requestMaxDiagnosticDepth {
			writeRequestDiagnosticFailure(w)
			return
		}
	}
	data, err := requestValidationJSON(s, value)
	if err != nil {
		writeJSON(s, w, 500, ErrorResponse{"response encoding failed"})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(422)
	w.Write(data)
}

func requestValidationJSON(s *Server, value RequestValidationResponse) (data []byte, err error) {
	defer func() {
		if recover() != nil {
			data = nil
			err = errors.New("request diagnostic projection failed")
		}
	}()
	masker := s.manager.RecordingMasker()
	if masker == nil {
		return json.Marshal(value)
	}
	projected, err := protocol.MaskedRequestDiagnosticJSON(value, masker.MaskText)
	return []byte(projected), err
}
