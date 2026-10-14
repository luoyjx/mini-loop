package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

func TestSDKUnicodeObservationPreservesNamedFieldsAndStrictWire(t *testing.T) {
	high := string([]byte{0xed, 0xa0, 0x80})
	low := string([]byte{0xed, 0xbf, 0xbf})
	blocks := []Block{NewTextBlock("before" + high + "after"), NewThinkingBlock(high, "signature"),
		NewThinkingBlock("thought", low), NewRedactedThinkingBlock(high), NewContainerUploadBlock(low)}
	fields := []string{"text", "thinking", "signature", "data", "file_id"}
	texts := []string{"before" + high + "after", high, low, high, low}
	for i, original := range blocks {
		t.Run(fields[i], func(t *testing.T) {
			cloned, _ := BlockContent(original).Clone().Blocks()
			for _, block := range []Block{original, cloned[0]} {
				ascii, err := block.MarshalObservationASCIIJSON()
				if err != nil || !json.Valid(ascii) || bytes.Contains(ascii, []byte{0xed}) {
					t.Fatalf("SDK ASCII snapshot: %s (%v)", ascii, err)
				}
				value, err := jsonvalue.Decode(string(ascii))
				if err != nil {
					t.Fatal(err)
				}
				field, _ := value.Lookup(fields[i])
				actual, ok := field.Text()
				if !ok || actual != texts[i] {
					t.Fatal("snapshot changed original code points")
				}
				if _, err := json.Marshal(block); !errors.Is(err, jsonvalue.ErrSurrogate) {
					t.Fatal("standard JSON silently replaced invalid Unicode", err)
				}
				if _, err := block.MarshalObservationJSON(); !errors.Is(err, jsonvalue.ErrSurrogate) {
					t.Fatal("UTF-8 observation changed its existing boundary", err)
				}
				message := Message{Role: RoleAssistant, Content: BlockContent(block)}
				if _, err := json.Marshal(message); !errors.Is(err, jsonvalue.ErrSurrogate) {
					t.Fatal("message wire replaced invalid Unicode", err)
				}
				if _, err := message.MarshalArchiveJSON(); !errors.Is(err, jsonvalue.ErrSurrogate) {
					t.Fatal("archive unexpectedly widened its UTF-8 boundary", err)
				}
			}
		})
	}
	input, err := DecodeModelToolInput(ToolBash, []byte(`{"command":"echo \ud800"}`))
	if err != nil {
		t.Fatal(err)
	}
	command, _ := input.Bash()
	if command.Command != "echo "+high {
		t.Fatal("model Bash command lost surrogate observation")
	}
	if _, err := json.Marshal(input); !errors.Is(err, jsonvalue.ErrSurrogate) {
		t.Fatal("Bash request encoding replaced the observed command", err)
	}
	if _, err := input.CanonicalJSON(); !errors.Is(err, jsonvalue.ErrSurrogate) {
		t.Fatal("canonical UTF-8 input hash replaced the observed command", err)
	}
}
