package protocol

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

func TestMCPModelRootsPreserveHistoryIdentityAndExplicitBoundary(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-mcp-model-roots.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name  string
			Input string `json:"input_json"`
		}
	}
	if err = json.Unmarshal(data, &fixture); err != nil || len(fixture.Cases) != 52 {
		t.Fatal(err)
	}
	for _, row := range fixture.Cases {
		t.Run(row.Name, func(t *testing.T) {
			input, err := DecodeModelToolInput("mcp__peer__echo", []byte(row.Input))
			if err != nil {
				t.Fatal(err)
			}
			expected, err := jsonvalue.Decode(row.Input)
			if err != nil {
				t.Fatal(err)
			}
			_, explicitErr := DecodeToolInput("mcp__peer__echo", []byte(row.Input))
			if (explicitErr == nil) != (expected.Kind() == jsonvalue.Object) {
				t.Fatal("explicit MCP object boundary widened", explicitErr)
			}
			for _, value := range []ToolInput{input, input.clone()} {
				canonical, err := value.CanonicalJSON()
				want, wantErr := PythonJSON(expected.Sorted(), false, true)
				if err != nil || wantErr != nil || canonical != want {
					t.Fatal(canonical, want, err)
				}
				message := Message{Role: RoleAssistant, Content: BlockContent(NewToolUse("model", value))}
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
				raw, err := use.Input.MarshalJSON()
				if err != nil {
					t.Fatal(err)
				}
				observed, err := jsonvalue.Decode(string(raw))
				if err != nil || !observed.PythonEqual(expected) {
					t.Fatal("archive normalized original MCP root", string(raw), err)
				}
			}
		})
	}
	input, err := DecodeModelToolInput("mcp__peer__echo", []byte(`[["secret-key","secret-value"]]`))
	if err != nil {
		t.Fatal(err)
	}
	masked := MapToolInputStrings(input, func(text string) string { return strings.ReplaceAll(text, "secret", "masked") })
	prepared, err := masked.ForModelBatch()
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := prepared.MarshalJSON()
	if err != nil || strings.Contains(string(encoded), "secret") {
		t.Fatal(string(encoded), err)
	}
	live, _ := input.MarshalJSON()
	if !strings.Contains(string(live), "secret") {
		t.Fatal("mask mutated live MCP root")
	}
	if _, err := DecodeModelToolInput("mcp__peer__echo", []byte(`{`)); err == nil {
		t.Fatal("accepted malformed model root")
	}
	if _, err := DecodeModelToolInput("mcp__peer__echo", []byte(`NaN`)); err == nil {
		t.Fatal("finite MCP root boundary widened")
	}
}
