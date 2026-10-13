package protocol

import (
	"encoding/json"
	"errors"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

// ObservedMessagesPythonJSON is the inspection/estimation projection. It does
// not change the finite model request encoder or grant execution authority.
func ObservedMessagesPythonJSON(messages []Message, ascii, compact bool) (string, error) {
	items := make([]jsonvalue.Value, len(messages))
	for i, message := range messages {
		data, err := message.MarshalArchiveJSON()
		if err != nil {
			return "", err
		}
		items[i], err = jsonvalue.Decode(string(data))
		if err != nil {
			return "", err
		}
	}
	data, err := jsonvalue.ArrayValue(items).MarshalLegacyUTF8()
	if err != nil {
		return "", err
	}
	return formatPythonJSONBytes(data, ascii, compact)
}

// ObservedWireMessagesPythonJSON retains detached cache annotations when
// estimating or recording a request. HTTP encoding still uses Wire.MarshalJSON.
func ObservedWireMessagesPythonJSON(messages []WireMessage, ascii, compact bool) (string, error) {
	items := make([]jsonvalue.Value, len(messages))
	for i, message := range messages {
		data, err := (Message{Role: message.Role, Content: message.Content.content}).MarshalArchiveJSON()
		if err != nil {
			return "", err
		}
		value, err := jsonvalue.Decode(string(data))
		if err != nil {
			return "", err
		}
		content, _ := value.Lookup("content")
		if blocks, ok := content.Array(); ok {
			for index, control := range message.Content.controls {
				if index < 0 || index >= len(blocks) {
					return "", errors.New("cache breakpoint outside observed content")
				}
				if err := control.Validate(); err != nil {
					return "", err
				}
				encoded, err := json.Marshal(control)
				if err != nil {
					return "", err
				}
				cache, err := jsonvalue.Decode(string(encoded))
				if err != nil {
					return "", err
				}
				blocks[index] = observationFields(blocks[index], jsonvalue.ObjectValue([]jsonvalue.Field{{Name: "cache_control", Value: cache}}))
			}
			value = replaceObservationField(value, "content", jsonvalue.ArrayValue(blocks))
		}
		items[i] = value
	}
	data, err := jsonvalue.ArrayValue(items).MarshalLegacyUTF8()
	if err != nil {
		return "", err
	}
	return formatPythonJSONBytes(data, ascii, compact)
}

// MarshalArchiveJSON preserves SDK observation numbers in a detached transcript.
// Message.MarshalJSON remains finite request encoding.
func (message Message) MarshalArchiveJSON() ([]byte, error) {
	if err := message.Validate(); err != nil {
		return nil, err
	}
	var content jsonvalue.Value
	if message.Content.plain != nil {
		content = jsonvalue.TextValue(*message.Content.plain)
	} else {
		values := make([]jsonvalue.Value, len(message.Content.blocks))
		for i, block := range message.Content.blocks {
			v, err := block.observationValue()
			if err != nil {
				return nil, err
			}
			values[i] = v
		}
		content = jsonvalue.ArrayValue(values)
	}
	return jsonvalue.ObjectValue([]jsonvalue.Field{{Name: "role", Value: jsonvalue.TextValue(string(message.Role))}, {Name: "content", Value: content}}).MarshalLegacyUTF8()
}

func decodeLegacyStoredMessage(data []byte) (Message, error) {
	value, err := jsonvalue.Decode(string(data))
	if err != nil {
		return Message{}, err
	}
	if value.Kind() != jsonvalue.Object {
		return Message{}, errors.New("stored message must be an object")
	}
	for _, key := range value.Keys() {
		if key != "role" && key != "content" {
			return Message{}, errors.New("unsupported stored message field")
		}
	}
	role, exists := value.Lookup("role")
	text, ok := role.Text()
	if !exists || !ok {
		return Message{}, errors.New("stored message requires role")
	}
	content, exists := value.Lookup("content")
	if !exists {
		return Message{}, errors.New("stored message requires content")
	}
	message := Message{Role: Role(text)}
	if text, ok := content.Text(); ok {
		message.Content = PlainContent(text)
	} else {
		items, ok := content.Array()
		if !ok {
			return Message{}, errors.New("content must be a string or block array")
		}
		blocks := make([]Block, len(items))
		for i, item := range items {
			data, err := item.MarshalLegacyUTF8()
			if err != nil {
				return Message{}, err
			}
			block, err := decodeLegacyObservedBlock(data)
			if err != nil {
				return Message{}, err
			}
			blocks[i] = block
		}
		message.Content = BlockContent(blocks...)
	}
	return message, message.Validate()
}

func decodeLegacyObservedBlock(data []byte) (Block, error) {
	if json.Valid(data) {
		return decodeBlock(data, false)
	}
	value, err := jsonvalue.Decode(string(data))
	if err != nil {
		return Block{}, err
	}
	kind, _ := value.Lookup("type")
	kindText, _ := kind.Text()
	toolInput := kindText == string(BlockToolUse) || kindText == string(BlockServerToolUse)
	callerKind := toolInput || kindText == string(BlockWebSearchToolResult) || kindText == string(BlockWebFetchToolResult)
	if !callerKind {
		return Block{}, errors.New("legacy metadata requires a named observation block")
	}
	metadata := value
	input, inputPresent := value.Lookup("input")
	if toolInput {
		if !inputPresent {
			return Block{}, errors.New("tool observation requires input")
		}
		metadata = replaceObservationField(metadata, "input", jsonvalue.NullValue())
	}
	callerValue, callerPresent := value.Lookup("caller")
	if callerPresent {
		metadata = replaceObservationField(metadata, "caller", jsonvalue.NullValue())
	}
	finite, err := metadata.MarshalJSON()
	if err != nil {
		return Block{}, err
	}
	block, err := decodeBlock(finite, false)
	if err != nil {
		return Block{}, err
	}
	if toolInput {
		original, err := input.MarshalLegacyUTF8()
		if err != nil {
			return Block{}, err
		}
		if block.kind == BlockToolUse {
			observed, err := DecodeModelToolInput(block.toolUse.Name, original)
			if err != nil {
				return Block{}, err
			}
			block.toolUse.Input = observed
		} else {
			observed, err := decodeSDKServerToolInput(original, false)
			if err != nil {
				return Block{}, err
			}
			block.serverTool.Input = observed
		}
	}
	if callerPresent && callerValue.Kind() != jsonvalue.Null {
		// Archived SDK construction is already complete. Preserve sparse
		// dictionaries and list elements without another round of defaults.
		observation := SDKCallerObservation{value: callerValue}
		block.setCaller(&ToolCaller{sdkObservation: &observation}, true)
	}
	return block, block.Validate()
}
