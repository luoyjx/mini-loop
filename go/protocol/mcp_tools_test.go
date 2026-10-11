package protocol

import (
	"encoding/json"
	"testing"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

func TestMCPInputRoundTripCanonicalAndMasking(t *testing.T) {
	name := MCPToolName("my.server", "echo text")
	raw := `{"z":[null,true,{"secret-key":"secret-value"}],"a":1,"unicode":"中文😀"}`
	input, err := DecodeToolInput(name, []byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	args, ok := input.MCP()
	if !ok || args.Arguments.Kind() != jsonvalue.Object {
		t.Fatal("variant missing")
	}
	canonical, err := input.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if canonical != `{"a":1,"unicode":"中文😀","z":[null,true,{"secret-key":"secret-value"}]}` {
		t.Fatal(canonical)
	}
	masked := MapToolInputStrings(input, func(s string) string {
		if s == "secret-key" || s == "secret-value" {
			return "hidden"
		}
		return s
	})
	maskedJSON, err := masked.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if maskedJSON != `{"a":1,"unicode":"中文😀","z":[null,true,{"hidden":"hidden"}]}` {
		t.Fatal(maskedJSON)
	}
	original, _ := input.CanonicalJSON()
	if original != canonical {
		t.Fatal("mask mutated live arguments")
	}
	data, err := input.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	restored, err := DecodeToolInput(name, data)
	if err != nil {
		t.Fatal(err)
	}
	again, _ := restored.CanonicalJSON()
	if again != canonical {
		t.Fatal("input archive changed")
	}
	for _, invalid := range []string{`null`, `[]`, `"text"`, `{"x":NaN}`, `{"x":"\ud800"}`} {
		if _, err := DecodeToolInput(name, []byte(invalid)); err == nil {
			t.Fatalf("accepted unsupported strict arguments %s", invalid)
		}
	}
	for _, invalid := range []ToolName{"mcp__a__b__c", "mcp____x", "mcp__a__b_", "ordinary"} {
		if _, err := MCPToolInput(invalid, args.Arguments); err == nil {
			t.Fatal("invalid namespace accepted")
		}
	}
}

func TestMCPSchemaPreservesExternalLanguageAndBoundary(t *testing.T) {
	for _, raw := range []string{`{"$defs":{"x":{"enum":[1,null]}},"type":"object","properties":{"value":{"$ref":"#/$defs/x"}},"additionalProperties":false}`, `null`, `false`} {
		value, err := jsonvalue.Decode(raw)
		if err != nil {
			t.Fatal(err)
		}
		schema, err := MCPToolSchema(MCPToolName("s", "tool"), "external", value)
		if err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(schema.Clone())
		if err != nil {
			t.Fatal(err)
		}
		var restored ToolSchema
		if err = json.Unmarshal(data, &restored); err != nil {
			t.Fatal(err)
		}
		again, err := json.Marshal(restored)
		if err != nil || string(again) != string(data) {
			t.Fatalf("schema changed: %s %s %v", data, again, err)
		}
		changed := schema.Clone()
		changed.Name = ToolBash
		if changed.Validate() == nil {
			t.Fatal("external schema attached to ordinary tool")
		}
	}
}
