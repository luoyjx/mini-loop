package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestWebSearchResultsCloneMaskExtensionsAndHistoricalBounds(t *testing.T) {
	raw := `{"type":"web_search_tool_result","tool_use_id":"server","caller":null,"content":[{"type":"web_search_result","title":"SECRET","url":"https://fixture.invalid","encrypted_content":"SECRET","page_age":"SECRET","SECRET":{"nested":"SECRET"}}],"SECRET":{"value":"SECRET"}}`
	var block Block
	if err := json.Unmarshal([]byte(raw), &block); err != nil {
		t.Fatal(err)
	}
	accessor, ok := block.WebSearchToolResult()
	if !ok {
		t.Fatal("named web result missing")
	}
	rows, ok := accessor.Content.Results()
	if !ok || len(rows) != 1 {
		t.Fatal("results missing")
	}
	*rows[0].PageAge = "changed"
	original, _ := block.WebSearchToolResult()
	rows, _ = original.Content.Results()
	if *rows[0].PageAge != "SECRET" {
		t.Fatal("accessor aliases original")
	}
	copy := BlockContent(block).Clone()
	copy.blocks[0].webSearch.Content.results[0].Title = "changed"
	if block.webSearch.Content.results[0].Title != "SECRET" {
		t.Fatal("clone aliases original")
	}
	mask := func(value string) string { return strings.ReplaceAll(value, "SECRET", "[masked]") }
	masked, err := MaskedPythonJSON(Message{Role: RoleAssistant, Content: BlockContent(block)}, mask, false, true)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(masked, "SECRET") {
		t.Fatal("metadata secret escaped mask")
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
		t.Fatal("server result required client-tool pairing", err)
	}
	huge := NewWebSearchToolResultBlock("server", NewWebSearchResults([]WebSearchResult{{Title: "large", URL: "u", EncryptedContent: strings.Repeat("x", MaxWireBytes+1)}}), nil)
	message, err := json.Marshal(Message{Role: RoleAssistant, Content: BlockContent(huge)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeStoredMessage(message); err != nil {
		t.Fatal("historical metadata frame truncated", err)
	}
	var ingress Message
	if err := json.Unmarshal(message, &ingress); err == nil {
		t.Fatal("unbounded network frame admitted")
	}
}

func TestWebSearchContentClosedVariantsAndAtomicFailure(t *testing.T) {
	if err := NewWebSearchResults(nil).Validate(); err != nil {
		t.Fatal("empty results refused", err)
	}
	var content WebSearchContent
	if err := json.Unmarshal([]byte(`{"type":"web_search_tool_result_error","error_code":"future_code","future":7}`), &content); err != nil {
		t.Fatal(err)
	}
	failure, ok := content.Error()
	if !ok || failure.Code != "future_code" {
		t.Fatal("named future error lost")
	}
	for _, raw := range []string{`null`, `3`, `[{}]`, `{"type":"unknown","error_code":"bad"}`} {
		if err := json.Unmarshal([]byte(raw), &content); err == nil {
			t.Fatal("invalid content admitted", raw)
		}
		failure, ok = content.Error()
		if !ok || failure.Code != "future_code" {
			t.Fatal("atomic failure changed content")
		}
	}
	if (WebSearchContent{kind: WebSearchResults, results: []WebSearchResult{}, failure: &WebSearchError{}}).Validate() == nil {
		t.Fatal("mixed content admitted")
	}
	if (Block{kind: BlockText, text: &TextBlock{Text: "x"}, webSearch: &WebSearchToolResultBlock{Content: NewWebSearchResults(nil)}}).Validate() == nil {
		t.Fatal("mixed block admitted")
	}
}
