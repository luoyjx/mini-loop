package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestContentStorageIdentityAndCallerPresence(t *testing.T) {
	for _, content := range []Content{PlainContent("same"), BlockContent(NewTextBlock("same"))} {
		copyOf := content
		if !copyOf.SameStorage(content) {
			t.Fatal("immutable copy lost identity")
		}
		replacement := PlainContent("same")
		if _, ok := content.Blocks(); ok {
			replacement = BlockContent(NewTextBlock("same"))
		}
		if replacement.SameStorage(content) {
			t.Fatal("new storage shares identity")
		}
	}
	for _, caller := range []string{"", `,"caller":null`, `,"caller":{"type":"direct"}`} {
		body := `{"type":"tool_use","id":"u","name":"bash","input":{"command":"echo x"}` + caller + `}`
		var block Block
		if err := json.Unmarshal([]byte(body), &block); err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(block)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(encoded), `"caller"`) != (caller != "") {
			t.Fatal(string(encoded))
		}
		if caller == `,"caller":null` && !strings.Contains(string(encoded), `"caller":null`) {
			t.Fatal(string(encoded))
		}
	}
}
