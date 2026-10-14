package provider

import (
	"errors"
	"unicode/utf8"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
)

// Only these SDK observations may contain legacy numbers. The remaining block
// fields still pass the ordinary finite JSON decoder and its named variants.
type sdkBlockObservations struct {
	input                                   jsonvalue.Value
	inputPresent                            bool
	caller                                  jsonvalue.Value
	callerPresent                           bool
	text, thinking, signature, data, fileID *string
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
			if _, encodeErr := input.MarshalUTF8(); errors.Is(encodeErr, jsonvalue.ErrNonfinite) || errors.Is(encodeErr, jsonvalue.ErrSurrogate) {
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
	for _, field := range []struct {
		name   string
		target **string
	}{{"text", &observations.text}, {"thinking", &observations.thinking},
		{"signature", &observations.signature}, {"data", &observations.data}, {"file_id", &observations.fileID}} {
		// Restore only existing named text variants, never arbitrary metadata.
		admitted := name == string(protocol.BlockText) && field.name == "text" ||
			name == string(protocol.BlockThinking) && (field.name == "thinking" || field.name == "signature") ||
			name == string(protocol.BlockRedactedThinking) && field.name == "data" ||
			name == string(protocol.BlockContainerUpload) && field.name == "file_id"
		if !admitted {
			continue
		}
		original, _ := value.Lookup(field.name)
		text, isText := original.Text()
		if isText && !utf8.ValidString(text) {
			*field.target = &text
			placeholder := ""
			if field.name == "signature" {
				placeholder = "sdk-signature"
			}
			value = replaceSDKField(value, field.name, jsonvalue.TextValue(placeholder))
		}
	}
	finite, err := value.MarshalUTF8()
	return finite, observations, err
}

func (observations sdkBlockObservations) restore(block protocol.Block) (protocol.Block, error) {
	var err error
	if observations.inputPresent {
		data, encodeErr := sdkObservationBytes(observations.input)
		if encodeErr != nil {
			return protocol.Block{}, encodeErr
		}
		block, err = block.WithSDKObservedInput(data)
		if err != nil {
			return protocol.Block{}, err
		}
	}
	if observations.callerPresent {
		data, encodeErr := sdkObservationBytes(observations.caller)
		if encodeErr != nil {
			return protocol.Block{}, encodeErr
		}
		block, err = block.WithSDKObservedCaller(data)
		if err != nil {
			return protocol.Block{}, err
		}
	}
	if observations.text != nil {
		text, _ := block.Text()
		block = text.WithText(*observations.text)
	}
	if observations.thinking != nil || observations.signature != nil {
		text, _ := block.Thinking()
		if observations.thinking != nil {
			text.Thinking = *observations.thinking
		}
		if observations.signature != nil {
			text.Signature = *observations.signature
		}
		block = protocol.NewThinkingBlock(text.Thinking, text.Signature)
	}
	if observations.data != nil {
		block = protocol.NewRedactedThinkingBlock(*observations.data)
	}
	if observations.fileID != nil {
		block = protocol.NewContainerUploadBlock(*observations.fileID)
	}
	return block, nil
}

func checkSDKBlockSize(block protocol.Block) error {
	data, err := block.MarshalObservationJSON()
	if errors.Is(err, jsonvalue.ErrSurrogate) {
		// The existing normalized limit measures UTF-8 bytes. ASCII escaping
		// would inflate valid Unicode, so only this new surrogatepass lane
		// obtains its original text-byte length from the closed value.
		ascii, asciiErr := block.MarshalObservationASCIIJSON()
		if asciiErr != nil {
			return asciiErr
		}
		value, decodeErr := jsonvalue.Decode(string(ascii))
		if decodeErr != nil {
			return decodeErr
		}
		text, textErr := value.LegacyString()
		if textErr != nil {
			return textErr
		}
		data, err = []byte(text), nil
	}
	if err != nil {
		return err
	}
	if len(data) > protocol.MaxWireBytes {
		return errors.New("normalized SDK block exceeds protocol limit")
	}
	return nil
}

// Keep ordinary Unicode literal so a surrogate in one field cannot inflate all
// other text past the existing decoder byte budget. Escape only surrogatepass
// code points; this is an internal observation bridge, never an HTTP encoder.
func sdkObservationBytes(value jsonvalue.Value) ([]byte, error) {
	data, err := value.MarshalLegacyUTF8()
	if errors.Is(err, jsonvalue.ErrSurrogate) {
		text, textErr := value.LegacyString()
		if textErr != nil {
			return nil, textErr
		}
		out := make([]byte, 0, len(text))
		const digits = "0123456789abcdef"
		for i := 0; i < len(text); i++ {
			if i+2 < len(text) && text[i] == 0xed && text[i+1] >= 0xa0 && text[i+1] <= 0xbf && text[i+2]&0xc0 == 0x80 {
				point := uint16(text[i]&15)<<12 | uint16(text[i+1]&63)<<6 | uint16(text[i+2]&63)
				out = append(out, '\\', 'u', digits[point>>12], digits[point>>8&15], digits[point>>4&15], digits[point&15])
				i += 2
			} else {
				out = append(out, text[i])
			}
		}
		return out, nil
	}
	return data, err
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
		data, err := sdkObservationBytes(item)
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
