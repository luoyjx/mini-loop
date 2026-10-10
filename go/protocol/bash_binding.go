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
	BashNonMappingArguments BashBindingFailureKind = "non_mapping_arguments"
)

// BashBindingFailure is a named invocation error, not executable arguments.
// It is settled by the tool gate after hooks/permission and before any handler.
type BashBindingFailure struct {
	kind         BashBindingFailureKind
	parameter    string
	argumentKind BashArgumentKind
}

func (failure BashBindingFailure) Kind() BashBindingFailureKind { return failure.kind }
func (failure BashBindingFailure) Parameter() string            { return failure.parameter }
func (failure BashBindingFailure) Error() string {
	if failure.kind == BashNonMappingArguments {
		return fmt.Sprintf("mini_loop.registry.Tool.run() argument after ** must be a mapping, not %s", failure.argumentKind)
	}
	if failure.kind == BashMissingCommand {
		return "_bash() missing 1 required positional argument: 'command'"
	}
	return fmt.Sprintf("_bash() got an unexpected keyword argument '%s'", failure.parameter)
}
func (failure BashBindingFailure) Validate() error {
	switch failure.kind {
	case BashNonMappingArguments:
		switch failure.argumentKind {
		case BashArgumentArray, BashArgumentNull, BashArgumentText, BashArgumentInteger, BashArgumentFloat, BashArgumentBoolean:
			return nil
		}
		return errors.New("invalid non-mapping Bash argument kind")
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

// DecodeModelToolInput retains finite Bash/background invocation errors from
// model replies. Strict explicit admission continues to use DecodeToolInput.
// Known fields stay typed; only unrecognized parameters enter immutable extras.
func DecodeModelToolInput(name ToolName, data []byte) (ToolInput, error) {
	if name == ToolBackgroundRun {
		return decodeModelBackgroundInput(data)
	}
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
		rejected, err := rejectedBashArguments(object)
		if err != nil {
			return ToolInput{}, err
		}
		input := BashToolInput(BashInput{})
		input.bashRejected = &rejected
		input.bashBinding = &BashBindingFailure{kind: BashNonMappingArguments, argumentKind: rejected.kind}
		return input, input.Validate()
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
	command, commandPresent := object.Lookup("command")
	text, commandIsText := command.Text()
	var observed *BashCommandObservation
	if commandPresent && !commandIsText {
		value, err := observeBashCommand(command)
		if err != nil {
			return ToolInput{}, err
		}
		observed = &value
	}
	_, backgroundPresent := object.Lookup("run_in_background")
	_, prefixPresent := object.Lookup("approval_prefix")
	if len(extra) == 0 && commandPresent && observed == nil && !backgroundPresent && !prefixPresent {
		return DecodeToolInput(name, data)
	}
	input := BashToolInput(BashInput{})
	input.bashCommand = observed
	if backgroundPresent {
		value, _ := object.Lookup("run_in_background")
		input.bashBackground = &BashBackgroundRequest{observed: value}
	}
	if prefixPresent {
		value, _ := object.Lookup("approval_prefix")
		input.bashApproval = &BashApprovalProposal{observed: value}
	}
	input.bash.Command = text
	input.bashCommandMissing = !commandPresent
	input.bashExtra = jsonvalue.ObjectValue(extra)
	if !commandPresent || len(extra) > 0 {
		failure := BashBindingFailure{kind: BashMissingCommand}
		if len(extra) > 0 {
			failure = BashBindingFailure{kind: BashUnexpectedParameter, parameter: extra[0].Name}
		}
		input.bashBinding = &failure
	}
	if err := json.Unmarshal(encoded, &input.nulls); err != nil {
		return ToolInput{}, err
	}
	return input, input.Validate()
}
func (input ToolInput) marshalBashBinding() ([]byte, error) {
	if input.bashRejected != nil {
		return input.bashRejected.MarshalJSON()
	}
	var command *modelBashCommand
	if !input.bashCommandMissing {
		value := modelBashCommand{text: input.bash.Command, observation: input.bashCommand}
		command = &value
	}
	background := input.bashBackground
	if background == nil && (input.bash.RunInBackground != nil || input.nulls.Background) {
		value := jsonvalue.NullValue()
		if input.bash.RunInBackground != nil {
			value = jsonvalue.BoolValue(*input.bash.RunInBackground)
		}
		background = &BashBackgroundRequest{observed: value}
	}
	prefix := input.bashApproval
	if prefix == nil && (input.bash.ApprovalPrefix != nil || input.nulls.ApprovalPrefix) {
		value := jsonvalue.NullValue()
		if input.bash.ApprovalPrefix != nil {
			items := make([]jsonvalue.Value, len(*input.bash.ApprovalPrefix))
			for i, token := range *input.bash.ApprovalPrefix {
				items[i] = jsonvalue.TextValue(token)
			}
			value = jsonvalue.ArrayValue(items)
		}
		prefix = &BashApprovalProposal{observed: value}
	}
	return marshalMetadataObject(struct {
		Command    *modelBashCommand      `json:"command,omitempty"`
		Background *BashBackgroundRequest `json:"run_in_background,omitempty"`
		Prefix     *BashApprovalProposal  `json:"approval_prefix,omitempty"`
	}{command, background, prefix}, input.bashExtra)
}
