package protocol

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

func TestUnknownModelToolObservationsPreserveHistoryMaskAndIdentity(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-provider-unknown-tool.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name     string
			ToolName ToolName `json:"tool_name"`
			Input    string   `json:"input_json"`
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, row := range fixture.Cases {
		t.Run(row.Name, func(t *testing.T) {
			input, err := DecodeModelToolInput(row.ToolName, []byte(row.Input))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := DecodeToolInput(row.ToolName, []byte(row.Input)); err == nil {
				t.Fatal("explicit unknown admission changed")
			}
			expected, err := jsonvalue.Decode(row.Input)
			if err != nil {
				t.Fatal(err)
			}
			for _, value := range []ToolInput{input, input.clone()} {
				canonical, err := value.CanonicalJSON()
				want, wantErr := PythonJSON(expected.Sorted(), false, true)
				if err != nil || wantErr != nil || canonical != want {
					t.Fatal("unknown step identity changed", canonical, want, err)
				}
				message := Message{Role: RoleAssistant, Content: BlockContent(NewToolUse("unknown", value))}
				encoded, err := json.Marshal(message)
				if err != nil {
					t.Fatal(err)
				}
				stored, err := DecodeStoredMessage(encoded)
				if err != nil {
					t.Fatal(err)
				}
				blocks, _ := stored.Content.Blocks()
				use, _ := blocks[0].ToolUse()
				if _, ok := use.Input.UnknownModelToolObservation(); !ok || use.Name != row.ToolName {
					t.Fatal("history reclassified inert observation")
				}
				wire, err := json.Marshal(use.Input)
				if err != nil {
					t.Fatal(err)
				}
				observed, err := jsonvalue.Decode(string(wire))
				if err != nil || !observed.PythonEqual(expected) {
					t.Fatal("unknown argument kind changed", string(wire), err)
				}
			}
			masked := MapToolInputStrings(input, func(s string) string { return strings.ReplaceAll(s, "secret", "masked") })
			projection, err := json.Marshal(masked)
			if err != nil || strings.Contains(string(projection), "secret") {
				t.Fatal("observation leaked secret", string(projection), err)
			}
			live, err := json.Marshal(input)
			if err != nil {
				t.Fatal(err)
			}
			observed, err := jsonvalue.Decode(string(live))
			if err != nil || !observed.PythonEqual(expected) {
				t.Fatal("mask mutated original observation")
			}
		})
	}
}

func TestUnknownModelToolFallbackDoesNotHideKnownDecoderErrors(t *testing.T) {
	for _, row := range []struct {
		Name  ToolName
		Input string
	}{
		{ToolReadFile, `{"path":17}`}, {ToolTodoWrite, `{"items":"invalid"}`}, {"mcp__missing__tool", `{broken`},
		{"missing-tool", `NaN`}, {"missing-tool", `{"nested":Infinity}`}, {"missing-tool", `{broken`},
	} {
		if _, err := DecodeModelToolInput(row.Name, []byte(row.Input)); err == nil {
			t.Fatal("known decoder or finite observation failure was hidden", row)
		}
	}
	// Source preserves a model null until batch preparation turns it into an
	// empty object. Explicit MCP calls still require object-shaped arguments.
	if _, err := DecodeToolInput("mcp__missing__tool", []byte(`null`)); err == nil {
		t.Fatal("explicit MCP decoder accepted a non-object root")
	}
	if _, err := DecodeToolInput("mcp__missing__tool", []byte(`NaN`)); err == nil {
		t.Fatal("explicit MCP decoder accepted a nonfinite root")
	}
	input, err := DecodeModelToolInput("missing-tool", []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	input.name = ToolBash
	if err := input.Validate(); err == nil {
		t.Fatal("inert observation acquired a known executable name")
	}
}
