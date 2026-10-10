package protocol

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

type BashCommandKind string

const (
	BashCommandNull    BashCommandKind = "NoneType"
	BashCommandBoolean BashCommandKind = "bool"
	BashCommandInteger BashCommandKind = "int"
	BashCommandFloat   BashCommandKind = "float"
	BashCommandArray   BashCommandKind = "list"
	BashCommandObject  BashCommandKind = "dict"
)

type BashCommandOperation string

const (
	BashCommandSplit BashCommandOperation = "split"
	BashCommandLower BashCommandOperation = "lower"
)

// BashCommandObservation retains non-text model input, never shell text.
// PolicyString is the Source str(value) projection for permission, approval and
// display consumers. Native handlers settle a named failure before shell argv.
type BashCommandObservation struct {
	kind     BashCommandKind
	observed jsonvalue.Value
}

func observeBashCommand(value jsonvalue.Value) (BashCommandObservation, error) {
	var kind BashCommandKind
	switch value.Kind() {
	case jsonvalue.Null:
		kind = BashCommandNull
	case jsonvalue.Boolean:
		kind = BashCommandBoolean
	case jsonvalue.Integer:
		kind = BashCommandInteger
	case jsonvalue.Float:
		kind = BashCommandFloat
	case jsonvalue.Array:
		kind = BashCommandArray
	case jsonvalue.Object:
		kind = BashCommandObject
	default:
		return BashCommandObservation{}, errors.New("non-text Bash command observation required")
	}
	if _, err := value.MarshalLegacyUTF8(); err != nil {
		return BashCommandObservation{}, err
	}
	return BashCommandObservation{kind: kind, observed: value}, nil
}
func (command BashCommandObservation) Kind() BashCommandKind { return command.kind }
func (command BashCommandObservation) PolicyString() string  { return command.observed.PythonString() }
func (command BashCommandObservation) Validate() error {
	next, err := observeBashCommand(command.observed)
	if err == nil && next.kind != command.kind {
		return errors.New("Bash command observation kind mismatch")
	}
	return err
}
func (command BashCommandObservation) MarshalJSON() ([]byte, error) {
	if err := command.Validate(); err != nil {
		return nil, err
	}
	return command.observed.MarshalJSON()
}
func (command BashCommandObservation) Failure(operation BashCommandOperation) error {
	if err := command.Validate(); err != nil {
		return err
	}
	if operation != BashCommandSplit && operation != BashCommandLower {
		return errors.New("unsupported Bash command operation")
	}
	return BashCommandFailure{kind: command.kind, operation: operation}
}

type BashCommandFailure struct {
	kind      BashCommandKind
	operation BashCommandOperation
}

func (failure BashCommandFailure) Error() string {
	return fmt.Sprintf("'%s' object has no attribute '%s'", failure.kind, failure.operation)
}
func (input ToolInput) BashCommandObservation() (BashCommandObservation, bool) {
	if input.name != ToolBash || input.bashCommand == nil {
		return BashCommandObservation{}, false
	}
	return *input.bashCommand, true
}

// A concrete wire-only text/observation union preserves known command values.
type modelBashCommand struct {
	text        string
	observation *BashCommandObservation
}

func (command modelBashCommand) MarshalJSON() ([]byte, error) {
	if command.observation != nil {
		return command.observation.MarshalJSON()
	}
	return json.Marshal(command.text)
}
