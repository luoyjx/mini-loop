package provider

import (
	"encoding/json"
	"errors"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
)

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
	if json.Valid(data) {
		return decodeBlock(data)
	}
	if len(data) > protocol.MaxWireBytes {
		return protocol.Block{}, errors.New("SDK block exceeds wire bound")
	}
	value, err := jsonvalue.Decode(string(data))
	if err != nil {
		return protocol.Block{}, err
	}
	kind, _ := value.Lookup("type")
	name, _ := kind.Text()
	if name != string(protocol.BlockToolUse) && name != string(protocol.BlockServerToolUse) {
		return protocol.Block{}, errors.New("nonfinite SDK input requires a tool observation")
	}
	input, present := value.Lookup("input")
	if !present {
		return protocol.Block{}, errors.New("tool observation requires input")
	}
	finite, err := replaceSDKField(value, "input", jsonvalue.NullValue()).MarshalJSON()
	if err != nil {
		return protocol.Block{}, err
	}
	block, err := decodeBlock(finite)
	if err != nil {
		return protocol.Block{}, err
	}
	original, err := input.MarshalLegacyUTF8()
	if err != nil {
		return protocol.Block{}, err
	}
	return block.WithSDKObservedInput(original)
}
