package protocol

import (
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

type unsupportedToolNameError ToolName

func (name unsupportedToolNameError) Error() string {
	return fmt.Sprintf("unsupported tool %q", ToolName(name))
}

// UnknownModelToolObservation is inert finite model data. A missing native
// codec never supplies an executable argument variant to a custom handler.
type UnknownModelToolObservation struct {
	arguments jsonvalue.Value
}

func (observation UnknownModelToolObservation) validate(name ToolName) error {
	if !utf8.ValidString(string(name)) || len(name) > MaxWireBytes {
		return errors.New("unknown tool name exceeds the native text boundary")
	}
	_, err := DecodeToolInput(name, nil)
	var unsupported unsupportedToolNameError
	if !errors.As(err, &unsupported) {
		return errors.New("unknown observation requires a name without a native codec")
	}
	encoded, err := observation.arguments.MarshalJSON()
	if err == nil && len(encoded) > MaxWireBytes {
		return errors.New("unknown tool observation exceeds wire bound")
	}
	return err
}

func decodeUnknownModelTool(name ToolName, data []byte) (ToolInput, error) {
	if len(data) > MaxWireBytes {
		return ToolInput{}, errors.New("unknown tool observation exceeds wire bound")
	}
	arguments, err := jsonvalue.Decode(string(data))
	if err != nil {
		return ToolInput{}, err
	}
	input := ToolInput{name: name, unknownModelTool: &UnknownModelToolObservation{arguments: arguments}}
	return input, input.Validate()
}

func (input ToolInput) UnknownModelToolObservation() (UnknownModelToolObservation, bool) {
	if input.unknownModelTool == nil {
		return UnknownModelToolObservation{}, false
	}
	return *input.unknownModelTool, true
}
