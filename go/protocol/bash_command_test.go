package protocol

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestNonTextBashCommandObservationsSurviveCloneMaskAndHistory(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-provider-bash-command.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Input string `json:"input_json"`
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 36 {
		t.Fatal("Source command inventory changed")
	}
	for _, row := range fixture.Cases {
		input, err := DecodeModelToolInput(ToolBash, []byte(row.Input))
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := input.BashCommandObservation(); !ok {
			t.Fatal("non-text command became executable text")
		}
		var expected bytes.Buffer
		if err := json.Compact(&expected, []byte(row.Input)); err != nil {
			t.Fatal(err)
		}
		for _, value := range []ToolInput{input, input.clone()} {
			wire, err := json.Marshal(value)
			if err != nil || string(wire) != expected.String() {
				t.Fatal("observed command value/kind changed", string(wire), err)
			}
			message := Message{Role: RoleAssistant, Content: BlockContent(NewToolUse("command", value))}
			data, err := json.Marshal(message)
			if err != nil {
				t.Fatal(err)
			}
			stored, err := DecodeStoredMessage(data)
			if err != nil {
				t.Fatal(err)
			}
			blocks, _ := stored.Content.Blocks()
			use, _ := blocks[0].ToolUse()
			wire, err = json.Marshal(use.Input)
			if err != nil || string(wire) != expected.String() {
				t.Fatal("history changed command observation", string(wire), err)
			}
		}
		masked := MapToolInputStrings(input, func(s string) string { return strings.ReplaceAll(s, "secret", "masked") })
		projection, err := json.Marshal(masked)
		if err != nil || strings.Contains(string(projection), "secret") {
			t.Fatal("command observation leaked", string(projection), err)
		}
		live, err := json.Marshal(input)
		if err != nil || string(live) != expected.String() {
			t.Fatal("mask mutated original command", string(live), err)
		}
		if _, err := DecodeToolInput(ToolBash, []byte(row.Input)); err == nil {
			t.Fatal("explicit caller command admission became permissive")
		}
	}
	for _, raw := range []string{`{"command":NaN}`, `{"command":{"nested":Infinity}}`} {
		input, err := DecodeModelToolInput(ToolBash, []byte(raw))
		if err != nil {
			t.Fatal("SDK observation refused", err)
		}
		if _, err := json.Marshal(input); err == nil {
			t.Fatal("nonfinite observation entered finite JSON")
		}
		if _, err := DecodeToolInput(ToolBash, []byte(raw)); err == nil {
			t.Fatal("explicit caller admission widened")
		}
	}
}
