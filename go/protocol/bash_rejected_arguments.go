package protocol

import (
	"errors"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

type BashArgumentKind string

const (
	BashArgumentArray   BashArgumentKind = "list"
	BashArgumentNull    BashArgumentKind = "NoneType"
	BashArgumentText    BashArgumentKind = "str"
	BashArgumentInteger BashArgumentKind = "int"
	BashArgumentFloat   BashArgumentKind = "float"
	BashArgumentBoolean BashArgumentKind = "bool"
)

// BashRejectedArguments is a non-mapping observation, never handler args.
// The immutable closed JSON tree retains unsupported array elements and scalars;
// known executable Bash fields still belong exclusively to BashInput.
type BashRejectedArguments struct {
	kind        BashArgumentKind
	observation jsonvalue.Value
}

func rejectedBashArguments(value jsonvalue.Value) (BashRejectedArguments, error) {
	var kind BashArgumentKind
	switch value.Kind() {
	case jsonvalue.Array:
		kind = BashArgumentArray
	case jsonvalue.Null:
		kind = BashArgumentNull
	case jsonvalue.Text:
		kind = BashArgumentText
	case jsonvalue.Integer:
		kind = BashArgumentInteger
	case jsonvalue.Float:
		kind = BashArgumentFloat
	case jsonvalue.Boolean:
		kind = BashArgumentBoolean
	default:
		return BashRejectedArguments{}, errors.New("rejected Bash arguments must be non-mapping")
	}
	if _, err := value.MarshalLegacyUTF8(); err != nil {
		return BashRejectedArguments{}, err
	}
	return BashRejectedArguments{kind: kind, observation: value}, nil
}

func (args BashRejectedArguments) Validate() error {
	next, err := rejectedBashArguments(args.observation)
	if err == nil && args.kind != next.kind {
		return errors.New("rejected Bash argument kind mismatch")
	}
	return err
}
func (args BashRejectedArguments) MarshalJSON() ([]byte, error) {
	if err := args.Validate(); err != nil {
		return nil, err
	}
	return args.observation.MarshalJSON()
}
func (args BashRejectedArguments) mapStrings(mask func(string) string) BashRejectedArguments {
	args.observation = args.observation.MapStrings(mask)
	return args
}
