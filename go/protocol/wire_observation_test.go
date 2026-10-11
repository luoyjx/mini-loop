package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestObservedWireMessagesRetainFiniteCacheJSONAndLegacyInput(t *testing.T) {
	for _, keep := range []string{"1", "NaN"} {
		input, err := DecodeModelToolInput(ToolBash, []byte(`{"command":"true","keep":`+keep+`}`))
		if err != nil {
			t.Fatal(err)
		}
		request := ModelRequest{Model: "model", MaxTokens: 8, Purpose: PurposeAgentTurn, Messages: []Message{
			{Role: RoleUser, Content: PlainContent("hello 名称")},
			{Role: RoleAssistant, Content: BlockContent(NewToolUse("call", input))},
			{Role: RoleUser, Content: BlockContent(NewToolResult("call", "error", true))},
		}, Cache: CacheAnnotations{Messages: []CacheBreakpoint{{MessageIndex: 2, BlockIndex: 0, Control: CacheControl{Type: CacheEphemeral}}}}}
		wire, err := request.Wire()
		if err != nil {
			t.Fatal(err)
		}
		for _, ascii := range []bool{false, true} {
			observed, err := ObservedWireMessagesPythonJSON(wire.Messages, ascii, false)
			if err != nil || !strings.Contains(observed, `"cache_control": {"type": "ephemeral"}`) {
				t.Fatal(observed, err)
			}
			if keep == "1" {
				finite, err := PythonJSON(wire.Messages, ascii, false)
				if err != nil || finite != observed {
					t.Fatal(observed, finite, err)
				}
			} else {
				if !strings.Contains(observed, "NaN") {
					t.Fatal(observed)
				}
				if _, err := json.Marshal(wire); err == nil {
					t.Fatal("observation enabled finite HTTP encoding")
				}
			}
		}
	}
}
