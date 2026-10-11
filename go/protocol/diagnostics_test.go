package protocol

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestDiagnosticsSourceSchemaAndTypedOptionalPath(t *testing.T) {
	var source struct{ Schema ToolSchema }
	data, err := os.ReadFile("../testdata/python-diagnostics.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &source); err != nil {
		t.Fatal(err)
	}
	if got, want := diagnosticsSchemaJSON(t, DiagnosticsSchema()), diagnosticsSchemaJSON(t, source.Schema); got != want {
		t.Fatal(got, want)
	}
	for _, encoded := range []string{`{}`, `{"path":null}`, `{"path":""}`, `{"path":"文.py"}`} {
		input, err := DecodeToolInput(ToolDiagnostics, []byte(encoded))
		if err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(input)
		if err != nil || string(data) != encoded {
			t.Fatal(string(data), err)
		}
		canonical, err := input.CanonicalJSON()
		if err != nil || canonical != encoded {
			t.Fatal(canonical, err)
		}
		value, ok := input.Diagnostics()
		if !ok {
			t.Fatal("wrong discriminator")
		}
		if value.Path != nil {
			*value.Path = "mutated"
		}
		again, _ := json.Marshal(input)
		if string(again) != encoded {
			t.Fatal("accessor aliased input")
		}
	}
	for _, encoded := range []string{`null`, `[]`, `{"path":3}`, `{"path":true}`, `{"path":[]}`, `{"path":{}}`, `{"root":"foreign"}`, `{"provider":"foreign"}`, `{"path":"x.py","extra":1}`} {
		if _, err := DecodeToolInput(ToolDiagnostics, []byte(encoded)); err == nil {
			t.Fatal("invalid input admitted", encoded)
		}
	}
}

func TestDiagnosticsInputConstructionAndMaskingDetach(t *testing.T) {
	path := "private.py"
	input := DiagnosticsToolInput(DiagnosticsInput{Path: &path})
	path = "mutated"
	masked := MapToolInputStrings(input, func(value string) string { return strings.ReplaceAll(value, "private", "[masked]") })
	original, _ := input.Diagnostics()
	projection, _ := masked.Diagnostics()
	if *original.Path != "private.py" || *projection.Path != "[masked].py" {
		t.Fatal(original, projection)
	}
	explicitNull, err := DecodeToolInput(ToolDiagnostics, []byte(`{"path":null}`))
	if err != nil {
		t.Fatal(err)
	}
	maskedNull := MapToolInputStrings(explicitNull, func(value string) string { return "masked" })
	encoded, err := json.Marshal(maskedNull)
	if err != nil || string(encoded) != `{"path":null}` {
		t.Fatal(string(encoded), err)
	}
}

func diagnosticsSchemaJSON(t *testing.T, schema ToolSchema) string {
	t.Helper()
	encoded, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}
