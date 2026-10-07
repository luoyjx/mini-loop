package protocol

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

func TestSelfAuditSourceSchemaAndKeywordBoundary(t *testing.T) {
	var source struct {
		Schema            ToolSchema `json:"schema"`
		AbsentFromDefault bool       `json:"absent_from_default"`
		Readonly          bool       `json:"readonly"`
		Risk              string     `json:"risk"`
		ParallelSafe      bool       `json:"parallel_safe"`
		ExecutionMode     string     `json:"execution_mode"`
		Cases             []struct {
			Input    json.RawMessage `json:"input"`
			Accepted bool            `json:"accepted"`
		} `json:"cases"`
	}
	data, err := os.ReadFile("../testdata/python-self-audit-tool.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &source); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(SelfAuditSchema(), source.Schema) {
		t.Fatal("source schema differs")
	}
	if !source.AbsentFromDefault || !source.Readonly || source.Risk != "read" || source.ParallelSafe || source.ExecutionMode != "exclusive" {
		t.Fatal("source installation/traits contract changed")
	}
	if _, installed := DefaultToolSchema(ToolSelfAudit); installed {
		t.Fatal("installed in default catalogue")
	}
	for _, name := range DefaultToolNames() {
		if name == ToolSelfAudit {
			t.Fatal("installed in default names")
		}
	}
	for _, row := range source.Cases {
		_, err := DecodeToolInput(ToolSelfAudit, row.Input)
		if (err == nil) != row.Accepted {
			t.Fatalf("%s: %v", row.Input, err)
		}
	}
}

func TestSelfAuditClosedInputRoundTrip(t *testing.T) {
	for _, raw := range []string{"", "null", "[]", "true", "1", `"x"`, "{} {}", `{"owner":null}`, `{"owner":"alice","owner":null}`} {
		if _, err := DecodeToolInput(ToolSelfAudit, []byte(raw)); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
	input, err := DecodeToolInput(ToolSelfAudit, []byte(" \n { } \t"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(input, SelfAuditToolInput()) {
		t.Fatal("wrong discriminator")
	}
	if _, ok := input.SelfAudit(); !ok {
		t.Fatal("missing typed payload")
	}
	if _, ok := GoalStatusToolInput().SelfAudit(); ok {
		t.Fatal("foreign discriminator accepted")
	}
	for _, copy := range []ToolInput{input, MapToolInputStrings(input, nil), MapToolInputStrings(input, func(string) string { t.Fatal("empty payload has strings"); return "" })} {
		if err := copy.Validate(); err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(copy)
		if err != nil || string(encoded) != "{}" {
			t.Fatal(string(encoded), err)
		}
		canonical, err := copy.CanonicalJSON()
		if err != nil || canonical != "{}" {
			t.Fatal(canonical, err)
		}
		sorted, err := copy.SortedPythonJSON()
		if err != nil || sorted != "{}" {
			t.Fatal(sorted, err)
		}
		decoded, err := DecodeToolInput(copy.Name(), encoded)
		if err != nil || !reflect.DeepEqual(decoded, input) {
			t.Fatal(decoded, err)
		}
	}
	var block Block
	if err := json.Unmarshal([]byte(`{"type":"tool_use","id":"audit","name":"self_audit","input":{}}`), &block); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(block)
	if err != nil {
		t.Fatal(err)
	}
	var restored Block
	if err := json.Unmarshal(encoded, &restored); err != nil {
		t.Fatal(err)
	}
	if err := restored.Validate(); err != nil {
		t.Fatal(err)
	}
	use, ok := restored.ToolUse()
	if !ok || use.Name != ToolSelfAudit || use.Input.Name() != ToolSelfAudit {
		t.Fatal("tool-use discriminator lost")
	}

	first := SelfAuditSchema()
	(*first.InputSchema.Properties)["owner"] = InputSchema{Type: SchemaString}
	if len(*SelfAuditSchema().InputSchema.Properties) != 0 {
		t.Fatal("schema storage aliased")
	}
}
