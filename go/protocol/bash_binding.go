package protocol

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

type BashBindingFailureKind string

const (
	BashUnexpectedParameter BashBindingFailureKind = "unexpected_parameter"
	BashMissingCommand      BashBindingFailureKind = "missing_command"
)

// BashBindingFailure is a named invocation error, not executable arguments.
// It is settled by the tool gate after hooks/permission and before any handler.
type BashBindingFailure struct {
	kind      BashBindingFailureKind
	parameter string
}

func (failure BashBindingFailure) Kind() BashBindingFailureKind { return failure.kind }
func (failure BashBindingFailure) Parameter() string            { return failure.parameter }
func (failure BashBindingFailure) Error() string {
	if failure.kind == BashMissingCommand {
		return "_bash() missing 1 required positional argument: 'command'"
	}
	return fmt.Sprintf("_bash() got an unexpected keyword argument '%s'", failure.parameter)
}
func (failure BashBindingFailure) Validate() error {
	switch failure.kind {
	case BashMissingCommand, BashUnexpectedParameter:
		return nil
	default:
		return errors.New("invalid Bash binding failure")
	}
}
func (input ToolInput) BashBindingFailure() (BashBindingFailure, bool) {
	if input.name != ToolBash || input.bashBinding == nil {
		return BashBindingFailure{}, false
	}
	return *input.bashBinding, true
}

// DecodeModelToolInput retains finite default-Bash invocation errors from model
// replies. Strict explicit input admission continues to use DecodeToolInput.
// Known fields stay typed; only unrecognized parameters enter immutable extras.
func DecodeModelToolInput(name ToolName, data []byte) (ToolInput, error) {
	if name != ToolBash {
		return DecodeToolInput(name, data)
	}
	if len(data) > MaxWireBytes {
		return ToolInput{}, errors.New("bash model input exceeds wire bound")
	}
	object, err := jsonvalue.Decode(string(data))
	if err != nil {
		return ToolInput{}, err
	}
	if object.Kind() != jsonvalue.Object {
		return DecodeToolInput(name, data)
	}
	known, extra := []jsonvalue.Field{}, []jsonvalue.Field{}
	for _, key := range object.Keys() {
		value, _ := object.Lookup(key)
		switch key {
		case "command", "run_in_background", "approval_prefix":
			known = append(known, jsonvalue.Field{Name: key, Value: value})
		default:
			extra = append(extra, jsonvalue.Field{Name: key, Value: value})
		}
	}
	encoded, err := jsonvalue.ObjectValue(known).MarshalJSON()
	if err != nil {
		return ToolInput{}, err
	}
	var wire struct {
		Command    *string   `json:"command"`
		Background *bool     `json:"run_in_background"`
		Prefix     *[]string `json:"approval_prefix"`
	}
	if err := json.Unmarshal(encoded, &wire); err != nil {
		return ToolInput{}, err
	}
	_, commandPresent := object.Lookup("command")
	if commandPresent && wire.Command == nil {
		return DecodeToolInput(name, data)
	} // known null/type policy is still strict
	if len(extra) == 0 && commandPresent {
		return DecodeToolInput(name, data)
	}
	input := BashToolInput(BashInput{RunInBackground: wire.Background, ApprovalPrefix: wire.Prefix})
	if wire.Command != nil {
		input.bash.Command = *wire.Command
	}
	input.bashCommandMissing = !commandPresent
	input.bashExtra = jsonvalue.ObjectValue(extra)
	failure := BashBindingFailure{kind: BashMissingCommand}
	if len(extra) > 0 {
		failure = BashBindingFailure{kind: BashUnexpectedParameter, parameter: extra[0].Name}
	}
	input.bashBinding = &failure
	if err := json.Unmarshal(encoded, &input.nulls); err != nil {
		return ToolInput{}, err
	}
	return input, input.Validate()
}
func (input ToolInput) marshalBashBinding() ([]byte, error) {
	var command *string
	if !input.bashCommandMissing {
		value := input.bash.Command
		command = &value
	}
	return marshalMetadataObject(struct {
		Command    *string                 `json:"command,omitempty"`
		Background *wireOptional[bool]     `json:"run_in_background,omitempty"`
		Prefix     *wireOptional[[]string] `json:"approval_prefix,omitempty"`
	}{command, optionalWire(input.bash.RunInBackground, input.nulls.Background), optionalWire(input.bash.ApprovalPrefix, input.nulls.ApprovalPrefix)}, input.bashExtra)
}
