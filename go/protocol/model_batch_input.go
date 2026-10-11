package protocol

import (
	"fmt"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

// ModelBatchInputFailure is an inert failure while preparing the entire batch,
// before any before hook, authority allocation, or handler invocation.
type ModelBatchInputFailure struct {
	kind          modelBatchFailureKind
	rootKind      BashArgumentKind
	index, length int
}
type modelBatchFailureKind uint8

const (
	batchNotIterable modelBatchFailureKind = iota + 1
	batchElementNotSequence
	batchWrongPairLength
	batchUnsupportedKey
)

func (failure ModelBatchInputFailure) SourceClass() string {
	if failure.kind == batchWrongPairLength {
		return "ValueError"
	}
	if failure.kind == batchUnsupportedKey {
		return "UnsupportedProfile"
	}
	return "TypeError"
}
func (failure ModelBatchInputFailure) Error() string {
	switch failure.kind {
	case batchNotIterable:
		return fmt.Sprintf("'%s' object is not iterable", failure.rootKind)
	case batchElementNotSequence:
		return fmt.Sprintf("cannot convert dictionary update sequence element #%d to a sequence", failure.index)
	case batchWrongPairLength:
		return fmt.Sprintf("dictionary update sequence element #%d has length %d; 2 is required", failure.index, failure.length)
	default:
		return "non-string model batch mapping keys require a named mapping observation"
	}
}

// ForModelBatch copies the observed shell arguments as Source dict(input or {}).
// The original assistant block remains unchanged. Non-string mapping-key
// profiles remain explicit rather than entering a string-key domain object.
func (input ToolInput) ForModelBatch() (ToolInput, error) {
	rejected := input.bashRejected
	if input.backgroundModel != nil {
		rejected = input.backgroundModel.rejected
	}
	if rejected == nil {
		return input, nil
	}
	value := rejected.observation
	if !value.Truth() {
		return DecodeModelToolInput(input.Name(), []byte(`{}`))
	}
	switch value.Kind() {
	case jsonvalue.Array:
		rows, _ := value.Array()
		fields := make([]jsonvalue.Field, 0, len(rows))
		for index, row := range rows {
			pair, ok := row.Array()
			if text, textual := row.Text(); textual {
				runes := []rune(text)
				pair = make([]jsonvalue.Value, len(runes))
				for i, r := range runes {
					pair[i] = jsonvalue.TextValue(string(r))
				}
				ok = true
			}
			if !ok {
				return ToolInput{}, ModelBatchInputFailure{kind: batchElementNotSequence, index: index}
			}
			if len(pair) != 2 {
				return ToolInput{}, ModelBatchInputFailure{kind: batchWrongPairLength, index: index, length: len(pair)}
			}
			key, ok := pair[0].Text()
			if !ok {
				return ToolInput{}, ModelBatchInputFailure{kind: batchUnsupportedKey, index: index}
			}
			fields = append(fields, jsonvalue.Field{Name: key, Value: pair[1]})
		}
		data, err := jsonvalue.ObjectValue(fields).MarshalLegacyUTF8()
		if err != nil {
			return ToolInput{}, err
		}
		return DecodeModelToolInput(input.Name(), data)
	case jsonvalue.Text:
		return ToolInput{}, ModelBatchInputFailure{kind: batchWrongPairLength, index: 0, length: 1}
	default:
		return ToolInput{}, ModelBatchInputFailure{kind: batchNotIterable, rootKind: rejected.kind}
	}
}
