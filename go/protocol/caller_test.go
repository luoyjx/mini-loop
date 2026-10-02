package protocol

import (
	"encoding/json"
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
		`{"type":"direct","tool_id":"unexpected"}`,
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
