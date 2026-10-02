package protocol

import (
	"encoding/json"
	"errors"
	"fmt"
)

type SchemaType string

const (
	SchemaObject  SchemaType = "object"
	SchemaArray   SchemaType = "array"
	SchemaString  SchemaType = "string"
	SchemaInteger SchemaType = "integer"
	SchemaBoolean SchemaType = "boolean"
)

type SchemaProperties map[string]InputSchema

// InputSchema describes the supported default-tool schema language. Properties
// is present for objects, including compress's empty object, and absent for
// scalar/array nodes. JSON is decoded into this concrete tree at the boundary.
// Field order is canonical so catalogue fingerprints match Python sort_keys.
type InputSchema struct {
	Description string            `json:"description,omitempty"`
	Enum        []string          `json:"enum,omitempty"`
	Items       *InputSchema      `json:"items,omitempty"`
	Properties  *SchemaProperties `json:"properties,omitempty"`
	Required    []string          `json:"required,omitempty"`
	Type        SchemaType        `json:"type"`
}

func (schema InputSchema) Clone() InputSchema {
	schema.Enum = append([]string(nil), schema.Enum...)
	schema.Required = append([]string(nil), schema.Required...)
	if schema.Items != nil {
		value := schema.Items.Clone()
		schema.Items = &value
	}
	if schema.Properties != nil {
		value := make(SchemaProperties, len(*schema.Properties))
		for key, property := range *schema.Properties {
			value[key] = property.Clone()
		}
		schema.Properties = &value
	}
	return schema
}

func (schema InputSchema) Validate() error { return schema.validate(0) }
func (schema InputSchema) validate(depth int) error {
	if depth > 32 {
		return errors.New("tool schema exceeds supported depth")
	}
	switch schema.Type {
	case SchemaObject:
		if schema.Properties == nil || *schema.Properties == nil || schema.Items != nil || len(schema.Enum) != 0 {
			return errors.New("object schema requires properties and no items/enum")
		}
		for key, property := range *schema.Properties {
			if key == "" {
				return errors.New("schema property requires a name")
			}
			if err := property.validate(depth + 1); err != nil {
				return fmt.Errorf("property %s: %w", key, err)
			}
		}
		seen := make(map[string]bool)
		for _, key := range schema.Required {
			if _, ok := (*schema.Properties)[key]; !ok || seen[key] {
				return errors.New("required schema properties must exist and be unique")
			}
			seen[key] = true
		}
	case SchemaArray:
		if schema.Items == nil || schema.Properties != nil || len(schema.Required) != 0 || len(schema.Enum) != 0 {
			return errors.New("array schema requires items only")
		}
		return schema.Items.validate(depth + 1)
	case SchemaString, SchemaInteger, SchemaBoolean:
		if schema.Items != nil || schema.Properties != nil || len(schema.Required) != 0 || (schema.Type != SchemaString && len(schema.Enum) != 0) {
			return errors.New("invalid scalar schema")
		}
	default:
		return fmt.Errorf("unsupported schema type %q", schema.Type)
	}
	return nil
}

type ToolSchema struct {
	Description string      `json:"description"`
	InputSchema InputSchema `json:"input_schema"`
	Name        ToolName    `json:"name"`
}

func (schema ToolSchema) Clone() ToolSchema {
	schema.InputSchema = schema.InputSchema.Clone()
	return schema
}
func (schema ToolSchema) Validate() error {
	if schema.Name == "" {
		return errors.New("tool schema requires a name")
	}
	return schema.InputSchema.Validate()
}

type RequestPurpose string

const (
	PurposeAgentTurn  RequestPurpose = "agent_turn"
	PurposeCompaction RequestPurpose = "compaction"
)

// ModelRequest is detached from session state. Purpose is local provenance,
// not an extra field sent to an Anthropic-compatible endpoint. Nil system or
// tools means absent, as in Python's summary request.
type ModelRequest struct {
	Model     string         `json:"model"`
	MaxTokens int            `json:"max_tokens"`
	Messages  []Message      `json:"messages"`
	System    *string        `json:"system,omitempty"`
	Tools     []ToolSchema   `json:"tools,omitempty"`
	Purpose   RequestPurpose `json:"-"`
}

func (request ModelRequest) MarshalJSON() ([]byte, error) {
	// Preserve absent tools separately from an explicitly empty fitted
	// catalogue, just as Python's _create does at the provider boundary.
	var tools *[]ToolSchema
	if request.Tools != nil {
		tools = &request.Tools
	}
	return json.Marshal(struct {
		Model     string        `json:"model"`
		MaxTokens int           `json:"max_tokens"`
		Messages  []Message     `json:"messages"`
		System    *string       `json:"system,omitempty"`
		Tools     *[]ToolSchema `json:"tools,omitempty"`
	}{request.Model, request.MaxTokens, request.Messages, request.System, tools})
}

func (request ModelRequest) Clone() ModelRequest {
	request.Messages = append([]Message(nil), request.Messages...)
	if request.System != nil {
		value := *request.System
		request.System = &value
	}
	if request.Tools != nil {
		tools := make([]ToolSchema, len(request.Tools))
		for i, schema := range request.Tools {
			tools[i] = schema.Clone()
		}
		request.Tools = tools
	}
	return request
}
func (request ModelRequest) Validate() error {
	if request.Model == "" || request.MaxTokens < 1 || len(request.Messages) == 0 {
		return errors.New("model request requires model, positive max tokens and messages")
	}
	if request.Purpose != PurposeAgentTurn && request.Purpose != PurposeCompaction {
		return errors.New("unsupported model request purpose")
	}
	if err := ValidateTranscript(request.Messages); err != nil {
		return err
	}
	seen := make(map[ToolName]bool)
	for _, schema := range request.Tools {
		if seen[schema.Name] {
			return errors.New("duplicate request tool")
		}
		if err := schema.Validate(); err != nil {
			return err
		}
		seen[schema.Name] = true
	}
	return nil
}
