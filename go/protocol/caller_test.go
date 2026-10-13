package protocol

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
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
	for _, kind := range []CallerKind{CallerCodeExecution2025, CallerCodeExecution2026} {
		if _, err := CodeExecutionToolCaller(kind, ""); err == nil {
			t.Fatal("explicit constructor admitted an empty tool ID", kind)
		}
	}
	for _, body := range []string{
		`{}`,
		`{"type":""}`,
		`{"type":null}`,
		`{"type":"unknown"}`,
		`null`,
	} {
		var caller ToolCaller
		if err := json.Unmarshal([]byte(body), &caller); err == nil {
			t.Errorf("accepted caller %s", body)
		}
	}
}

func TestCallerFiniteToolIDObservations(t *testing.T) {
	for _, kind := range []CallerKind{CallerCodeExecution2025, CallerCodeExecution2026} {
		for _, test := range []struct {
			name, field, normalized, text string
			textPresent                   bool
		}{
			{name: "missing", normalized: `null`},
			{name: "null", field: `,"tool_id":null`, normalized: `null`},
			{name: "empty", field: `,"tool_id":""`, normalized: `""`, textPresent: true},
			{name: "text", field: `,"tool_id":"parent"`, normalized: `"parent"`, text: "parent", textPresent: true},
			{name: "integer", field: `,"tool_id":513`, normalized: `513`},
			{name: "boolean", field: `,"tool_id":true`, normalized: `true`},
			{name: "array", field: `,"tool_id":[null,"secret-value",513]`, normalized: `[null,"secret-value",513]`},
			{name: "object", field: `,"tool_id":{"secret-key":"secret-value"}`, normalized: `{"secret-key":"secret-value"}`},
		} {
			t.Run(string(kind)+"/"+test.name, func(t *testing.T) {
				var caller ToolCaller
				if err := json.Unmarshal([]byte(`{"type":"`+string(kind)+`"`+test.field+`}`), &caller); err != nil {
					t.Fatal(err)
				}
				if err := caller.Validate(); err != nil {
					t.Fatal("observed caller failed structural validation", err)
				}
				if caller.Kind() != kind || caller.ToolID() != test.text {
					t.Fatal("caller kind or text projection changed", caller.Kind(), caller.ToolID())
				}
				metadata := caller.ToolIDMetadata()
				if text, present := metadata.Text(); present != test.textPresent || (present && text != test.text) {
					t.Fatal("metadata text projection changed", text, present)
				}
				observed, err := json.Marshal(metadata)
				if err != nil || string(observed) != test.normalized {
					t.Fatal("observed ID changed", string(observed), err)
				}
				encoded, err := json.Marshal(caller)
				want := `{"type":"` + string(kind) + `","tool_id":` + test.normalized + `}`
				if err != nil || string(encoded) != want {
					t.Fatal("caller JSON changed", string(encoded), err)
				}
			})
		}
	}
}

func TestCallerObservedToolIDDetachesAndMasks(t *testing.T) {
	for _, id := range []string{`[null,"secret-value",513]`, `{"secret-key":["secret-value",513]}`} {
		var caller ToolCaller
		body := `{"type":"code_execution_20250825","tool_id":` + id + `}`
		if err := json.Unmarshal([]byte(body), &caller); err != nil {
			t.Fatal(err)
		}
		metadata := caller.ToolIDMetadata()
		value := metadata.Observation()
		if items, ok := value.Array(); ok {
			if len(items) != 3 {
				t.Fatal("array observation changed", items)
			}
			items[1] = jsonvalue.TextValue("mutated")
		} else {
			keys := value.Keys()
			if len(keys) != 1 || keys[0] != "secret-key" {
				t.Fatal("object observation changed", keys)
			}
			keys[0] = "mutated"
			nested, _ := value.Lookup("secret-key")
			items, ok := nested.Array()
			if !ok || len(items) != 2 {
				t.Fatal("nested observation changed", nested)
			}
			items[0] = jsonvalue.TextValue("mutated")
		}
		if err := json.Unmarshal([]byte(`"replacement"`), &metadata); err != nil {
			t.Fatal(err)
		}
		block := NewToolUseWithCaller("client", BashToolInput(BashInput{Command: "true"}), &caller)
		clone := BlockContent(block).Clone()
		masked, err := MaskedPythonJSON(clone, func(s string) string { return strings.ReplaceAll(s, "secret", "masked") }, false, true)
		if err != nil || strings.Contains(masked, "secret") || !strings.Contains(masked, "masked-value") {
			t.Fatal("observed ID was not masked", masked, err)
		}
		blocks, _ := clone.Blocks()
		use, _ := blocks[0].ToolUse()
		if use.ID != "client" || use.Name != "bash" || use.Caller == nil || use.Caller.ToolID() != "" {
			t.Fatal("observed caller changed execution identity", use)
		}
		for _, snapshot := range []*ToolCaller{&caller, use.Caller} {
			encoded, err := json.Marshal(snapshot)
			if err != nil || string(encoded) != body {
				t.Fatal("metadata access or masking mutated caller", string(encoded), err)
			}
		}
	}
}

func TestCallerInvalidObservedIDDecodeIsAtomic(t *testing.T) {
	const body = `{"type":"code_execution_20260120","tool_id":{"secret-key":[null,"secret-value",513]}}`
	var caller ToolCaller
	if err := json.Unmarshal([]byte(body), &caller); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{
		`{"type":"unknown","tool_id":null}`,
		`{"type":null,"tool_id":513}`,
		`{"type":"code_execution_20250825","tool_id":NaN}`,
		`{"type":"code_execution_20250825","tool_id":1e999}`,
		`{"type":"code_execution_20250825","tool_id":"\ud800"}`,
		`[]`,
	} {
		if err := json.Unmarshal([]byte(invalid), &caller); err == nil {
			t.Fatal("invalid observation admitted", invalid)
		}
		encoded, err := json.Marshal(caller)
		if err != nil || string(encoded) != body {
			t.Fatal("failed observed decode changed receiver", string(encoded), err)
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
	for _, invalid := range []string{`{"type":"unknown","tool_id":null}`, `{"type":7,"tool_id":7}`, `{"type":null}`, `[]`} {
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

func TestStoredCallerObservedIDUsesHistoricalBounds(t *testing.T) {
	large := strings.Repeat("x", MaxWireBytes+1)
	caller := `{"type":"code_execution_20260120","tool_id":{"large":"` + large + `"}}`
	for _, body := range []string{
		`{"type":"tool_use","id":"client","name":"bash","input":{"command":"true"},"caller":` + caller + `}`,
		`{"type":"server_tool_use","id":"server","name":"web_search","input":{},"caller":` + caller + `}`,
		`{"type":"web_search_tool_result","tool_use_id":"search","content":{"type":"web_search_tool_result_error","error_code":"unavailable"},"caller":` + caller + `}`,
		`{"type":"web_fetch_tool_result","tool_use_id":"fetch","content":{"type":"web_fetch_tool_result_error","error_code":"unavailable"},"caller":` + caller + `}`,
	} {
		raw := []byte(`{"role":"assistant","content":[` + body + `]}`)
		var network Message
		if err := json.Unmarshal(raw, &network); err == nil {
			t.Fatal("oversized observed ID bypassed network bound")
		}
		restored, err := DecodeStoredMessage(raw)
		if err != nil {
			t.Fatal("historical observed ID rejected", err)
		}
		encoded, err := json.Marshal(restored)
		if err != nil || !strings.Contains(string(encoded), `"tool_id":{"large":"`+large+`"}`) {
			t.Fatal("historical observed ID lost", err)
		}
	}
}
