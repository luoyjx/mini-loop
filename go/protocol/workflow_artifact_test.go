package protocol

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

func TestWorkflowArtifactInputAndExactSchema(t *testing.T) {
	for _, raw := range []string{`{"value":null}`, `{"value":{"secret":[1,1.0,true,"secret"]}}`, `{"value":1,"value":2}`} {
		input, err := DecodeToolInput(ToolReturnArtifact, []byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		wire, err := input.MarshalJSON()
		if err != nil {
			t.Fatal(err)
		}
		copy, err := DecodeToolInput(input.Name(), wire)
		if err != nil {
			t.Fatal(err)
		}
		a, _ := input.CanonicalJSON()
		b, _ := copy.CanonicalJSON()
		if a != b {
			t.Fatal(a, b)
		}
		masked := MapToolInputStrings(input, func(text string) string { return strings.ReplaceAll(text, "secret", "hidden") })
		maskedWire, _ := masked.MarshalJSON()
		if strings.Contains(string(maskedWire), "secret") {
			t.Fatal(string(maskedWire))
		}
		unchanged, _ := input.MarshalJSON()
		if string(unchanged) != string(wire) {
			t.Fatal("masking mutated original")
		}
	}
	for _, raw := range []string{`{}`, `null`, `[]`, `{"value":1,"owner":"foreign"}`} {
		if _, err := DecodeToolInput(ToolReturnArtifact, []byte(raw)); err == nil {
			t.Fatal("accepted", raw)
		}
	}
	output, _ := jsonvalue.Decode(`{"type":"integer","enum":[1,2],"const":2}`)
	schema, err := WorkflowArtifactSchema(output)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := json.Marshal(schema.Clone())
	if err != nil {
		t.Fatal(err)
	}
	var copy ToolSchema
	if err := json.Unmarshal(wire, &copy); err != nil {
		t.Fatal(err)
	}
	repeated, _ := json.Marshal(copy.Clone())
	if string(wire) != string(repeated) || !strings.Contains(string(wire), `"enum":[1,2]`) {
		t.Fatalf("schema loss %s %s", wire, repeated)
	}
	copy.Name = ToolBash
	if copy.Validate() == nil {
		t.Fatal("exact schema leaked into another tool")
	}
}
