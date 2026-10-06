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
	SchemaNull    SchemaType = "null"
	SchemaNumber  SchemaType = "number"
)

type SchemaProperties map[string]InputSchema

// SchemaAdditional is a closed bool-or-schema variant, including explicit false.
type SchemaAdditional struct {
	allowed *bool
	schema  *InputSchema
}

func AdditionalAllowed(v bool) *SchemaAdditional { return &SchemaAdditional{allowed: &v} }
func AdditionalSchema(v InputSchema) *SchemaAdditional {
	x := v.Clone()
	return &SchemaAdditional{schema: &x}
}
func (v SchemaAdditional) Rule() (InputSchema, bool) {
	if v.schema == nil {
		return InputSchema{}, false
	}
	return v.schema.Clone(), true
}
func (v SchemaAdditional) Allowed() (bool, bool) {
	if v.allowed == nil {
		return false, false
	}
	return *v.allowed, true
}
func (v SchemaAdditional) MarshalJSON() ([]byte, error) {
	if v.allowed != nil && v.schema == nil {
		return json.Marshal(*v.allowed)
	}
	if v.schema != nil && v.allowed == nil {
		return json.Marshal(v.schema)
	}
	return nil, errors.New("invalid additional-properties variant")
}
func (v *SchemaAdditional) UnmarshalJSON(b []byte) error {
	if string(b) == "true" || string(b) == "false" {
		var flag bool
		if e := json.Unmarshal(b, &flag); e != nil {
			return e
		}
		*v = *AdditionalAllowed(flag)
		return nil
	}
	var schema InputSchema
	if e := json.Unmarshal(b, &schema); e != nil {
		return e
	}
	*v = *AdditionalSchema(schema)
	return nil
}

// InputSchema describes the closed source schema language. Multiple Types,
// OneOf and Additional cover decision questions without erasing their contract.
type InputSchema struct {
	Additional    *SchemaAdditional `json:"additionalProperties,omitempty"`
	Const         *string           `json:"const,omitempty"`
	Description   string            `json:"description,omitempty"`
	Enum          []string          `json:"enum,omitempty"`
	Items         *InputSchema      `json:"items,omitempty"`
	MaxItems      *int              `json:"maxItems,omitempty"`
	MaxProperties *int              `json:"maxProperties,omitempty"`
	MinItems      *int              `json:"minItems,omitempty"`
	MinProperties *int              `json:"minProperties,omitempty"`
	OneOf         []InputSchema     `json:"oneOf,omitempty"`
	Properties    *SchemaProperties `json:"properties,omitempty"`
	Required      []string          `json:"required,omitempty"`
	Type          SchemaType        `json:"type,omitempty"`
	Types         []SchemaType      `json:"-"`
}
type schemaTypeWire struct {
	single   SchemaType
	multiple []SchemaType
}

