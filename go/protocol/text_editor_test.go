package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEditorNullValueEmptyArrayClonesAndHistoricalView(t *testing.T) {
	raw := `{"type":"text_editor_code_execution_tool_result","tool_use_id":"id","content":{"type":"text_editor_code_execution_str_replace_result","lines":["SECRET"],"new_lines":1208925819614629174706193,"new_start":null,"SECRET":{"value":"SECRET"}},"SECRET":"SECRET"}`
	var block Block
	if err := json.Unmarshal([]byte(raw), &block); err != nil {
		t.Fatal(err)
	}
	value, _ := block.TextEditorToolResult()
	result, _ := value.Content.Replace()
	if result.NewLines.Decimal() != "1208925819614629174706193" {
		t.Fatal("editor integer truncated")
	}
	(*result.Lines)[0] = "changed"
	*result.NewLines = NewEditorInteger(3)
	clone := BlockContent(block).Clone()
	(*clone.blocks[0].textEditor.Content.replace.Lines)[0] = "clone"
	original, _ := block.TextEditorToolResult()
	untouched, _ := original.Content.Replace()
	if (*untouched.Lines)[0] != "SECRET" || untouched.NewLines.Decimal() != "1208925819614629174706193" {
		t.Fatal("editor clone/accessor aliases data")
	}
	mask := func(s string) string { return strings.ReplaceAll(s, "SECRET", "[masked]") }
	masked, err := MaskedPythonJSON(Message{Role: RoleAssistant, Content: BlockContent(block)}, mask, false, true)
	if err != nil || strings.Contains(masked, "SECRET") {
		t.Fatal("editor metadata escaped mask", err)
	}
	if _, err := DecodeStoredMessage([]byte(masked)); err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(value)
	if !strings.Contains(string(encoded), `"new_start":null`) || strings.Contains(string(encoded), `"old_start"`) {
		t.Fatal("sparse/null distinction lost", string(encoded))
	}
	for _, raw := range []string{`{"type":"text_editor_code_execution_str_replace_result"}`, `{"type":"text_editor_code_execution_str_replace_result","lines":null}`, `{"type":"text_editor_code_execution_str_replace_result","lines":[]}`} {
		var r TextEditorReplaceResult
		if err := json.Unmarshal([]byte(raw), &r); err != nil {
			t.Fatal(err)
		}
		encoded, _ := json.Marshal(r)
		if string(encoded) != raw {
			t.Fatal("absent/null/empty array changed", raw, string(encoded))
		}
	}
	empty := []string{}
	normalized := NewTextEditorToolResultBlock("id", NewTextEditorReplace(TextEditorReplaceResult{Lines: &empty}))
	value, _ = normalized.TextEditorToolResult()
	encoded, _ = json.Marshal(value.WithSDKDefaults(true))
	for _, field := range []string{`"lines":[]`, `"new_start":null`, `"old_lines":null`, `"text":null`, `"citations":null`, `"parsed_output":null`} {
		if !strings.Contains(string(encoded), field) {
			t.Fatal("SDK default missing", field, string(encoded))
		}
	}
	number := NewEditorInteger(-7)
	view := NewTextEditorToolResultBlock("id", NewTextEditorView(TextEditorViewResult{Content: "text", FileType: EditorText, NumLines: &number}))
	value, _ = view.TextEditorToolResult()
	snapshot, _ := value.Content.View()
	*snapshot.NumLines = NewEditorInteger(99)
	untouchedView, _ := value.Content.View()
	if untouchedView.NumLines.Decimal() != "-7" {
		t.Fatal("view numeric accessor aliases")
	}
	message := NewTextEditorToolResultBlock("id", NewTextEditorView(TextEditorViewResult{Content: strings.Repeat("x", MaxWireBytes+1), FileType: EditorText}))
	encoded, err = json.Marshal(Message{Role: RoleAssistant, Content: BlockContent(message)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeStoredMessage(encoded); err != nil {
		t.Fatal("large historical view was bounded", err)
	}
	var network Message
	if err := json.Unmarshal(encoded, &network); err == nil {
		t.Fatal("oversized network frame admitted")
	}
	if err := ValidateTranscript([]Message{{Role: RoleAssistant, Content: BlockContent(block)}}); err != nil {
		t.Fatal("editor metadata required client pairing", err)
	}
}

func TestEditorNestedRefusalAndAtomicReceivers(t *testing.T) {
	code := NewEditorInteger(9)
	for _, raw := range []string{`null`, `1.5`, `true`, `"9"`, `[]`, `{}`} {
		if err := json.Unmarshal([]byte(raw), &code); err == nil {
			t.Fatal("noninteger editor value admitted", raw)
		}
		if code.Decimal() != "9" {
			t.Fatal("failed number decode mutated receiver")
		}
	}
	block := NewTextEditorToolResultBlock("id", NewTextEditorFailure(TextEditorUnavailable, nil))
	original, _ := json.Marshal(block)
	for _, content := range []string{
		`{"type":"text_editor_code_execution_view_result","file_type":"text"}`,
		`{"type":"text_editor_code_execution_view_result","file_type":"text","content":"x","num_lines":"3"}`,
		`{"type":"text_editor_code_execution_create_result"}`,
		`{"type":"text_editor_code_execution_create_result","is_file_update":"false"}`,
		`{"type":"text_editor_code_execution_str_replace_result","lines":[null]}`,
		`{"type":"text_editor_code_execution_str_replace_result","lines":[3]}`,
		`{"type":"text_editor_code_execution_str_replace_result","new_start":true}`,
		`{"type":"text_editor_code_execution_tool_result_error"}`,
	} {
		raw := `{"type":"text_editor_code_execution_tool_result","tool_use_id":"id","content":` + content + `}`
		if err := json.Unmarshal([]byte(raw), &block); err == nil {
			t.Fatal("invalid editor content admitted", content)
		}
		encoded, _ := json.Marshal(block)
		if string(encoded) != string(original) {
			t.Fatal("failed nested decode mutated receiver")
		}
	}
	value, _ := block.TextEditorToolResult()
	value.Content.create = &TextEditorCreateResult{}
	if err := value.Validate(); err == nil {
		t.Fatal("mixed editor content admitted")
	}
	block.toolUse = &ToolUseBlock{}
	if err := block.Validate(); err == nil {
		t.Fatal("mixed editor/client block admitted")
	}
}
