package protocol

import (
	"encoding/json"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"os"
	"strings"
	"testing"
)

func TestBackgroundOptionsRetainArchivesAndFiniteCallerBoundary(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-provider-background-options.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name  string
			Input string `json:"input_json"`
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, row := range fixture.Cases {
		t.Run(row.Name, func(t *testing.T) {
			for _, raw := range []string{`{"command":"true","timeout":true}`, `{"command":"true","timeout":0.5}`, `{"command":"true","timeout":{}}`, `{"command":"true","timeout":[]}`, `{"command":"true","timeout":NaN}`, `{"command":"true","approval_prefix":1}`, `{"command":"true","approval_prefix":[1]}`} {
				if _, err := DecodeToolInput(ToolBackgroundRun, []byte(raw)); err == nil {
					t.Fatal("strict caller admission changed", raw)
				}
			}
			input, err := DecodeModelToolInput(ToolBackgroundRun, []byte(row.Input))
			if err != nil {
				t.Fatal(err)
			}
			expected, err := jsonvalue.Decode(row.Input)
			if err != nil {
				t.Fatal(err)
			}
			finite, finiteErr := expected.MarshalJSON()
			for _, value := range []ToolInput{input, input.clone(), MapToolInputStrings(input, func(text string) string { return text })} {
				observation, err := value.Observation()
				if err != nil {
					t.Fatal(err)
				}
				raw, err := observation.LegacyJSON()
				if err != nil {
					t.Fatal(err)
				}
				actual, err := jsonvalue.Decode(string(raw))
				got, gotErr := actual.Sorted().MarshalLegacyUTF8()
				want, wantErr := expected.Sorted().MarshalLegacyUTF8()
				if err != nil || gotErr != nil || wantErr != nil || string(got) != string(want) {
					t.Fatal("optional observation changed", string(raw), err)
				}
				wire, err := json.Marshal(value)
				if (err == nil) != (finiteErr == nil) {
					t.Fatal("finite boundary changed", err, finiteErr)
				}
				if err == nil {
					got, _ := jsonvalue.Decode(string(wire))
					want, _ := jsonvalue.Decode(string(finite))
					if !got.PythonEqual(want) {
						t.Fatal("finite projection changed")
					}
				}
				message := Message{Role: RoleAssistant, Content: BlockContent(NewToolUse("options", value))}
				archive, err := message.MarshalArchiveJSON()
				if err != nil {
					t.Fatal(err)
				}
				restored, err := DecodeStoredMessage(archive)
				if err != nil {
					t.Fatal(err)
				}
				blocks, _ := restored.Content.Blocks()
				use, _ := blocks[0].ToolUse()
				before, _ := value.CanonicalJSON()
				after, err := use.Input.CanonicalJSON()
				if err != nil || before != after {
					t.Fatal("archive identity changed", err)
				}
			}

		})
	}
	input, err := DecodeModelToolInput(ToolBackgroundRun, []byte(`{"command":"printf secret","timeout":{"secret-key":"secret-value"},"approval_prefix":["printf","secret"]}`))
	if err != nil {
		t.Fatal(err)
	}
	masked := MapToolInputStrings(input, func(text string) string { return strings.ReplaceAll(text, "secret", "masked") })
	encoded, err := json.Marshal(masked)
	if err != nil || strings.Contains(string(encoded), "secret") {
		t.Fatal("masked optional projection leaked", string(encoded), err)
	}
	live, _ := json.Marshal(input)
	if !strings.Contains(string(live), "secret-key") {
		t.Fatal("mask mutated live observation")
	}
}
