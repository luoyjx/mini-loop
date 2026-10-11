package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestWebFetchCloneMaskSparseDefaultsAndHistoricalDocument(t *testing.T) {
	raw := `{"type":"web_fetch_tool_result","tool_use_id":"SECRET","content":{"type":"web_fetch_result","url":"SECRET","retrieved_at":"SECRET","content":{"type":"document","title":"SECRET","source":{"type":"text","media_type":"text/plain","data":"SECRET","SECRET":{"source":"SECRET"}},"citations":{"enabled":true,"SECRET":"SECRET"},"SECRET":"SECRET"},"SECRET":"SECRET"},"caller":{"type":"code_execution_20250825","tool_id":"SECRET"},"SECRET":"SECRET"}`
	var block Block
	if err := json.Unmarshal([]byte(raw), &block); err != nil {
		t.Fatal(err)
	}
	value, _ := block.WebFetchToolResult()
	result, _ := value.Content.Result()
	*result.Document.Title = "changed"
	*result.RetrievedAt = "changed"
	result.Document.Citations.Enabled = false
	value.Caller = nil
	clone := BlockContent(block).Clone()
	*clone.blocks[0].webFetch.Content.result.Document.Title = "clone"
	original, _ := block.WebFetchToolResult()
	untouched, _ := original.Content.Result()
	if *untouched.Document.Title != "SECRET" || *untouched.RetrievedAt != "SECRET" || !untouched.Document.Citations.Enabled || original.Caller == nil {
		t.Fatal("web fetch accessor/clone aliases original")
	}
	mask := func(s string) string { return strings.ReplaceAll(s, "SECRET", "[masked]") }
	masked, err := MaskedPythonJSON(Message{Role: RoleAssistant, Content: BlockContent(block)}, mask, false, true)
	if err != nil || strings.Contains(masked, "SECRET") {
		t.Fatal("nested document metadata escaped mask", err)
	}
	restored, err := DecodeStoredMessage([]byte(masked))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(restored)
	if err != nil || !strings.Contains(string(encoded), `"[masked]":{"source":"[masked]"}`) {
		t.Fatal("masked extension lost", err)
	}
	if err := ValidateTranscript([]Message{{Role: RoleAssistant, Content: BlockContent(block)}}); err != nil {
		t.Fatal("web fetch required client result pairing", err)
	}
	sparse := NewWebFetchToolResultBlock("server", NewWebFetchSuccess(WebFetchResult{URL: "url", Document: FetchedDocument{Source: TextDocumentSource("text")}}))
	encoded, _ = json.Marshal(sparse)
	for _, field := range []string{`"title"`, `"citations"`, `"retrieved_at"`, `"caller"`} {
		if strings.Contains(string(encoded), field) {
			t.Fatal("sparse document was defaulted", string(encoded))
		}
	}
	value, _ = sparse.WebFetchToolResult()
	normalized := value.WithSDKDefaults(true)
	encoded, _ = json.Marshal(normalized)
	for _, field := range []string{`"title":null`, `"citations":null`, `"retrieved_at":null`, `"caller":null`, `"text":null`, `"parsed_output":null`} {
		if !strings.Contains(string(encoded), field) {
			t.Fatal("SDK nested/default field missing", field, string(encoded))
		}
	}
	historical := NewWebFetchToolResultBlock("server", NewWebFetchSuccess(WebFetchResult{URL: "url", Document: FetchedDocument{Source: PDFDocumentSource(strings.Repeat("x", MaxWireBytes+1))}}))
	message, err := json.Marshal(Message{Role: RoleAssistant, Content: BlockContent(historical)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeStoredMessage(message); err != nil {
		t.Fatal("historical PDF source was bounded", err)
	}
	var network Message
	if err := json.Unmarshal(message, &network); err == nil {
		t.Fatal("oversized network message admitted")
	}
}

func TestWebFetchNestedAtomicFailureAndClosedVariants(t *testing.T) {
	valid := NewWebFetchToolResultBlock("id", NewWebFetchSuccess(WebFetchResult{URL: "url", Document: FetchedDocument{Source: TextDocumentSource("initial")}}))
	original, _ := json.Marshal(valid)
	for _, content := range []string{
		`{"type":"web_fetch_result","url":"url","content":null}`,
		`{"type":"web_fetch_result","url":"url","content":{"type":"document"}}`,
		`{"type":"web_fetch_result","url":"url","content":{"type":"document","source":{"type":"text","data":"x"}}}`,
		`{"type":"web_fetch_result","url":"url","content":{"type":"document","source":{"type":"text","data":null,"media_type":"text/plain"}}}`,
		`{"type":"web_fetch_result","url":"url","content":{"type":"document","source":{"type":"text","data":"x","media_type":"text/plain"},"citations":{}}}`,
		`{"type":"web_fetch_result","url":"url","content":{"type":"document","source":{"type":"text","data":"x","media_type":"text/plain"},"citations":{"enabled":"true"}}}`,
		`{"type":"unknown"}`,
	} {
		raw := `{"type":"web_fetch_tool_result","tool_use_id":"id","content":` + content + `}`
		if err := json.Unmarshal([]byte(raw), &valid); err == nil {
			t.Fatal("invalid nested document admitted", content)
		}
		encoded, _ := json.Marshal(valid)
		if string(encoded) != string(original) {
			t.Fatal("failed nested decode mutated receiver")
		}
	}
	value, _ := valid.WebFetchToolResult()
	value.Content.failure = &WebFetchError{}
	if err := value.Validate(); err == nil {
		t.Fatal("mixed success/error accepted")
	}
	valid.toolUse = &ToolUseBlock{}
	if err := valid.Validate(); err == nil {
		t.Fatal("mixed web-fetch/client variants accepted")
	}
	if _, err := json.Marshal(FetchedDocument{}); err == nil {
		t.Fatal("zero document accepted")
	}
}
