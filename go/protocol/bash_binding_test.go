package protocol

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

func TestBashModelBindingMetadataSurvivesCloneMaskAndHistory(t *testing.T) {
	for _, raw := range []string{`{}`, `{"command":"true","secret-key":"secret-value"}`, `{"z_extra":1,"a_extra":2}`, `{"command":"true","":1}`} {
		input, err := DecodeModelToolInput(ToolBash, []byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := input.BashBindingFailure(); !ok {
			t.Fatal("invocation error lost")
		}
		if _, err := DecodeToolInput(ToolBash, []byte(raw)); err == nil {
			t.Fatal("strict explicit admission changed")
		}
		message := Message{Role: RoleAssistant, Content: BlockContent(NewToolUseWithCaller("call", input, nil))}
		encoded, err := json.Marshal(message.Content.Clone())
		if err != nil {
			t.Fatal(err)
		}
		value, err := jsonvalue.Decode(string(encoded))
		if err != nil {
			t.Fatal(err)
		}
		blocks, _ := value.Array()
		args, _ := blocks[0].Lookup("input")
		expected, _ := jsonvalue.Decode(raw)
		if !args.PythonEqual(expected) {
			t.Fatal("binding payload changed", string(encoded), raw)
		}
		wire, err := json.Marshal(message)
		if err != nil {
			t.Fatal(err)
		}
		stored, err := DecodeStoredMessage(wire)
		if err != nil {
			t.Fatal(err)
		}
		restored, _ := stored.Content.Blocks()
		use, _ := restored[0].ToolUse()
		failure, _ := input.BashBindingFailure()
		again, ok := use.Input.BashBindingFailure()
		if !ok || failure.Error() != again.Error() {
			t.Fatal("binding failure lost in history")
		}
		masked := MapToolInputStrings(input, func(s string) string { return strings.ReplaceAll(s, "secret", "masked") })
		projection, err := json.Marshal(masked)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(projection), "secret") {
			t.Fatal("unknown arguments not masked")
		}
		original, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		unchanged, _ := jsonvalue.Decode(string(original))
		if !unchanged.PythonEqual(expected) {
			t.Fatal("masked clone changed authority")
		}
	}
}
