package protocol

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestMemoryInputsMatchActualSourceAndDetachOptionals(t *testing.T) {
	raw, err := os.ReadFile("../testdata/python-memory-tools.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Schemas []ToolSchema
		Steps   []struct {
			Name      ToolName
			Input     json.RawMessage
			Canonical string
		}
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Steps) != 14 || !reflect.DeepEqual(MemorySchemas(), fixture.Schemas) {
		t.Fatal("source schema/steps differ", fixture.Schemas)
	}
	for _, step := range fixture.Steps {
		input, err := DecodeToolInput(step.Name, step.Input)
		if err != nil {
			t.Fatal(string(step.Input), err)
		}
		if got, err := input.CanonicalJSON(); err != nil || got != step.Canonical {
			t.Fatal(got, step.Canonical, err)
		}
		if v, ok := input.Remember(); ok {
			if v.Description != nil {
				*v.Description = "rebound"
			}
			if v.Type != nil {
				*v.Type = MemoryReference
			}
		}
		if v, ok := input.Recall(); ok && v.Query != nil {
			*v.Query = "rebound"
		}
		wire, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		copy, err := DecodeToolInput(step.Name, wire)
		if err != nil {
			t.Fatal(err)
		}
		if got, _ := copy.CanonicalJSON(); got != step.Canonical {
			t.Fatal("lost null/absence or aliased optional", got)
		}
	}
	for _, name := range []ToolName{ToolRemember, ToolRecall} {
		if _, err := DecodeToolInput(name, []byte(`{"owner":"foreign"}`)); err == nil {
			t.Fatal("model owner admitted", name)
		}
	}
	for _, raw := range []string{`{}`, `{"name":null,"content":"body"}`, `{"name":"x","content":42}`, `{"name":"x","content":"body","type":42}`} {
		if _, err := DecodeToolInput(ToolRemember, []byte(raw)); err == nil {
			t.Fatal("malformed input admitted", raw)
		}
	}
	v, _ := DecodeToolInput(ToolRemember, []byte(`{"name":"secret","content":"secret","description":"secret","type":"secret"}`))
	masked := MapToolInputStrings(v, func(s string) string { return strings.ReplaceAll(s, "secret", "hidden") })
	if wire, _ := json.Marshal(masked); strings.Contains(string(wire), "secret") {
		t.Fatal(string(wire))
	}
	if wire, _ := json.Marshal(v); !strings.Contains(string(wire), "secret") {
		t.Fatal("mask changed live input")
	}
	if len(DefaultToolNames()) != 10 {
		t.Fatal("memory tools became default")
	}
}
