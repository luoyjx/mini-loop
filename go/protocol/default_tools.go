package protocol

import (
	"bytes"
	_ "embed"
	"encoding/json"
)

// These production descriptors mirror the Python defaults. Differential tests
// compare them with the independently exported contract, not a runtime fixture.
//
//go:embed default_tools.json
var defaultToolData []byte

var defaultTools = loadDefaultTools()

func loadDefaultTools() map[ToolName]ToolSchema {
	var tools []ToolSchema
	decoder := json.NewDecoder(bytes.NewReader(defaultToolData))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&tools); err != nil {
		panic(err)
	}
	result := make(map[ToolName]ToolSchema, len(tools))
	for _, schema := range tools {
		if err := schema.Validate(); err != nil {
			panic(err)
		}
		if _, exists := result[schema.Name]; exists {
			panic("duplicate default tool schema")
		}
		result[schema.Name] = schema
	}
	return result
}

func DefaultToolSchema(name ToolName) (ToolSchema, bool) {
	schema, ok := defaultTools[name]
	return schema.Clone(), ok
}
