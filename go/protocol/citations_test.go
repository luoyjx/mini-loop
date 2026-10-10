package protocol

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

func citationJSONEqual(t *testing.T, actual, expected []byte) {
	t.Helper()
	decode := func(data []byte) any {
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		var value any
		if err := decoder.Decode(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	if !reflect.DeepEqual(decode(actual), decode(expected)) {
		t.Fatalf("JSON differs:\n%s\nwant:\n%s", actual, expected)
	}
}
func TestCitationSourceCompletedBlocksRoundTrip(t *testing.T) {
	fixture, err := os.ReadFile("../testdata/python-provider-citations.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Completed []struct {
			Name       string
			Normalized json.RawMessage
		}
	}
	if err := json.Unmarshal(fixture, &corpus); err != nil {
		t.Fatal(err)
	}
	if len(corpus.Completed) != 17 {
		t.Fatal("missing Source cases")
	}
	for _, test := range corpus.Completed {
		t.Run(test.Name, func(t *testing.T) {
			var block Block
			if err := json.Unmarshal(test.Normalized, &block); err != nil {
				t.Fatal(err)
			}
			got, err := json.Marshal(block)
			if err != nil {
				t.Fatal(err)
			}
			citationJSONEqual(t, got, test.Normalized)
			request := ModelRequest{Model: "citation-model", MaxTokens: 8, Purpose: PurposeAgentTurn,
				Messages: []Message{{Role: RoleUser, Content: PlainContent("probe")}, {Role: RoleAssistant, Content: BlockContent(block)}, {Role: RoleUser, Content: PlainContent("next")}}}
			forwarded, err := json.Marshal(request.Clone())
			if err != nil {
				t.Fatal(err)
			}
			var wire struct {
				Messages []struct{ Content json.RawMessage }
			}
			if err := json.Unmarshal(forwarded, &wire); err != nil {
				t.Fatal(err)
			}
			var blocks []json.RawMessage
			if err := json.Unmarshal(wire.Messages[1].Content, &blocks); err != nil {
				t.Fatal(err)
			}
			citationJSONEqual(t, blocks[0], test.Normalized)
			text, ok := block.Text()
			if !ok {
				t.Fatal("missing text")
			}
			items, array := text.Citations().Items()
			switch test.Name {
			case "char_location", "page_location", "content_block_location", "web_search_result_location", "search_result_location":
				if !array || len(items) != 1 || items[0].Kind() != CitationKind(test.Name) {
					t.Fatal("known variant not selected")
				}
			case "wrong_known_field", "missing_known_field", "unknown_variant", "null_item":
				if !array || items[0].Kind() != CitationExtension {
					t.Fatal("extension not preserved")
				}
			case "unbounded_index":
				char, ok := items[0].Char()
				if !ok || char.DocumentIndex.Decimal() != "1"+strings.Repeat("0", 80) {
					t.Fatal("integer truncated")
				}
			case "negative_indices":
				char, ok := items[0].Char()
				if !ok || char.StartCharIndex.Decimal() != "-2" {
					t.Fatal("negative index lost")
				}
			}
			clone := BlockContent(block).Clone()
			copied, _ := clone.Blocks()
			got, err = json.Marshal(copied[0])
			if err != nil {
				t.Fatal(err)
			}
			citationJSONEqual(t, got, test.Normalized)
			if len(items) > 0 {
				items[0] = Citation{}
			}
			again, _ := text.Citations().Items()
			if len(again) > 0 && again[0].Kind() == "" {
				t.Fatal("accessor aliases list")
			}
			for _, item := range again {
				if char, ok := item.Char(); ok {
					char.CitedText = "changed"
				}
			}
			got, err = json.Marshal(block)
			if err != nil {
				t.Fatal(err)
			}
			citationJSONEqual(t, got, test.Normalized)
		})
	}
}
func TestCitationPresenceAndCacheEncoding(t *testing.T) {
	for _, data := range []string{
		`{"type":"text","text":"x"}`,
		`{"type":"text","text":"x","citations":null}`,
		`{"type":"text","text":"x","citations":[]}`,
	} {
		var block Block
		if err := json.Unmarshal([]byte(data), &block); err != nil {
			t.Fatal(err)
		}
		got, err := block.marshalWithCache(&CacheControl{Type: CacheEphemeral})
		if err != nil {
			t.Fatal(err)
		}
		expected := strings.TrimSuffix(data, "}") + `,"cache_control":{"type":"ephemeral"}}`
		citationJSONEqual(t, got, []byte(expected))
		text, _ := block.Text()
		if text.Citations().Present() != strings.Contains(data, "citations") {
			t.Fatal("presence changed")
		}
		encoded, err := json.Marshal(text)
		if err != nil {
			t.Fatal(err)
		}
		var round TextBlock
		if err := json.Unmarshal(encoded, &round); err != nil {
			t.Fatal(err)
		}
		if round.Citations().Present() != text.Citations().Present() {
			t.Fatal("text projection lost presence")
		}
	}
}
func TestCitationMaskingAndImmutableAppend(t *testing.T) {
	citations, err := DecodeCitations([]byte(`[{"type":"future_location","fixture-secret":{"fixture-secret":"fixture-secret"}}]`))
	if err != nil {
		t.Fatal(err)
	}
	original, _ := json.Marshal(citations)
	masked := citations.MapStrings(func(s string) string { return strings.ReplaceAll(s, "fixture-secret", "[redacted]") })
	got, err := json.Marshal(masked)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(got, []byte("fixture-secret")) {
		t.Fatal("extension key or value leaked")
	}
	after, _ := json.Marshal(citations)
	if !bytes.Equal(original, after) {
		t.Fatal("mask changed original")
	}
	var citation Citation
	if err := json.Unmarshal([]byte(`{"type":"future_location","x":1}`), &citation); err != nil {
		t.Fatal(err)
	}
	appended, err := citations.Append(citation)
	if err != nil {
		t.Fatal(err)
	}
	items, _ := appended.Items()
	old, _ := citations.Items()
	if len(items) != 2 || len(old) != 1 {
		t.Fatal("append mutated list")
	}
	for _, data := range [][]byte{nil, []byte("null"), []byte("[]")} {
		empty, err := DecodeCitations(data)
		if err != nil {
			t.Fatal(err)
		}
		next, err := empty.Append(citation)
		if err != nil {
			t.Fatal(err)
		}
		items, ok := next.Items()
		if !ok || len(items) != 1 {
			t.Fatal("SDK null initialization lost")
		}
	}
	invalid, _ := DecodeCitations([]byte(`"opaque"`))
	if _, err := invalid.Append(citation); err == nil {
		t.Fatal("non-array accumulation accepted")
	}
	if _, err := DecodeCitations([]byte(`{"broken"`)); err == nil {
		t.Fatal("invalid JSON accepted")
	}
	block := NewTextBlockWithCitations("fixture-secret", citations)
	recording, err := MaskedPythonJSON(block, func(s string) string { return strings.ReplaceAll(s, "fixture-secret", "[redacted]") }, false, true)
	if err != nil || strings.Contains(recording, "fixture-secret") {
		t.Fatalf("recording leak: %s %v", recording, err)
	}
}

func TestCitationStoredMessageHasIndependentFrameLimit(t *testing.T) {
	data := []byte(`{"role":"assistant","content":[{"type":"text","text":"x","citations":[{"type":"future_location","extension":"` + strings.Repeat("a", MaxWireBytes) + `"}]}]}`)
	var bounded Message
	if json.Unmarshal(data, &bounded) == nil {
		t.Fatal("network limit bypassed")
	}
	stored, err := DecodeStoredMessage(data)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(stored.Content.Clone())
	if err != nil {
		t.Fatal(err)
	}
	var row struct{ Content json.RawMessage }
	if err := json.Unmarshal(data, &row); err != nil {
		t.Fatal(err)
	}
	citationJSONEqual(t, encoded, row.Content)
}
