package protocol

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

func TestSDKCallerDefaultsKeepUnknownRootsAndListFirstBranch(t *testing.T) {
	for _, row := range []struct{ raw, expected string }{
		{`{"tool_id":"parent"}`, `{"tool_id":"parent","type":null}`},
		{`{"type":"future","secret-key":"secret-value"}`, `{"type":"future","secret-key":"secret-value"}`},
		{`{"type":"code_execution_20250825"}`, `{"type":"code_execution_20250825","tool_id":null}`},
		{`[{},{"tool_id":7},{"type":"code_execution_20250825"}]`, `[{"type":null},{"tool_id":7,"type":null},{"type":"code_execution_20250825"}]`},
		{`[[{}],[{"type":"code_execution_20250825"}],{}]`, `[[{}],[{"type":"code_execution_20250825"}],{"type":null}]`},
		{`"metadata"`, `"metadata"`}, {`513`, `513`}, {`true`, `true`}, {`[]`, `[]`},
		{`{"type":NaN,"tool_id":Infinity}`, `{"type":NaN,"tool_id":Infinity}`},
	} {
		caller, err := DecodeSDKToolCaller([]byte(row.raw))
		if err != nil {
			t.Fatal(row.raw, err)
		}
		observation, ok := caller.SDKObservation()
		if !ok {
			t.Fatal("SDK observation missing")
		}
		encoded, err := observation.LegacyJSON()
		if err != nil || string(encoded) != row.expected {
			t.Fatal(string(encoded), row.expected, err)
		}
		if err := caller.Validate(); err != nil {
			t.Fatal("valid SDK observation rejected", err)
		}
		// SDK construction is explicit. The declared public codec remains strict.
		var explicit ToolCaller
		if caller.Kind() == CallerObserved && json.Unmarshal([]byte(row.raw), &explicit) == nil {
			t.Fatal("observed root entered explicit declared admission", row.raw)
		}
	}
}

func TestSDKCallerNonfiniteMetadataSeparatesArchiveAndRequest(t *testing.T) {
	for _, raw := range []string{
		`{"type":"code_execution_20250825","tool_id":NaN}`,
		`{"type":"future","secret-key":{"secret-value":Infinity}}`,
		`-Infinity`,
	} {
		caller, err := DecodeSDKToolCaller([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		use, _ := NewBashUse("actual-use", "true").ToolUse()
		block := NewToolUseWithCaller("actual-use", use.Input, &caller)
		message := Message{Role: RoleAssistant, Content: BlockContent(block)}
		if err := message.Validate(); err != nil {
			t.Fatal("snapshot validation rejected metadata", err)
		}
		if _, err := json.Marshal(message); !errors.Is(err, jsonvalue.ErrNonfinite) {
			t.Fatal("finite request did not refuse legacy caller", err)
		}
		archive, err := message.MarshalArchiveJSON()
		if err != nil {
			t.Fatal(err)
		}
		stored, err := DecodeStoredMessage(archive)
		if err != nil {
			t.Fatal(err)
		}
		again, err := stored.MarshalArchiveJSON()
		if err != nil || string(again) != string(archive) {
			t.Fatal("archive caller changed", err)
		}
		observed, err := jsonvalue.Decode(string(archive))
		if err != nil {
			t.Fatal(err)
		}
		maskedData, err := observed.MapStrings(func(value string) string {
			return strings.ReplaceAll(value, "secret", "masked")
		}).MarshalLegacyUTF8()
		masked := string(maskedData)
		if err != nil || strings.Contains(masked, "secret") {
			t.Fatal("caller masking failed", masked, err)
		}
		if _, err := MaskedPythonJSON(message.Content, nil, false, true); !errors.Is(err, jsonvalue.ErrNonfinite) {
			t.Fatal("finite projection admitted nonfinite caller", err)
		}
		after, err := message.MarshalArchiveJSON()
		if err != nil || string(after) != string(archive) {
			t.Fatal("mask changed original", err)
		}
	}
}

func TestSDKCallerArchivePreservesSparseListAndHistoricalBounds(t *testing.T) {
	caller := `[{"type":"code_execution_20250825"},{"extra":"` + strings.Repeat("x", MaxWireBytes+1) + `"}]`
	raw := `{"role":"assistant","content":[{"type":"tool_use","id":"actual-use","name":"bash","input":{"command":"true"},"caller":` + caller + `}]}`
	var public Message
	if err := json.Unmarshal([]byte(raw), &public); err == nil {
		t.Fatal("oversized historical caller passed network admission")
	}
	stored, err := DecodeStoredMessage([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	archive, err := stored.MarshalArchiveJSON()
	if err != nil || !strings.Contains(string(archive), `"caller":`+caller) {
		t.Fatal("archive ran SDK defaults or reapplied wire cap", err)
	}
}

func TestSDKCallerAccessorDetachesRootAndRejectsInvalidUTF8(t *testing.T) {
	caller, err := DecodeSDKToolCaller([]byte(`[{},"secret"]`))
	if err != nil {
		t.Fatal(err)
	}
	observation, _ := caller.SDKObservation()
	items, _ := observation.Value().Array()
	items[0] = jsonvalue.TextValue("replacement")
	encoded, err := caller.observationValue()
	if err != nil {
		t.Fatal(err)
	}
	data, err := encoded.MarshalLegacyUTF8()
	if err != nil || string(data) != `[{"type":null},"secret"]` {
		t.Fatal("accessor mutated caller", string(data), err)
	}
	for _, invalid := range []string{`"\ud800"`, `{"type":"direct","x":"\ud800"}`} {
		if _, err := DecodeSDKToolCaller([]byte(invalid)); err == nil {
			t.Fatal("invalid UTF-8 observation admitted")
		}
	}
}
