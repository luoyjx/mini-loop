package protocol

import (
	"errors"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

// ExtensionToolSchema preserves caller-supplied schema metadata as immutable,
// finite JSON. It does not resolve references or validate executable arguments;
// the explicitly registered named-input codec owns that separate contract.
func ExtensionToolSchema(name ToolName, description string, data []byte) (ToolSchema, error) {
	if len(data) > MaxProjectionBytes {
		return ToolSchema{}, errors.New("extension schema exceeds projection byte limit")
	}
	value, err := jsonvalue.Decode(string(data))
	if err != nil {
		return ToolSchema{}, err
	}
	value = value.Sorted()
	result := ToolSchema{Name: name, Description: description, InputSchema: InputSchema{extensionSchema: &value}}
	return result, result.Validate()
}

func extensionSchemaName(name ToolName) bool {
	input, err := DecodeModelToolInput(name, []byte("{}"))
	if err != nil {
		return false
	}
	_, unknown := input.UnknownModelToolObservation()
	return unknown
}
