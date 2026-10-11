package protocol

import (
	"encoding/json"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"strings"
	"testing"
)

func TestTypedCallerVariantsAndToolUseRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		body   string
		kind   CallerKind
		toolID string
	}{
		{`{"type":"direct"}`, CallerDirect, ""},
		{`{"type":"code_execution_20250825","tool_id":"srv-1"}`, CallerCodeExecution2025, "srv-1"},
		{`{"type":"code_execution_20260120","tool_id":"srv-2"}`, CallerCodeExecution2026, "srv-2"},
	} {
		var caller ToolCaller
		if err := json.Unmarshal([]byte(tc.body), &caller); err != nil {
			t.Fatal(err)
		}
		if caller.Kind() != tc.kind || caller.ToolID() != tc.toolID {
			t.Fatalf("caller decoded incorrectly: %+v", caller)
		}
		block := NewToolUseWithCaller("u1", BashToolInput(BashInput{Command: "echo hi"}), &caller)
		encoded, err := json.Marshal(block)
		if err != nil {
			t.Fatal(err)
		}
		var restored Block
		if err := json.Unmarshal(encoded, &restored); err != nil {
			t.Fatal(err)
		}
		use, _ := restored.ToolUse()
		if use.Caller == nil || use.Caller.Kind() != tc.kind || use.Caller.ToolID() != tc.toolID {
			t.Fatalf("caller lost in tool_use roundtrip: %+v", use)
		}
	}
}

func TestCallerRejectsUnknownOrIncompleteVariants(t *testing.T) {
	if _, err := CodeExecutionToolCaller(CallerDirect, ""); err == nil {
		t.Fatal("code execution constructor admitted a direct caller")
	}
	for _, body := range []string{
		`{"type":"code_execution_20250825"}`,
		`{"type":"code_execution_20260120","tool_id":""}`,
		`{"type":"unknown"}`,
		`null`,
	} {
		var caller ToolCaller
		if err := json.Unmarshal([]byte(body), &caller); err == nil {
			t.Errorf("accepted caller %s", body)
		}
	}
}

func TestToolUseCallerAccessorDetachesPointer(t *testing.T) {
	caller := DirectToolCaller()
	block := NewToolUseWithCaller("u1", BashToolInput(BashInput{Command: "echo hi"}), &caller)
	first, _ := block.ToolUse()
	first.Caller.kind = CallerCodeExecution2025
	second, _ := block.ToolUse()
	if second.Caller.Kind() != CallerDirect {
		t.Fatal("caller mutation escaped through a tool_use accessor")
	}
}

func TestCallerExtensionsRemainMetadataAndMaskWithoutMutation(t *testing.T) {
	body := `{"type":"direct","tool_id":"opaque metadata","secret-key":{"value":"secret-value","scalar":null}}`
	var caller ToolCaller
	if err := json.Unmarshal([]byte(body), &caller); err != nil {
		t.Fatal(err)
	}
	if caller.Kind() != CallerDirect || caller.ToolID() != "" {
		t.Fatal("extension gained typed caller authority")
	}
	encoded, err := json.Marshal(caller)
	if err != nil {
		t.Fatal(err)
	}
	before, err := jsonvalue.Decode(string(encoded))
	expected, _ := jsonvalue.Decode(body)
	if err != nil || !before.PythonEqual(expected) {
		t.Fatal("caller metadata lost", string(encoded))
	}
	block := NewToolUseWithCaller("client", BashToolInput(BashInput{Command: "true"}), &caller)
	clone := BlockContent(block).Clone()
	masked, err := MaskedPythonJSON(clone, func(s string) string { return strings.ReplaceAll(s, "secret", "masked") }, false, true)
	if err != nil || strings.Contains(masked, "secret") || !strings.Contains(masked, "masked-key") || !strings.Contains(masked, "masked-value") {
		t.Fatal("caller metadata not masked", masked, err)
	}
	first, _ := block.ToolUse()
	*first.Caller = DirectToolCaller()
	second, _ := block.ToolUse()
	unchanged, _ := json.Marshal(second.Caller)
	if string(unchanged) != string(encoded) {
		t.Fatal("masked/accessor copy mutated authoritative caller")
	}
	for _, invalid := range []string{`{"type":"code_execution_20250825","tool_id":null}`, `{"type":"code_execution_20260120","tool_id":7}`, `{"type":null}`, `[]`} {
		if err := json.Unmarshal([]byte(invalid), &caller); err == nil {
			t.Fatal("invalid caller admitted", invalid)
		}
		restored, _ := json.Marshal(caller)
		if string(restored) != string(encoded) {
			t.Fatal("failed caller decode changed receiver")
		}
	}
}

func TestStoredCallerExtensionsUseHistoricalBounds(t *testing.T) {
	caller := `{"type":"code_execution_20260120","tool_id":"parent","future":"` + strings.Repeat("x", MaxWireBytes+1) + `"}`
	for _, body := range []string{
		`{"type":"tool_use","id":"client","name":"bash","input":{"command":"true"},"caller":` + caller + `}`,
		`{"type":"server_tool_use","id":"server","name":"web_search","input":{},"caller":` + caller + `}`,
		`{"type":"web_search_tool_result","tool_use_id":"search","content":{"type":"web_search_tool_result_error","error_code":"unavailable"},"caller":` + caller + `}`,
		`{"type":"web_fetch_tool_result","tool_use_id":"fetch","content":{"type":"web_fetch_tool_result_error","error_code":"unavailable"},"caller":` + caller + `}`,
	} {
		raw := []byte(`{"role":"assistant","content":[` + body + `]}`)
		var network Message
		if err := json.Unmarshal(raw, &network); err == nil {
			t.Fatal("historical caller bypassed network bound")
		}
		restored, err := DecodeStoredMessage(raw)
		if err != nil {
			t.Fatal("historical caller rejected", err)
		}
		encoded, err := json.Marshal(restored)
		if err != nil || !strings.Contains(string(encoded), strings.Repeat("x", MaxWireBytes+1)) {
			t.Fatal("historical caller extension lost", err)
		}
	}
}
