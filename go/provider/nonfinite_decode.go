package provider

import (
	"errors"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
)

// Only these SDK observations may contain legacy numbers. The remaining block
// fields still pass the ordinary finite JSON decoder and its named variants.
type sdkBlockObservations struct {
	input         jsonvalue.Value
	inputPresent  bool
	caller        jsonvalue.Value
	callerPresent bool
}

func prepareSDKBlock(data []byte) ([]byte, sdkBlockObservations, error) {
	var observations sdkBlockObservations
	if len(data) > protocol.MaxWireBytes {
		return nil, observations, errors.New("SDK block exceeds wire bound")
	}
	value, err := jsonvalue.Decode(string(data))
	if err != nil {
		return nil, observations, err
	}
	kind, _ := value.Lookup("type")
	name, _ := kind.Text()
	switch protocol.BlockKind(name) {
	case protocol.BlockToolUse, protocol.BlockServerToolUse:
		if input, present := value.Lookup("input"); present {
			// Finite inputs must reach their original named decoder. Several
			// tool schemas require an object before observation restoration.
			if _, encodeErr := input.MarshalUTF8(); errors.Is(encodeErr, jsonvalue.ErrNonfinite) {
				observations.input, observations.inputPresent = input, true
				value = replaceSDKField(value, "input", jsonvalue.NullValue())
			} else if encodeErr != nil {
				return nil, observations, encodeErr
			}
		}
		fallthrough
	case protocol.BlockWebSearchToolResult, protocol.BlockWebFetchToolResult:
		observations.caller, observations.callerPresent = value.Lookup("caller")
		if observations.callerPresent {
			value = replaceSDKField(value, "caller", jsonvalue.NullValue())
		}
	}
	finite, err := value.MarshalUTF8()
	return finite, observations, err
}

func (observations sdkBlockObservations) restore(block protocol.Block) (protocol.Block, error) {
	var err error
	if observations.inputPresent {
		data, encodeErr := observations.input.MarshalLegacyUTF8()
		if encodeErr != nil {
			return protocol.Block{}, encodeErr
		}
		block, err = block.WithSDKObservedInput(data)
		if err != nil {
			return protocol.Block{}, err
		}
	}
	if observations.callerPresent {
		data, encodeErr := observations.caller.MarshalLegacyUTF8()
		if encodeErr != nil {
			return protocol.Block{}, encodeErr
		}
		block, err = block.WithSDKObservedCaller(data)
		if err != nil {
			return protocol.Block{}, err
		}
	}
	return block, nil
}

func checkSDKBlockSize(block protocol.Block) error {
	data, err := block.MarshalObservationJSON()
	if err != nil {
		return err
	}
	if len(data) > protocol.MaxWireBytes {
		return errors.New("normalized SDK block exceeds protocol limit")
	}
	return nil
}

func replaceSDKField(value jsonvalue.Value, key string, replacement jsonvalue.Value) jsonvalue.Value {
	fields := make([]jsonvalue.Field, 0, len(value.Keys()))
	for _, name := range value.Keys() {
		v, _ := value.Lookup(name)
		if name == key {
			v = replacement
		}
		fields = append(fields, jsonvalue.Field{Name: name, Value: v})
	}
	return jsonvalue.ObjectValue(fields)
}
func decodeLegacySDKReply(data []byte) (protocol.ModelReply, error) {
	value, err := jsonvalue.Decode(string(data))
	if err != nil {
		return protocol.ModelReply{}, err
	}
	content, present := value.Lookup("content")
	items, ok := content.Array()
	if !present || !ok {
		return protocol.ModelReply{}, errors.New("completed message requires content array")
	}
	metadata := replaceSDKField(value, "content", jsonvalue.ArrayValue(nil))
	finite, err := metadata.MarshalJSON()
	if err != nil {
		return protocol.ModelReply{}, err
	}
	reply, err := decodeReply(finite)
	if err != nil {
		return protocol.ModelReply{}, err
	}
	for _, item := range items {
		data, err := item.MarshalLegacyUTF8()
		if err != nil {
			return protocol.ModelReply{}, err
		}
		block, err := decodeLegacySDKBlock(data)
		if err != nil {
			return protocol.ModelReply{}, err
		}
		reply.Content = append(reply.Content, block)
	}
	return reply, reply.Validate()
}
func decodeLegacySDKBlock(data []byte) (protocol.Block, error) {
	return decodeBlock(data)
}
