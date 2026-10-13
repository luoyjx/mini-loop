package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestExtensionSchemaAdmissionAndNativeDecoderIsolation(t *testing.T) {
	for _, name := range []ToolName{"", "bash", "read_file", "return_artifact", "mcp__server__tool", ToolName(string([]byte{255}))} {
		if _, err := ExtensionToolSchema(name, "metadata", []byte(`{"type":"object"}`)); err == nil {
			t.Fatalf("accepted native/invalid name %q", name)
		}
	}
	for _, data := range []string{`{`, `NaN`, `1e309`, `"\ud800"`, strings.Repeat(" ", MaxProjectionBytes+1)} {
		if _, err := ExtensionToolSchema("greet", "metadata", []byte(data)); err == nil {
			t.Fatal("accepted invalid/nonfinite projection")
		}
	}
	var native ToolSchema
	if err := json.Unmarshal([]byte(`{"name":"bash","description":"native","input_schema":{"$ref":"#/$defs/x"}}`), &native); err == nil {
		t.Fatal("known native schema downgraded to extension observation")
	}
	schema, err := ExtensionToolSchema("greet", "metadata", []byte(`{"$ref":"#/$defs/x"}`))
	if err != nil {
		t.Fatal(err)
	}
	schema.Name = ToolBash
	if err := schema.Validate(); err == nil {
		t.Fatal("extension metadata acquired a native decoder name")
	}
}

func TestExtensionSchemaArchiveCanonicalOrderAndMaskedProjection(t *testing.T) {
	first, err := ExtensionToolSchema("greet", "metadata", []byte(`{"z":1,"a":{"secret-key":"secret-value","$ref":"secret-value"}}`))
	if err != nil {
		t.Fatal(err)
	}
	second, err := ExtensionToolSchema("greet", "metadata", []byte(`{"a":{"$ref":"secret-value","secret-key":"secret-value"},"z":1}`))
	if err != nil {
		t.Fatal(err)
	}
	a, _ := PythonJSON(first, false, true)
	b, _ := PythonJSON(second, false, true)
	if a != b {
		t.Fatal("metadata insertion order altered canonical identity", a, b)
	}
	encoded, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	var restored ToolSchema
	if err := json.Unmarshal(encoded, &restored); err != nil {
		t.Fatal(err)
	}
	got, err := PythonJSON(restored, false, true)
	if err != nil || got != a {
		t.Fatal("archive lost complete metadata", got, a, err)
	}
	masked, err := MaskedPythonJSON(first, func(text string) string { return strings.ReplaceAll(text, "secret", "MASK") }, false, true)
	if err != nil || strings.Contains(masked, "secret") || !strings.Contains(masked, "MASK-key") || !strings.Contains(masked, "MASK-value") {
		t.Fatal(masked, err)
	}
	unchanged, _ := PythonJSON(first, false, true)
	if unchanged != a {
		t.Fatal("recording mask changed live schema")
	}
}
