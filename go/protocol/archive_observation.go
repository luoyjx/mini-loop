package protocol

import (
	"encoding/json"
	"errors"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

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
	if kindText != string(BlockToolUse) && kindText != string(BlockServerToolUse) {
		return Block{}, errors.New("legacy input requires a named tool observation block")
	}
	input, present := value.Lookup("input")
	if !present {
		return Block{}, errors.New("tool observation requires input")
	}
	metadata := replaceObservationField(value, "input", jsonvalue.NullValue())
	finite, err := metadata.MarshalJSON()
	if err != nil {
		return Block{}, err
	}
	block, err := decodeBlock(finite, false)
	if err != nil {
		return Block{}, err
	}
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
	return block, block.Validate()
}
