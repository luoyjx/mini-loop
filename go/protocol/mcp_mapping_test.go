package protocol

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

func TestMCPMappingSourceRootsArchiveCloneAndStrictCaller(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-mcp-mapping-keys.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name  string
			Input string `json:"input_json"`
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil || len(fixture.Cases) != 116 {
		t.Fatal(err)
	}
	for _, row := range fixture.Cases {
		t.Run(row.Name, func(t *testing.T) {
			input, err := DecodeModelToolInput("mcp__peer__echo", []byte(row.Input))
			if err != nil {
				t.Fatal(err)
			}
			for _, copy := range []ToolInput{input, input.clone()} {
				message := Message{Role: RoleAssistant, Content: BlockContent(NewToolUse("model", copy))}
				archive, err := message.MarshalArchiveJSON()
				if err != nil {
					t.Fatal(err)
				}
				stored, err := DecodeStoredMessage(archive)
				if err != nil {
					t.Fatal(err)
				}
				blocks, _ := stored.Content.Blocks()
				use, _ := blocks[0].ToolUse()
				observed, err := use.Input.Observation()
				if err != nil {
					t.Fatal(err)
				}
				got, err := observed.LegacyJSON()
				want, _ := jsonvalue.Decode(row.Input)
				expected, _ := want.MarshalLegacyUTF8()
				if err != nil || string(got) != string(expected) {
					t.Fatal("original MCP root lost", string(got), string(expected), err)
				}
			}
			if _, err := DecodeToolInput("mcp__peer__echo", []byte(row.Input)); err == nil {
				t.Fatal("explicit caller accepted observed non-object root")
			}
		})
	}
}

func TestMCPMappingKeyIdentityAndMaskedProjection(t *testing.T) {
	input, err := DecodeModelToolInput("mcp__peer__echo", []byte(`[[true,"first"],[1,"second"],[1.0,"third"]]`))
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := input.ForModelBatch()
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := prepared.CanonicalJSON()
	if err != nil || canonical != `{"true":"third"}` {
		t.Fatal("first equal key / last value differs", canonical, err)
	}
	if _, ok := prepared.MCP(); ok {
		t.Fatal("non-text key became callable MCP arguments")
	}
	if _, ok := prepared.MCPKeywordBindingFailure(); !ok {
		t.Fatal("lost deferred keyword binding fault")
	}
	archive, err := (Message{Role: RoleAssistant, Content: BlockContent(NewToolUse("prepared", prepared))}).MarshalArchiveJSON()
	if err != nil {
		t.Fatal(err)
	}
	restored, err := DecodeStoredMessage(archive)
	if err != nil {
		t.Fatal(err)
	}
	blocks, _ := restored.Content.Blocks()
	use, _ := blocks[0].ToolUse()
	encoded, err := use.Input.MarshalJSON()
	if err != nil || string(encoded) != `{"true":"third"}` {
		t.Fatal("prepared archive kept raw pair array instead of recorded mapping", string(encoded), err)
	}
	secret, err := DecodeModelToolInput("mcp__peer__echo", []byte(`[[1,"secret-value"],["secret-key","visible"]]`))
	if err != nil {
		t.Fatal(err)
	}
	secret, err = secret.ForModelBatch()
	if err != nil {
		t.Fatal(err)
	}
	masked := MapToolInputStrings(secret, func(s string) string { return strings.ReplaceAll(s, "secret", "masked") })
	observation, err := masked.Observation()
	if err != nil {
		t.Fatal(err)
	}
	recorded, err := observation.LegacyJSON()
	if err != nil || strings.Contains(string(recorded), "secret") {
		t.Fatal(string(recorded), err)
	}
	live, _ := secret.Observation()
	raw, _ := live.LegacyJSON()
	if !strings.Contains(string(raw), "secret") {
		t.Fatal("mask mutated live mapping")
	}
	if _, err := observation.SortedPythonJSON(); err == nil {
		t.Fatal("mask hid mixed-key comparison fault")
	}
}
