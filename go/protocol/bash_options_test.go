package protocol

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestBashModelOptionsRetainActualSourceValues(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-provider-bash-options.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Field, Name string
			Input       string `json:"input_json"`
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 56 {
		t.Fatal("Source option inventory changed")
	}
	for _, row := range fixture.Cases {
		t.Run(row.Field+"/"+row.Name, func(t *testing.T) {
			input, err := DecodeModelToolInput(ToolBash, []byte(row.Input))
			if err != nil {
				t.Fatal(err)
			}
			var expected bytes.Buffer
			if err := json.Compact(&expected, []byte(row.Input)); err != nil {
				t.Fatal(err)
			}
			cloned := input.clone()
			for _, value := range []ToolInput{input, cloned} {
				encoded, err := json.Marshal(value)
				if err != nil || string(encoded) != expected.String() {
					t.Fatal("observed option value or kind changed", string(encoded), err)
				}
				message := Message{Role: RoleAssistant, Content: BlockContent(NewToolUse("options", value))}
				wire, err := json.Marshal(message)
				if err != nil {
					t.Fatal(err)
				}
				stored, err := DecodeStoredMessage(wire)
				if err != nil {
					t.Fatal(err)
				}
				blocks, _ := stored.Content.Blocks()
				use, _ := blocks[0].ToolUse()
				restored, err := json.Marshal(use.Input)
				if err != nil || string(restored) != expected.String() {
					t.Fatal("history changed option observation", string(restored), err)
				}
			}
			masked := MapToolInputStrings(input, func(s string) string { return strings.ReplaceAll(s, "secret", "masked") })
			projection, err := json.Marshal(masked)
			if err != nil || strings.Contains(string(projection), "secret") {
				t.Fatal("optional observation leaked", string(projection), err)
			}
			live, err := json.Marshal(input)
			if err != nil || string(live) != expected.String() {
				t.Fatal("mask mutated live options", string(live), err)
			}
			bash, _ := cloned.Bash()
			if bash.ApprovalPrefix != nil && len(*bash.ApprovalPrefix) > 0 {
				(*bash.ApprovalPrefix)[0] = "mutated"
			}
			if bash.RunInBackground != nil {
				*bash.RunInBackground = !*bash.RunInBackground
			}
			live, err = json.Marshal(input)
			if err != nil || string(live) != expected.String() {
				t.Fatal("accessor mutated live options")
			}
		})
	}
	for _, raw := range []string{`{"command":"true","run_in_background":"false"}`, `{"command":"true","approval_prefix":["printf",7]}`} {
		if _, err := DecodeToolInput(ToolBash, []byte(raw)); err == nil {
			t.Fatal("explicit caller admission became permissive")
		}
	}
	for _, raw := range []string{`{"command":"true","run_in_background":NaN}`, `{"command":"true","approval_prefix":[Infinity]}`} {
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
