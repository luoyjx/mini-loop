package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestToolSearchCloneMaskExtensionsAndHistoricalBounds(t *testing.T) {
	raw := `{"type":"tool_search_tool_result","tool_use_id":"server","content":{"type":"tool_search_tool_search_result","tool_references":[{"type":"tool_reference","tool_name":"SECRET","SECRET":{"nested":"SECRET"}}],"SECRET":8},"SECRET":{"value":"SECRET"}}`
	var block Block
	if err := json.Unmarshal([]byte(raw), &block); err != nil {
		t.Fatal(err)
	}
	accessor, ok := block.ToolSearchToolResult()
	if !ok {
		t.Fatal("named tool search missing")
	}
	result, ok := accessor.Content.Results()
	if !ok || len(result.References) != 1 {
		t.Fatal("references missing")
	}
	result.References[0].Name = "changed"
	copy := BlockContent(block).Clone()
	copy.blocks[0].toolSearch.Content.results.References[0].Name = "changed"
	original, _ := block.ToolSearchToolResult()
	untouched, _ := original.Content.Results()
	if untouched.References[0].Name != "SECRET" {
		t.Fatal("reference accessor/clone aliases original")
	}
	mask := func(value string) string { return strings.ReplaceAll(value, "SECRET", "[masked]") }
	masked, err := MaskedPythonJSON(Message{Role: RoleAssistant, Content: BlockContent(block)}, mask, false, true)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(masked, "SECRET") {
		t.Fatal("reference metadata escaped mask")
	}
	stored, err := DecodeStoredMessage([]byte(masked))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(stored)
	if err != nil || !strings.Contains(string(encoded), `"[masked]":{"value":"[masked]"}`) {
		t.Fatal("extension projection lost", string(encoded), err)
	}
	if err := ValidateTranscript([]Message{{Role: RoleAssistant, Content: BlockContent(block)}}); err != nil {
		t.Fatal("references required client-tool pairing", err)
	}
	huge := NewToolSearchToolResultBlock("server", NewToolSearchResults([]ToolReference{{Name: ToolName(strings.Repeat("x", MaxWireBytes+1))}}))
	message, err := json.Marshal(Message{Role: RoleAssistant, Content: BlockContent(huge)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeStoredMessage(message); err != nil {
		t.Fatal("historical reference truncated", err)
	}
	var ingress Message
	if err := json.Unmarshal(message, &ingress); err == nil {
		t.Fatal("unbounded network frame admitted")
	}
}
func TestToolSearchErrorNullDefaultsIsolationAndAtomicFailure(t *testing.T) {
	var result ToolSearchToolResultBlock
	if err := json.Unmarshal([]byte(`{"type":"tool_search_tool_result","tool_use_id":"server","content":{"type":"tool_search_tool_result_error","error_code":"future_code","future":17}}`), &result); err != nil {
		t.Fatal(err)
	}
	sparse, _ := json.Marshal(result)
	if strings.Contains(string(sparse), "error_message") {
		t.Fatal("sparse codec added null")
	}
	direct, _ := json.Marshal(result.WithSDKDefaults(false))
	streamed, _ := json.Marshal(result.WithSDKDefaults(true))
	if !strings.Contains(string(direct), `"error_message":null`) || strings.Contains(string(direct), `"text":null`) {
		t.Fatal("direct defaults changed")
	}
	if !strings.Contains(string(streamed), `"text":null`) || !strings.Contains(string(streamed), `"parsed_output":null`) {
		t.Fatal("stream defaults lost")
	}
	message := "detail"
	block := NewToolSearchToolResultBlock("server", NewToolSearchFailure(ToolSearchUnavailable, &message))
	accessor, _ := block.ToolSearchToolResult()
	failure, _ := accessor.Content.Error()
	*failure.Message = "changed"
	untouched, _ := block.ToolSearchToolResult()
	original, _ := untouched.Content.Error()
	if *original.Message != "detail" {
		t.Fatal("error accessor aliases original")
	}
	for _, raw := range []string{`null`, `3`, `{"type":"unknown"}`, `{"type":"tool_search_tool_search_result","tool_references":[{}]}`} {
		content := result.Content
		if err := json.Unmarshal([]byte(raw), &content); err == nil {
			t.Fatal("invalid content admitted", raw)
		}
		failure, ok := content.Error()
		if !ok || failure.Code != "future_code" {
			t.Fatal("atomic failure changed content")
		}
	}
	if (ToolSearchContent{kind: ToolSearchSuccess, results: &ToolSearchResults{}, failure: &ToolSearchError{}}).Validate() == nil {
		t.Fatal("mixed content admitted")
	}
	if (Block{kind: BlockText, text: &TextBlock{Text: "x"}, toolSearch: &ToolSearchToolResultBlock{Content: NewToolSearchResults(nil)}}).Validate() == nil {
		t.Fatal("mixed block admitted")
	}
}
