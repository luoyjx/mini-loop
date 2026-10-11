package protocol

import (
	"encoding/json"
	"errors"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

const ToolReturnArtifact ToolName = "return_artifact"

type ReturnArtifactInput struct {
	Value jsonvalue.Value `json:"value"`
}

func ReturnArtifactToolInput(value jsonvalue.Value) ToolInput {
	return ToolInput{name: ToolReturnArtifact, returnArtifact: value}
}
func (input ToolInput) ReturnArtifact() (ReturnArtifactInput, bool) {
	return ReturnArtifactInput{Value: input.returnArtifact}, input.name == ToolReturnArtifact
}
func decodeReturnArtifactInput(data []byte) (ToolInput, error) {
	object, err := jsonvalue.Decode(string(data))
	if err != nil {
		return ToolInput{}, err
	}
	value, present := object.Lookup("value")
	if object.Kind() != jsonvalue.Object || !present || len(object.Keys()) != 1 {
		return ToolInput{}, errors.New("return_artifact requires exactly value")
	}
	input := ReturnArtifactToolInput(value)
	return input, input.Validate()
}

// WorkflowArtifactSchema preserves numeric enum/const and all admitted workflow
// schema fields. The controller validates that language before constructing it.
// This variant never changes the ordinary default-tool schema decoder.
func WorkflowArtifactSchema(output jsonvalue.Value) (ToolSchema, error) {
	if output.Kind() != jsonvalue.Object {
		return ToolSchema{}, errors.New("workflow output schema must be an object")
	}
	schema := jsonvalue.ObjectValue([]jsonvalue.Field{
		{Name: "type", Value: jsonvalue.TextValue("object")},
		{Name: "properties", Value: jsonvalue.ObjectValue([]jsonvalue.Field{{Name: "value", Value: output}})},
		{Name: "required", Value: jsonvalue.ArrayValue([]jsonvalue.Value{jsonvalue.TextValue("value")})},
		{Name: "additionalProperties", Value: jsonvalue.BoolValue(false)},
	})
	tool := ToolSchema{Name: ToolReturnArtifact, Description: "Submit the node's final structured result. Call exactly once.", InputSchema: InputSchema{workflowSchema: &schema}}
	return tool, tool.Validate()
}

// Synthetic schemas need their exact closed representation when an archived
// model request/catalogue is read back. Ordinary tools retain the existing parser.
func (schema *ToolSchema) UnmarshalJSON(data []byte) error {
	var wire struct {
		Name        ToolName        `json:"name"`
		Description string          `json:"description"`
		InputSchema jsonvalue.Value `json:"input_schema"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	if IsMCPToolName(wire.Name) {
		decoded, err := MCPToolSchema(wire.Name, wire.Description, wire.InputSchema)
		if err != nil {
			return err
		}
		*schema = decoded
		return nil
	}
	if wire.Name == ToolReturnArtifact {
		value := wire.InputSchema
		decoded := ToolSchema{Name: wire.Name, Description: wire.Description, InputSchema: InputSchema{workflowSchema: &value}}
		if err := decoded.Validate(); err != nil {
			return err
		}
		*schema = decoded
		return nil
	}
	type ordinary ToolSchema
	var decoded ordinary
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*schema = ToolSchema(decoded)
	return nil
}