func (v schemaTypeWire) MarshalJSON() ([]byte, error) {
	if v.multiple != nil {
		return json.Marshal(v.multiple)
	}
	return json.Marshal(v.single)
}
func (v *schemaTypeWire) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '[' {
		return json.Unmarshal(b, &v.multiple)
	}
	return json.Unmarshal(b, &v.single)
}
func (schema InputSchema) MarshalJSON() ([]byte, error) {
	type fields InputSchema
	var typ *schemaTypeWire
	if schema.Type != "" {
		typ = &schemaTypeWire{single: schema.Type}
	} else if schema.Types != nil {
		typ = &schemaTypeWire{multiple: schema.Types}
	}
	// The embedded fields keep source sorted key order; the shadow type follows
	// required as the final alphabetical schema member.
	return json.Marshal(struct {
		fields
		Type *schemaTypeWire `json:"type,omitempty"`
	}{fields(schema), typ})
}
func (schema *InputSchema) UnmarshalJSON(b []byte) error {
	type fields InputSchema
	var wire struct {
		fields
		Type *schemaTypeWire `json:"type"`
	}
	if e := decodeStrict(b, &wire); e != nil {
		return e
	}
	out := InputSchema(wire.fields)
	if wire.Type != nil {
		out.Type = wire.Type.single
		out.Types = wire.Type.multiple
	}
	*schema = out
	return nil
}
func (schema InputSchema) Clone() InputSchema {
	schema.Enum = append([]string(nil), schema.Enum...)
	schema.Required = append([]string(nil), schema.Required...)
	if schema.Types != nil {
		schema.Types = append([]SchemaType{}, schema.Types...)
	}
	if schema.Const != nil {
		v := *schema.Const
		schema.Const = &v
	}
	for _, p := range []**int{&schema.MinItems, &schema.MaxItems, &schema.MinProperties, &schema.MaxProperties} {
		if *p != nil {
			v := **p
			*p = &v
		}
	}
	if schema.Items != nil {
		v := schema.Items.Clone()
		schema.Items = &v
	}
	if schema.Properties != nil {
		v := make(SchemaProperties, len(*schema.Properties))
		for k, p := range *schema.Properties {
			v[k] = p.Clone()
		}
		schema.Properties = &v
	}
	if schema.Additional != nil {
		v := *schema.Additional
		if v.allowed != nil {
			x := *v.allowed
			v.allowed = &x
		}
		if v.schema != nil {
			x := v.schema.Clone()
			v.schema = &x
		}
		schema.Additional = &v
	}
	if schema.OneOf != nil {
		v := make([]InputSchema, len(schema.OneOf))
		for i, p := range schema.OneOf {
			v[i] = p.Clone()
		}
		schema.OneOf = v
	}
	return schema
}
func (schema InputSchema) Validate() error { return schema.validate(0) }
func (schema InputSchema) validate(depth int) error {
	if depth > 32 {
		return errors.New("tool schema exceeds supported depth")
	}
	if schema.Type != "" && schema.Types != nil {
		return errors.New("schema type cannot be both scalar and union")
	}
	if schema.Types != nil && len(schema.Types) < 1 {
		return errors.New("empty schema type union")
	}
	types := schema.Types
	if types == nil && schema.Type != "" {
		types = []SchemaType{schema.Type}
	}
	seen := map[SchemaType]bool{}
	for _, typ := range types {
		switch typ {
		case SchemaObject, SchemaArray, SchemaString, SchemaInteger, SchemaBoolean, SchemaNull, SchemaNumber:
		default:
			return fmt.Errorf("unsupported schema type %q", typ)
		}
		if seen[typ] {
			return errors.New("duplicate schema union type")
		}
		seen[typ] = true
	}
	if len(types) == 0 && len(schema.OneOf) == 0 && schema.Const == nil {
		return errors.New("schema requires a type, const or oneOf")
	}
	object, array := seen[SchemaObject], seen[SchemaArray]
	if schema.Properties != nil {
		if !object || *schema.Properties == nil {
			return errors.New("object schema requires nonnull properties")
		}
		for k, p := range *schema.Properties {
			if k == "" {
				return errors.New("schema property requires a name")
			}
			if e := p.validate(depth + 1); e != nil {
				return fmt.Errorf("property %s: %w", k, e)
			}
		}
	}
	if schema.Type == SchemaObject && schema.Properties == nil && schema.Additional == nil {
		return errors.New("object schema requires properties or explicit additional properties")
	}
	if schema.Items != nil {
		if !array {
			return errors.New("items requires array schema")
		}
		if e := schema.Items.validate(depth + 1); e != nil {
			return e
		}
	} else if schema.Type == SchemaArray {
		return errors.New("array schema requires items")
	}
	if len(schema.Enum) != 0 && schema.Type != SchemaString {
		return errors.New("enum requires a string schema")
	}
	if schema.Additional != nil {
		if !object {
			return errors.New("additional properties requires object schema")
		}
		a := schema.Additional
		if (a.allowed == nil) == (a.schema == nil) {
			return errors.New("invalid additional-properties variant")
		}
		if a.schema != nil {
			if e := a.schema.validate(depth + 1); e != nil {
				return e
			}
		}
	}
	required := map[string]bool{}
	for _, k := range schema.Required {
		if schema.Properties == nil || required[k] {
			return errors.New("required schema properties must exist and be unique")
		}
		if _, ok := (*schema.Properties)[k]; !ok {
			return errors.New("required schema properties must exist and be unique")
		}
		required[k] = true
	}
	for _, pair := range []struct {
		min, max *int
		valid    bool
	}{{schema.MinItems, schema.MaxItems, array}, {schema.MinProperties, schema.MaxProperties, object}} {
		if (pair.min != nil || pair.max != nil) && !pair.valid {
			return errors.New("schema bounds require matching container type")
		}
		if (pair.min != nil && *pair.min < 0) || (pair.max != nil && *pair.max < 0) || (pair.min != nil && pair.max != nil && *pair.min > *pair.max) {
			return errors.New("schema bounds must be nonnegative and ordered")
		}
	}
	for _, p := range schema.OneOf {
		if e := p.validate(depth + 1); e != nil {
			return e
		}
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
	PurposeAgentTurn       RequestPurpose = "agent_turn"
	PurposeCompaction      RequestPurpose = "compaction"
	PurposeDecision        RequestPurpose = "decision"
	PurposeMemorySelection RequestPurpose = "memory_selection"
)

// ModelRequest is detached from session state. Purpose is local provenance,
// not an extra field sent to an Anthropic-compatible endpoint. Nil system or
// tools means absent, as in Python's summary request.
type ModelRequest struct {
	Model     string           `json:"model"`
	MaxTokens int              `json:"max_tokens"`
	Messages  []Message        `json:"messages"`
	System    *string          `json:"system,omitempty"`
	Tools     []ToolSchema     `json:"tools,omitempty"`
	Purpose   RequestPurpose   `json:"-"`
	Cache     CacheAnnotations `json:"-"`
}

func (request ModelRequest) MarshalJSON() ([]byte, error) {
	wire, err := request.Wire()
	if err != nil {
		return nil, err
	}
	return json.Marshal(wire)
}

func (request ModelRequest) Clone() ModelRequest {
	request.Cache = request.Cache.Clone()
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
	if err := request.Cache.Validate(request); err != nil {
		return err
	}
	if request.Model == "" || request.MaxTokens < 1 || len(request.Messages) == 0 {
		return errors.New("model request requires model, positive max tokens and messages")
	}
	if request.Purpose != PurposeAgentTurn && request.Purpose != PurposeCompaction && request.Purpose != PurposeDecision && request.Purpose != PurposeMemorySelection {
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
