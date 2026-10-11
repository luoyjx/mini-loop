package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestServerToolUseMaskCloneSparseAndHistoricalInput(t *testing.T) {
	raw := `{"type":"server_tool_use","id":"SECRET","name":"future_server_tool","input":{"SECRET":{"array":[null,true,3,1.5,"SECRET"]}},"caller":{"type":"code_execution_20260120","tool_id":"SECRET"},"SECRET":{"value":"SECRET"}}`
	var block Block
	if err := json.Unmarshal([]byte(raw), &block); err != nil {
		t.Fatal(err)
	}
	value, ok := block.ServerToolUse()
	if !ok {
		t.Fatal("server tool variant missing")
	}
	value.Caller = nil
	value.ID = "changed"
	clone := BlockContent(block).Clone()
	clone.blocks[0].serverTool.Caller = nil
	original, _ := block.ServerToolUse()
	if original.Caller == nil || original.ID != "SECRET" {
		t.Fatal("server tool clone/accessor aliases original")
	}
	if _, ok := block.ToolUse(); ok {
		t.Fatal("server tool entered client dispatch")
	}
	if err := ValidateTranscript([]Message{{Role: RoleAssistant, Content: BlockContent(block)}}); err != nil {
		t.Fatal("server tool requires a client result", err)
	}
	mask := func(s string) string { return strings.ReplaceAll(s, "SECRET", "[masked]") }
	masked, err := MaskedPythonJSON(Message{Role: RoleAssistant, Content: BlockContent(block)}, mask, false, true)
	if err != nil || strings.Contains(masked, "SECRET") {
		t.Fatal("server input/caller/extension escaped mask", err)
	}
	restored, err := DecodeStoredMessage([]byte(masked))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(restored)
	if err != nil || !strings.Contains(string(encoded), `"[masked]":{"value":"[masked]"}`) {
		t.Fatal("server metadata lost", string(encoded), err)
	}
	sparse := `{"type":"server_tool_use","id":"id","name":"web_search","input":{}}`
	var r ServerToolUseBlock
	if err := json.Unmarshal([]byte(sparse), &r); err != nil {
		t.Fatal(err)
	}
	encoded, _ = json.Marshal(r)
	if strings.Contains(string(encoded), "caller") {
		t.Fatal("sparse caller was defaulted")
	}
	encoded, _ = json.Marshal(r.WithSDKDefaults())
	if !strings.Contains(string(encoded), `"caller":null`) {
		t.Fatal("SDK caller default lost")
	}
	large := `{"role":"assistant","content":[{"type":"server_tool_use","id":"id","name":"web_search","input":{"query":"` + strings.Repeat("x", MaxWireBytes+1) + `"}}]}`
	restored, err = DecodeStoredMessage([]byte(large))
	if err != nil {
		t.Fatal("historical server input was bounded", err)
	}
	encoded, err = json.Marshal(restored)
	if err != nil || len(encoded) < MaxWireBytes {
		t.Fatal("historical server input truncated", err)
	}
	var network Message
	if err := json.Unmarshal([]byte(large), &network); err == nil {
		t.Fatal("oversized network message admitted")
	}
}

func TestServerToolInputAtomicFailureAndMixedVariants(t *testing.T) {
	input, err := DecodeServerToolInput([]byte(`{"query":"initial"}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`{"query":`, `NaN`, `{"query":Infinity}`} {
		if err := json.Unmarshal([]byte(raw), &input); err == nil {
			t.Fatal("malformed/nonfinite input admitted", raw)
		}
		encoded, _ := json.Marshal(input)
		if string(encoded) != `{"query":"initial"}` {
			t.Fatal("failed input decode mutated receiver")
		}
	}
	block := NewServerToolUseBlock("id", ServerWebSearch, input)
	original, _ := json.Marshal(block)
	if err := json.Unmarshal([]byte(`{"type":"server_tool_use","id":"id","name":"web_search","input":NaN}`), &block); err == nil {
		t.Fatal("invalid input admitted")
	}
	encoded, _ := json.Marshal(block)
	if string(encoded) != string(original) {
		t.Fatal("failed block decode mutated receiver")
	}
	block.toolUse = &ToolUseBlock{}
	if err := block.Validate(); err == nil {
		t.Fatal("mixed server/client variants admitted")
	}
	if data, err := json.Marshal(ServerToolInput{}); err != nil || string(data) != "null" {
		t.Fatal("zero observational input changed", err)
	}
}
