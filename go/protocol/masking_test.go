package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRecordingProjectionRejectsExcessiveBytesAndNesting(t *testing.T) {
	mask := func(value string) string { return value }
	if _, err := MaskedPythonJSON(strings.Repeat("x", MaxProjectionBytes), mask, true, false); err == nil {
		t.Fatal("unbounded projection")
	}
	nested := strings.Repeat("[", maxProjectionDepth+1) + "0" + strings.Repeat("]", maxProjectionDepth+1)
	if _, err := MaskedPythonJSON(json.RawMessage(nested), mask, true, false); err == nil {
		t.Fatal("unbounded nesting")
	}
	if _, err := MaskedPythonJSON(json.RawMessage(`{"bad":`), mask, true, false); err == nil {
		t.Fatal("malformed JSON accepted")
	}
}

func TestRequestDiagnosticsKeepBoundedProjection(t *testing.T) {
	mask := func(value string) string { return value }
	nested := strings.Repeat("[", maxRequestDiagnosticProjectionDepth+1) + "0" + strings.Repeat("]", maxRequestDiagnosticProjectionDepth+1)
	if _, err := MaskedRequestDiagnosticJSON(json.RawMessage(nested), mask); err == nil {
		t.Fatal("unbounded request diagnostic nesting")
	}
	if _, err := MaskedRequestDiagnosticJSON(strings.Repeat("x", MaxProjectionBytes), mask); err == nil {
		t.Fatal("unbounded request diagnostic bytes")
	}
}
