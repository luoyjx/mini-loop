package protocol

import (
	"errors"
	"fmt"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

// BackgroundBindingFailure is an invocation failure settled after admission.
// Its observed argument kinds and parameter name grant no shell authority.
type BackgroundBindingFailure struct {
	kind         BashBindingFailureKind
	parameter    string
	argumentKind BashArgumentKind
}

func (failure BackgroundBindingFailure) Error() string {
	if failure.kind == BashNonMappingArguments {
		return fmt.Sprintf("mini_loop.registry.Tool.run() argument after ** must be a mapping, not %s", failure.argumentKind)
	}
	if failure.kind == BashMissingCommand {
		return "install_background.<locals>.background_run() missing 1 required positional argument: 'command'"
	}
	return fmt.Sprintf("install_background.<locals>.background_run() got an unexpected keyword argument '%s'", failure.parameter)
}

// backgroundModelArguments separates observations from the executable typed
// BackgroundRunInput. Unknown parameters are immutable metadata, never argv.
type backgroundModelArguments struct {
	timeout  *BackgroundTimeoutObservation
	approval *BashApprovalProposal
	command  *BashCommandObservation
	failure  *BackgroundBindingFailure
	rejected *BashRejectedArguments
	extra    jsonvalue.Value
	missing  bool
}

func (args backgroundModelArguments) clone() *backgroundModelArguments {
	if args.timeout != nil {
		value := *args.timeout
		args.timeout = &value
	}
	if args.approval != nil {
		value := *args.approval
		args.approval = &value
	}
	if args.command != nil {
		value := *args.command
		args.command = &value
	}
	if args.failure != nil {
		value := *args.failure
		args.failure = &value
	}
	if args.rejected != nil {
		value := *args.rejected
		args.rejected = &value
	}
	return &args
}

func (args backgroundModelArguments) validate() error {
	if args.timeout != nil {
		if _, err := args.timeout.observed.MarshalLegacyUTF8(); err != nil {
			return err
		}
	}
	if args.approval != nil {
		if _, err := args.approval.observed.MarshalLegacyUTF8(); err != nil {
			return err
		}
	}
	if args.command != nil {
		if err := args.command.Validate(); err != nil {
			return err
		}
	}
	if args.failure != nil {
		failure := args.failure
		if err := (BashBindingFailure{kind: failure.kind, parameter: failure.parameter, argumentKind: failure.argumentKind}).Validate(); err != nil {
			return err
		}
		if failure.kind == BashNonMappingArguments {
			if args.rejected == nil || args.rejected.kind != failure.argumentKind {
				return errors.New("background non-mapping failure requires matching arguments")
			}
			return args.rejected.Validate()
		}
	}
	if args.rejected != nil {
		return errors.New("background rejected arguments require non-mapping failure")
	}
	if args.extra.Kind() != jsonvalue.Object {
		return errors.New("background extra parameters must be an object")
	}
	_, err := args.extra.MarshalLegacyUTF8()
	return err
}

func decodeModelBackgroundInput(data []byte) (ToolInput, error) {
	if len(data) > MaxWireBytes {
		return ToolInput{}, errors.New("background model input exceeds wire bound")
	}
	object, err := jsonvalue.Decode(string(data))
	if err != nil {
		return ToolInput{}, err
	}
	args := &backgroundModelArguments{extra: jsonvalue.ObjectValue(nil)}
	if object.Kind() != jsonvalue.Object {
		rejected, err := rejectedBashArguments(object)
		if err != nil {
			return ToolInput{}, err
		}
		args.rejected = &rejected
		args.failure = &BackgroundBindingFailure{kind: BashNonMappingArguments, argumentKind: rejected.kind}
		input := BackgroundRunToolInput(BackgroundRunInput{})
		input.backgroundModel = args
		return input, input.Validate()
	}
	extra := []jsonvalue.Field{}
	for _, key := range object.Keys() {
		value, _ := object.Lookup(key)
		switch key {
		case "command": // Selected below, preserving the observed value separately.
		case "timeout":
			args.timeout = &BackgroundTimeoutObservation{observed: value}
		case "approval_prefix":
			args.approval = &BashApprovalProposal{observed: value}
		default:
			extra = append(extra, jsonvalue.Field{Name: key, Value: value})
		}
	}
	command, present := object.Lookup("command")
	text, isText := command.Text()
	args.missing = !present
	if present && !isText {
		value, err := observeBashCommand(command)
		if err != nil {
			return ToolInput{}, err
		}
		args.command = &value
	}
	input := BackgroundRunToolInput(BackgroundRunInput{Command: text})
	args.extra = jsonvalue.ObjectValue(extra)
	if len(extra) > 0 {
		args.failure = &BackgroundBindingFailure{kind: BashUnexpectedParameter, parameter: extra[0].Name}
	} else if !present {
		args.failure = &BackgroundBindingFailure{kind: BashMissingCommand}
	}
	input.backgroundModel = args
	return input, input.Validate()
}

func (input ToolInput) BackgroundBindingFailure() (BackgroundBindingFailure, bool) {
	if input.name != ToolBackgroundRun || input.backgroundModel == nil || input.backgroundModel.failure == nil {
		return BackgroundBindingFailure{}, false
	}
	return *input.backgroundModel.failure, true
}

func (input ToolInput) BackgroundCommandObservation() (BashCommandObservation, bool) {
	if input.name != ToolBackgroundRun || input.backgroundModel == nil || input.backgroundModel.command == nil {
		return BashCommandObservation{}, false
	}
	return *input.backgroundModel.command, true
}

func (input ToolInput) marshalBackgroundModel() ([]byte, error) {
	value, err := input.backgroundObservation()
	if err != nil {
		return nil, err
	}
	return value.MarshalJSON()
}

func (input ToolInput) backgroundObservation() (jsonvalue.Value, error) {
	if err := input.Validate(); err != nil {
		return jsonvalue.Value{}, err
	}
	args := input.backgroundModel
	if args.rejected != nil {
		return args.rejected.observation, nil
	}
	plain := input
	plain.backgroundModel = nil
	encoded, err := plain.marshalOptionalJSON()
	if err != nil {
		return jsonvalue.Value{}, err
	}
	object, err := jsonvalue.Decode(string(encoded))
	if err != nil {
		return jsonvalue.Value{}, err
	}
	fields := []jsonvalue.Field{}
	for _, key := range object.Keys() {
		value, _ := object.Lookup(key)
		if key == "command" {
			if args.missing {
				continue
			}
			if args.command != nil {
				value = args.command.observed
			}
		}
		fields = append(fields, jsonvalue.Field{Name: key, Value: value})
	}
	if args.timeout != nil {
		fields = append(fields, jsonvalue.Field{Name: "timeout", Value: args.timeout.observed})
	}
	if args.approval != nil {
		fields = append(fields, jsonvalue.Field{Name: "approval_prefix", Value: args.approval.observed})
	}
	for _, key := range args.extra.Keys() {
		value, _ := args.extra.Lookup(key)
		fields = append(fields, jsonvalue.Field{Name: key, Value: value})
	}
	return jsonvalue.ObjectValue(fields), nil
}

func (args *backgroundModelArguments) mask(mask func(string) string) {
	if args.timeout != nil {
		args.timeout.observed = args.timeout.observed.MapStrings(mask)
	}
	if args.approval != nil {
		args.approval.observed = args.approval.observed.MapStrings(mask)
	}
	if args.command != nil {
		args.command.observed = args.command.observed.MapStrings(mask)
	}
	if args.failure != nil {
		args.failure.parameter = mask(args.failure.parameter)
	}
	if args.rejected != nil {
		*args.rejected = args.rejected.mapStrings(mask)
	}
	args.extra = args.extra.MapStrings(mask)
}
