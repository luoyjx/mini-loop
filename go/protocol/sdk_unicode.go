package protocol

import (
	"unicode/utf8"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

// MarshalObservationASCIIJSON is an escaped SDK snapshot. Unlike the existing
// UTF-8 observation and HTTP encoders it can retain unpaired Unicode code
// points. The result grants no execution authority and is not HTTP admission.
func (block Block) MarshalObservationASCIIJSON() ([]byte, error) {
	value, err := block.observationValue()
	if err != nil {
		return nil, err
	}
	return jsonvalue.AppendLegacy(nil, value)
}

// These existing named string fields can carry Python surrogatepass text after
// SDK ingress. encoding/json would silently replace it. Build the observation
// from those fields, while ordinary wire encoding refuses it explicitly.
func (block Block) typedTextObservation() (jsonvalue.Value, bool, error) {
	var fields []jsonvalue.Field
	switch block.kind {
	case BlockText:
		fields = []jsonvalue.Field{{Name: "text", Value: jsonvalue.TextValue(block.text.Text)}}
		block = block.text.WithText("")
	case BlockThinking:
		original := *block.thinking
		fields = []jsonvalue.Field{{Name: "thinking", Value: jsonvalue.TextValue(original.Thinking)},
			{Name: "signature", Value: jsonvalue.TextValue(original.Signature)}}
		block = NewThinkingBlock("", "sdk-signature")
	case BlockRedactedThinking:
		original := *block.redactedThinking
		fields = []jsonvalue.Field{{Name: "data", Value: jsonvalue.TextValue(original.Data)}}
		block.redactedThinking = &RedactedThinkingBlock{}
	case BlockContainerUpload:
		original := *block.containerUpload
		fields = []jsonvalue.Field{{Name: "file_id", Value: jsonvalue.TextValue(original.FileID)}}
		original.FileID = ""
		block.containerUpload = &original
	default:
		return jsonvalue.Value{}, false, nil
	}
	data, err := block.MarshalJSON()
	if err != nil {
		return jsonvalue.Value{}, true, err
	}
	value, err := jsonvalue.Decode(string(data))
	if err != nil {
		return jsonvalue.Value{}, true, err
	}
	for _, field := range fields {
		value = replaceObservationField(value, field.Name, field.Value)
	}
	return value, true, nil
}

func (block Block) textWireUTF8() bool {
	switch block.kind {
	case BlockText:
		return utf8.ValidString(block.text.Text)
	case BlockThinking:
		return utf8.ValidString(block.thinking.Thinking) && utf8.ValidString(block.thinking.Signature)
	case BlockRedactedThinking:
		return utf8.ValidString(block.redactedThinking.Data)
	case BlockContainerUpload:
		return utf8.ValidString(block.containerUpload.FileID)
	}
	return true
}

func validateSDKCallerKeys(value jsonvalue.Value) error {
	for _, key := range value.Keys() {
		if !utf8.ValidString(key) {
			return jsonvalue.ErrSurrogate
		}
		child, _ := value.Lookup(key)
		if err := validateSDKCallerKeys(child); err != nil {
			return err
		}
	}
	if items, ok := value.Array(); ok {
		for _, child := range items {
			if err := validateSDKCallerKeys(child); err != nil {
				return err
			}
		}
	}
	return nil
}
