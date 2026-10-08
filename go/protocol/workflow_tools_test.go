package protocol

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

func TestWorkflowToolContractsMatchPython(t *testing.T) {
	var fixture struct {
		Inputs []struct {
			Name              string
			Tool              ToolName
			Input             string `json:"input_json"`
			Canonical, Spaced string
		}
		Schemas  []ToolSchema
		Refusals []struct {
			Tool  ToolName
			Input string `json:"input_json"`
		}
	}
	data, err := os.ReadFile(filepath.Join("..", "testdata", "python-workflow-tools.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, row := range fixture.Inputs {
		t.Run(row.Name, func(t *testing.T) {
			input, err := DecodeToolInput(row.Tool, []byte(row.Input))
			if err != nil {
				t.Fatal(err)
			}
			for _, copy := range []ToolInput{input, input.clone(), MapToolInputStrings(input, nil)} {
				got, err := copy.CanonicalJSON()
				if err != nil || got != row.Canonical {
					t.Fatalf("canonical got %q, %v; want %q", got, err, row.Canonical)
				}
				spaced, err := copy.SortedPythonJSON()
				if err != nil || spaced != row.Spaced {
					t.Fatalf("spaced got %q, %v; want %q", spaced, err, row.Spaced)
				}
			}
			wire, err := input.MarshalJSON()
			if err != nil {
				t.Fatal(err)
			}
			repeated, err := DecodeToolInput(row.Tool, wire)
			if err != nil {
				t.Fatal(err)
			}
			got, err := repeated.CanonicalJSON()
			if err != nil || got != row.Canonical {
				t.Fatal("archival round trip lost identity", got, err)
			}
			if row.Tool == ToolWorkflow {
				value, ok := input.Workflow()
				if !ok || value.Definition.Kind() != jsonvalue.Object || value.Args.Kind() != jsonvalue.Object {
					t.Fatal("missing closed launch input")
				}
				if _, ok := input.WorkflowReference(); ok {
					t.Fatal("launch exposed reference")
				}
			} else {
				if _, ok := input.WorkflowReference(); !ok {
					t.Fatal("missing named reference")
				}
				if _, ok := input.Workflow(); ok {
					t.Fatal("reference exposed launch")
				}
			}
		})
	}
	for _, row := range fixture.Refusals {
		if _, err := DecodeToolInput(row.Tool, []byte(row.Input)); err == nil {
			t.Fatal("accepted invalid tool shape", row.Tool, row.Input)
		}
	}
	for _, name := range []ToolName{ToolWorkflow, ToolWorkflowStatus, ToolWorkflowCancel} {
		for _, raw := range []string{`null`, `[]`, `"x"`} {
			if _, err := DecodeToolInput(name, []byte(raw)); err == nil {
				t.Fatal("non-object input admitted")
			}
		}
		for _, defaultName := range DefaultToolNames() {
			if name == defaultName {
				t.Fatal("optional workflow tool became default")
			}
		}
	}
	schemas := WorkflowToolSchemas()
	if len(schemas) != len(fixture.Schemas) {
		t.Fatal("missing schema")
	}
	for i, schema := range schemas {
		if err := schema.Validate(); err != nil {
			t.Fatal(err)
		}
		actual, err := json.Marshal(schema.Clone())
		if err != nil {
			t.Fatal(err)
		}
		want, err := json.Marshal(fixture.Schemas[i])
		if err != nil {
			t.Fatal(err)
		}
		if string(actual) != string(want) {
			t.Fatalf("schema got %s want %s", actual, want)
		}
		var archived ToolSchema
		if err := json.Unmarshal(actual, &archived); err != nil {
			t.Fatal(err)
		}
		if err := archived.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	(*schemas[1].InputSchema.Properties)["run_id"] = InputSchema{Type: SchemaNumber}
	if (*schemas[2].InputSchema.Properties)["run_id"].Type != SchemaString || (*WorkflowToolSchemas()[1].InputSchema.Properties)["run_id"].Type != SchemaString {
		t.Fatal("schema mutation leaked")
	}
}

func TestWorkflowToolMaskingAndConstructorAdmission(t *testing.T) {
	original, err := DecodeToolInput(ToolWorkflow, []byte(`{"definition":{"secret":"secret","nodes":[{"id":"secret"}]},"args":{"secret":["secret",true,1,1.0,null]}}`))
	if err != nil {
		t.Fatal(err)
	}
	masked := MapToolInputStrings(original, func(s string) string { return strings.ReplaceAll(s, "secret", "hidden") })
	wire, err := masked.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(wire), "secret") {
		t.Fatal("recording leaked secret", string(wire))
	}
	before, err := original.CanonicalJSON()
	if err != nil || !strings.Contains(before, "secret") {
		t.Fatal("mask mutated original", before, err)
	}
	reference := WorkflowCancelToolInput(WorkflowReferenceInput{RunID: "secret"})
	maskedRef, _ := MapToolInputStrings(reference, func(string) string { return "hidden" }).WorkflowReference()
	if maskedRef.RunID != "hidden" {
		t.Fatal(maskedRef)
	}
	originalRef, _ := reference.WorkflowReference()
	if originalRef.RunID != "secret" {
		t.Fatal("reference mutated")
	}
	invalid := WorkflowToolInput(WorkflowInput{})
	if invalid.Validate() == nil {
		t.Fatal("null constructor fields admitted")
	}
	if _, err := invalid.MarshalJSON(); err == nil {
		t.Fatal("invalid payload serialized")
	}
	if _, err := invalid.CanonicalJSON(); err == nil {
		t.Fatal("invalid identity hashed")
	}
}
