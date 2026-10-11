package protocol

import (
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"testing"
)

type schemaProperty struct {
	Type       string                    `json:"type"`
	Enum       []string                  `json:"enum"`
	Items      *schemaProperty           `json:"items"`
	Properties map[string]schemaProperty `json:"properties"`
	Required   []string                  `json:"required"`
}

type pythonToolSchema struct {
	Name        ToolName `json:"name"`
	InputSchema struct {
		Type       string                    `json:"type"`
		Properties map[string]schemaProperty `json:"properties"`
		Required   []string                  `json:"required"`
	} `json:"input_schema"`
}

func TestTypedInputsTrackPythonSchema(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-default-tools.json")
	if err != nil {
		t.Fatal(err)
	}
	var schemas []pythonToolSchema
	if err := json.Unmarshal(data, &schemas); err != nil {
		t.Fatal(err)
	}
	expected := []struct {
		name       ToolName
		properties map[string]string
		required   []string
	}{
		{ToolBash, map[string]string{"command": "string", "run_in_background": "boolean", "approval_prefix": "array"}, []string{"command"}},
		{ToolReadFile, map[string]string{"path": "string", "limit": "integer", "offset": "integer"}, []string{"path"}},
		{ToolWriteFile, map[string]string{"path": "string", "content": "string"}, []string{"path", "content"}},
		{ToolEditFile, map[string]string{"path": "string", "old_text": "string", "new_text": "string"}, []string{"path", "old_text", "new_text"}},
		{ToolGlob, map[string]string{"pattern": "string"}, []string{"pattern"}},
		{ToolTodoWrite, map[string]string{"items": "array"}, []string{"items"}},
		{ToolTask, map[string]string{"prompt": "string", "agent_type": "string"}, []string{"prompt"}},
		{ToolLoadSkill, map[string]string{"name": "string", "scope": "string"}, []string{"name"}},
		{ToolCompress, map[string]string{}, nil},
		{ToolAskUser, map[string]string{"question": "string"}, []string{"question"}},
	}
	if len(schemas) != len(expected) {
		t.Fatalf("Python has %d default schemas; Go expects %d", len(schemas), len(expected))
	}
	for i, schema := range schemas {
		want := expected[i]
		if schema.Name != want.name || schema.InputSchema.Type != "object" {
			t.Fatalf("tool %d changed: name=%q type=%q", i, schema.Name, schema.InputSchema.Type)
		}
		gotProps := make(map[string]string, len(schema.InputSchema.Properties))
		for name, property := range schema.InputSchema.Properties {
			gotProps[name] = property.Type
		}
		if !reflect.DeepEqual(gotProps, want.properties) {
			t.Errorf("%s properties: Python=%v Go=%v", schema.Name, gotProps, want.properties)
		}
		gotRequired := append([]string(nil), schema.InputSchema.Required...)
		wantRequired := append([]string(nil), want.required...)
		sort.Strings(gotRequired)
		sort.Strings(wantRequired)
		if !reflect.DeepEqual(gotRequired, wantRequired) {
			t.Errorf("%s required: Python=%v Go=%v", schema.Name, gotRequired, wantRequired)
		}
	}
	todo := schemas[5].InputSchema.Properties["items"].Items
	if todo == nil || todo.Type != "object" || !reflect.DeepEqual(todo.Required, []string{"content", "status", "activeForm"}) {
		t.Fatal("TodoWrite item shape drifted")
	}
	if !reflect.DeepEqual(todo.Properties["status"].Enum, []string{"pending", "in_progress", "completed"}) {
		t.Fatal("TodoWrite status enum drifted")
	}
	if !reflect.DeepEqual(schemas[6].InputSchema.Properties["agent_type"].Enum, []string{"Explore", "general-purpose"}) {
		t.Fatal("task agent_type enum drifted")
	}
	if !reflect.DeepEqual(schemas[7].InputSchema.Properties["scope"].Enum, []string{"agent", "user"}) {
		t.Fatal("load_skill scope enum drifted")
	}
}
