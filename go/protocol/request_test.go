package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRequestDistinguishesAbsentAndEmptyTools(t *testing.T) {
	request := ModelRequest{Model: "model", MaxTokens: 2000, Messages: []Message{{Role: RoleUser, Content: PlainContent("summary")}}, Purpose: PurposeCompaction}
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"tools"`) || strings.Contains(string(data), `"purpose"`) || strings.Contains(string(data), `"system"`) {
		t.Fatal("absent fields or local provenance serialized")
	}
	request.Tools = []ToolSchema{}
	data, err = json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"tools":[]`) {
		t.Fatal("empty catalogue became absent")
	}
	clone := request.Clone()
	if clone.Tools == nil {
		t.Fatal("clone lost present empty tools")
	}
}
func TestSchemaRejectsNullObjectPropertiesAndIncompatibleNodes(t *testing.T) {
	var nullProperties SchemaProperties
	for _, schema := range []InputSchema{
		{Type: SchemaObject, Properties: &nullProperties},
		{Type: SchemaArray},
		{Type: SchemaString, Items: &InputSchema{Type: SchemaString}},
	} {
		if schema.Validate() == nil {
			t.Fatal("invalid schema node accepted")
		}
	}
	properties := SchemaProperties{}
	if err := (InputSchema{Type: SchemaObject, Properties: &properties}).Validate(); err != nil {
		t.Fatal(err)
	}
}

// Source send_message advertises unconstrained object-valued metadata without
// adding properties/additionalProperties. Preserve that exact schema shape.
func TestObjectSchemaRetainsUnconstrainedSourceMetadata(t *testing.T) {
	var schema InputSchema
	if err := json.Unmarshal([]byte(`{"type":"object"}`), &schema); err != nil {
		t.Fatal(err)
	}
	if err := schema.Validate(); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(schema.Clone())
	if err != nil || string(encoded) != `{"type":"object"}` {
		t.Fatal(string(encoded), err)
	}
	for _, raw := range []string{`{"type":"object","properties":null}`, `{"type":"object","properties":null,"additionalProperties":true}`, `{"type":"object","additionalProperties":null}`} {
		var invalid InputSchema
		if err := json.Unmarshal([]byte(raw), &invalid); err == nil {
			t.Fatal("null object schema admitted", raw)
		}
	}
}
