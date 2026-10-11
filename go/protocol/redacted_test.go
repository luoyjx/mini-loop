package protocol

import (
	"encoding/json"
	"testing"
)

func TestRedactedThinkingRoundTripAndIsolation(t *testing.T) {
	b := NewRedactedThinkingBlock("opaque encrypted data")
	wire, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Block
	if err := json.Unmarshal(wire, &decoded); err != nil {
		t.Fatal(err)
	}
	v, ok := decoded.RedactedThinking()
	if !ok || v.Data != "opaque encrypted data" {
		t.Fatal(v)
	}
	v.Data = "changed"
	copy := BlockContent(decoded).Clone()
	blocks, _ := copy.Blocks()
	got, _ := blocks[0].RedactedThinking()
	if got.Data != "opaque encrypted data" {
		t.Fatal("redacted data aliases accessor")
	}
	if _, ok := decoded.Text(); ok {
		t.Fatal("redacted data became model text")
	}
	for _, wire := range []string{`{"type":"redacted_thinking"}`, `{"type":"redacted_thinking","data":null}`, `{"type":"redacted_thinking","data":4}`, `{"type":"redacted_thinking","data":"x","text":"extra"}`} {
		if err := json.Unmarshal([]byte(wire), &decoded); err == nil {
			t.Fatal("malformed redacted block admitted", wire)
		}
	}
	if (Block{kind: BlockText, text: &TextBlock{Text: "x"}, redactedThinking: &RedactedThinkingBlock{Data: "x"}}).Validate() == nil {
		t.Fatal("mixed variants admitted")
	}
}
