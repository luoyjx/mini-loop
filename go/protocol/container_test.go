package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestContainerUploadVariantIsolationAndStoredProjection(t *testing.T) {
	for _, makeBlock := range []func(string) Block{NewContainerUploadBlock, NewStreamedContainerUploadBlock} {
		original := makeBlock("registered-secret")
		content := BlockContent(original)
		clone := content.Clone()
		clone.blocks[0].containerUpload.FileID = "changed"
		if original.containerUpload.FileID != "registered-secret" {
			t.Fatal("container clone aliases input")
		}
		message := Message{Role: RoleAssistant, Content: content}
		masked, err := MaskedPythonJSON(message, func(value string) string { return strings.ReplaceAll(value, "registered-secret", "[masked]") }, false, true)
		if err != nil {
			t.Fatal(err)
		}
		restored, err := DecodeStoredMessage([]byte(masked))
		if err != nil {
			t.Fatal(err)
		}
		blocks, _ := restored.Content.Blocks()
		value, ok := blocks[0].ContainerUpload()
		if !ok || value.FileID != "[masked]" {
			t.Fatal("stored container projection changed")
		}
		if err := ValidateTranscript([]Message{message}); err != nil {
			t.Fatal("container requires client-tool pairing", err)
		}
	}
	for _, raw := range []string{`{"type":"container_upload"}`, `{"type":"container_upload","file_id":null}`, `{"type":"container_upload","file_id":3}`, `{"type":"container_upload","file_id":"file","text":"unexpected"}`} {
		block := NewContainerUploadBlock("original")
		if err := json.Unmarshal([]byte(raw), &block); err == nil || block.containerUpload.FileID != "original" {
			t.Fatal("invalid decode was not atomic", raw)
		}
	}
	if (Block{kind: BlockText, text: &TextBlock{Text: "x"}, containerUpload: &ContainerUploadBlock{FileID: "file"}}).Validate() == nil {
		t.Fatal("mixed variant admitted")
	}
	if (Block{kind: BlockContainerUpload, text: &TextBlock{Text: "x"}, containerUpload: &ContainerUploadBlock{FileID: "file"}}).Validate() == nil {
		t.Fatal("mixed container admitted")
	}
}
