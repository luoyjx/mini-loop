package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBashExecutionCloneMaskDefaultsAndHistoricalOutput(t *testing.T) {
	raw := `{"type":"bash_code_execution_tool_result","tool_use_id":"SECRET","content":{"type":"bash_code_execution_result","content":[{"type":"bash_code_execution_output","file_id":"SECRET","SECRET":{"file":"SECRET"}}],"return_code":1208925819614629174706193,"stdout":"SECRET","stderr":"SECRET","SECRET":"SECRET"},"SECRET":"SECRET"}`
	var block Block
	if err := json.Unmarshal([]byte(raw), &block); err != nil {
		t.Fatal(err)
	}
	value, _ := block.BashExecutionToolResult()
	result, _ := value.Content.Result()
	if result.ReturnCode.Decimal() != "1208925819614629174706193" {
		t.Fatal("large return code truncated")
	}
	result.OutputFiles[0].FileID = "changed"
	clone := BlockContent(block).Clone()
	clone.blocks[0].bashExecution.Content.result.OutputFiles[0].FileID = "clone"
	original, _ := block.BashExecutionToolResult()
	untouched, _ := original.Content.Result()
	if untouched.OutputFiles[0].FileID != "SECRET" {
		t.Fatal("output file clone/accessor aliases original")
	}
	mask := func(s string) string { return strings.ReplaceAll(s, "SECRET", "[masked]") }
	masked, err := MaskedPythonJSON(Message{Role: RoleAssistant, Content: BlockContent(block)}, mask, false, true)
	if err != nil || strings.Contains(masked, "SECRET") {
		t.Fatal("execution metadata escaped mask", err)
	}
	restored, err := DecodeStoredMessage([]byte(masked))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(restored)
	if err != nil || !strings.Contains(string(encoded), `"[masked]":{"file":"[masked]"}`) || !strings.Contains(string(encoded), `"return_code":1208925819614629174706193`) {
		t.Fatal("execution metadata lost", string(encoded), err)
	}
	if err := ValidateTranscript([]Message{{Role: RoleAssistant, Content: BlockContent(block)}}); err != nil {
		t.Fatal("execution result required client-tool pairing", err)
	}
	value, _ = block.BashExecutionToolResult()
	encoded, _ = json.Marshal(value.WithSDKDefaults(true))
	for _, field := range []string{`"text":null`, `"citations":null`, `"parsed_output":null`} {
		if !strings.Contains(string(encoded), field) {
			t.Fatal("stream null default lost", field)
		}
	}
	encoded, _ = json.Marshal(value)
	if strings.Contains(string(encoded), `"text"`) {
		t.Fatal("sparse block was defaulted")
	}
	large := NewBashExecutionToolResultBlock("id", NewBashExecutionSuccess(BashExecutionResult{ReturnCode: NewExecutionReturnCode(0), Stdout: strings.Repeat("x", MaxWireBytes+1)}))
	message, err := json.Marshal(Message{Role: RoleAssistant, Content: BlockContent(large)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeStoredMessage(message); err != nil {
		t.Fatal("historical stdout bounded", err)
	}
	var network Message
	if err := json.Unmarshal(message, &network); err == nil {
		t.Fatal("oversized network frame admitted")
	}
}

func TestBashExecutionNestedAtomicFailureAndDistinctLabels(t *testing.T) {
	block := NewBashExecutionToolResultBlock("id", NewBashExecutionFailure(BashExecutionUnavailable))
	original, _ := json.Marshal(block)
	for _, content := range []string{
		`{"type":"bash_code_execution_result","content":[],"return_code":0,"stderr":""}`,
		`{"type":"bash_code_execution_result","content":null,"return_code":0,"stdout":"","stderr":""}`,
		`{"type":"bash_code_execution_result","content":[{"type":"bash_code_execution_output"}],"return_code":0,"stdout":"","stderr":""}`,
		`{"type":"bash_code_execution_result","content":[],"return_code":"0","stdout":"","stderr":""}`,
		`{"type":"bash_code_execution_result","content":[],"return_code":1.5,"stdout":"","stderr":""}`,
		`{"type":"code_execution_result","content":[],"return_code":0,"stdout":"","stderr":""}`,
		`{"type":"bash_code_execution_result","content":[{"type":"code_execution_output","file_id":"id"}],"return_code":0,"stdout":"","stderr":""}`,
	} {
		raw := `{"type":"bash_code_execution_tool_result","tool_use_id":"id","content":` + content + `}`
		if err := json.Unmarshal([]byte(raw), &block); err == nil {
			t.Fatal("invalid/different execution metadata admitted", content)
		}
		encoded, _ := json.Marshal(block)
		if string(encoded) != string(original) {
			t.Fatal("failed nested decode mutated receiver")
		}
	}
	value, _ := block.BashExecutionToolResult()
	value.Content.result = &BashExecutionResult{}
	if err := value.Validate(); err == nil {
		t.Fatal("mixed result/error content admitted")
	}
	block.toolUse = &ToolUseBlock{}
	if err := block.Validate(); err == nil {
		t.Fatal("mixed bash server/client variant admitted")
	}
}
