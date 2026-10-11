package protocol

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

// Whole usage observations have no count authority. Their text is the SDK's
// event projection; the closed source kind retains delta-assignment faults.
type wholeUsageKind uint8

const (
	wholeUsageNull wholeUsageKind = iota
	wholeUsageInteger
	wholeUsageFloat
	wholeUsageBoolean
	wholeUsageText
	wholeUsageArray
	wholeUsageArchivedText
)

type wholeUsageObservation struct {
	kind wholeUsageKind
	text string
}

// SDKUsageDeltaFault returns the captured Python assignment failure for a
// non-model usage. Named usage models, including partial models, are mutable.
func (u TokenUsage) SDKUsageDeltaFault() string {
	if u.whole == nil {
		return ""
	}
	names := [...]string{"NoneType", "int", "float", "bool", "str", "list", "str"}
	return "'" + names[u.whole.kind] + "' object has no attribute 'output_tokens'"
}

// HasUsageCounts identifies the named model variant. Whole usage observations
// cannot acquire numeric authority from mutation of the public count slots.
func (u TokenUsage) HasUsageCounts() bool { return u.whole == nil }

// DecodeSDKUsage selects actual SDK loose construction at the wire boundary.
// The ordinary TokenUsage codec remains strict about missing count fields.
func DecodeSDKUsage(data []byte, stream bool) (TokenUsage, error) {
	if len(data) == 0 {
		return newWholeUsage(wholeUsageObservation{kind: wholeUsageNull}), nil
	}
	if len(data) > MaxWireBytes {
		return TokenUsage{}, errors.New("usage exceeds protocol limit")
	}
	value, err := jsonvalue.Decode(string(data))
	if err != nil {
		return TokenUsage{}, err
	}
	if value.Kind() == jsonvalue.Object {
		for _, key := range []string{"input_tokens", "output_tokens"} {
			if _, ok := value.Lookup(key); !ok {
				fields := make([]jsonvalue.Field, 0, len(value.Keys())+1)
				for _, name := range value.Keys() {
					item, _ := value.Lookup(name)
					fields = append(fields, jsonvalue.Field{Name: name, Value: item})
				}
				fields = append(fields, jsonvalue.Field{Name: key, Value: jsonvalue.NullValue()})
				value = jsonvalue.ObjectValue(fields)
			}
		}
		encoded, err := value.MarshalJSON()
		if err != nil {
			return TokenUsage{}, err
		}
		var usage TokenUsage
		if err := json.Unmarshal(encoded, &usage); err != nil {
			return TokenUsage{}, err
		}
		if stream {
			return usage.WithSDKStreamCounts(), nil
		}
		return usage.WithSDKDirectCounts(), nil
	}
	observation, err := usageMetadataFromValue(value)
	if err != nil {
		return TokenUsage{}, err
	}
	whole := wholeUsageObservation{}
	switch observation.kind {
	case UsageMetadataNull:
		whole.kind = wholeUsageNull
	case UsageMetadataInteger:
		whole.kind = wholeUsageInteger
	case UsageMetadataFloat:
		whole.kind = wholeUsageFloat
	case UsageMetadataBoolean:
		whole.kind = wholeUsageBoolean
	case UsageMetadataText:
		whole.kind = wholeUsageText
	case UsageMetadataArray:
		whole.kind = wholeUsageArray
	default:
		return TokenUsage{}, errors.New("invalid whole usage kind")
	}
	if whole.kind != wholeUsageNull {
		whole.text, err = sdkWholeUsageText(value)
		if err != nil {
			return TokenUsage{}, err
		}
		if len(whole.text) > MaxWireBytes {
			return TokenUsage{}, errors.New("usage projection exceeds protocol limit")
		}
	}
	return newWholeUsage(whole), nil
}

var sdkUsageFields = []string{"cache_creation", "cache_creation_input_tokens", "cache_read_input_tokens", "inference_geo", "input_tokens", "output_tokens", "output_tokens_details", "server_tool_use", "service_tier"}

func sdkWholeUsageText(value jsonvalue.Value) (string, error) {
	if value.Kind() != jsonvalue.Array {
		return value.PythonString(), nil
	}
	items, _ := value.Array()
	parts := make([]string, len(items))
	for i, item := range items {
		if item.Kind() != jsonvalue.Object {
			parts[i] = jsonvalue.ArrayValue([]jsonvalue.Value{item}).PythonString()
			parts[i] = parts[i][1 : len(parts[i])-1]
			continue
		}
		normalized := sdkUsageReprConstruct(item, sdkUsageReprRoot, true)
		parts[i] = sdkUsageModelRepr("Usage", sdkUsageFields, normalized)
	}
	return "[" + strings.Join(parts, ", ") + "]", nil
}

func sdkUsageModelRepr(name string, fields []string, value jsonvalue.Value) string {
	parts := make([]string, 0, len(value.Keys()))
	seen := make(map[string]bool, len(fields)) // temporary projection lookup only
	for _, key := range fields {
		seen[key] = true
		item, _ := value.Lookup(key)
		parts = append(parts, key+"="+sdkUsageFieldRepr(key, item))
	}
	for _, key := range value.Keys() {
		if !seen[key] {
			item, _ := value.Lookup(key)
			parts = append(parts, key+"="+sdkUsageFieldRepr("", item))
		}
	}
	return name + "(" + strings.Join(parts, ", ") + ")"
}
func sdkUsageFieldRepr(key string, value jsonvalue.Value) string {
	var name string
	var fields []string
	switch key {
	case "cache_creation":
		name = "CacheCreation"
		fields = []string{"ephemeral_1h_input_tokens", "ephemeral_5m_input_tokens"}
	case "output_tokens_details":
		name = "OutputTokensDetails"
		fields = []string{"thinking_tokens"}
	case "server_tool_use":
		name = "ServerToolUsage"
		fields = []string{"web_fetch_requests", "web_search_requests"}
	}
	if name != "" {
		if value.Kind() == jsonvalue.Object {
			return sdkUsageModelRepr(name, fields, value)
		}
		if value.Kind() == jsonvalue.Array {
			items, _ := value.Array()
			parts := make([]string, len(items))
			for i, item := range items {
				if item.Kind() == jsonvalue.Object {
					parts[i] = sdkUsageModelRepr(name, fields, item)
				} else {
					parts[i] = sdkUsageFieldRepr("", item)
				}
			}
			return "[" + strings.Join(parts, ", ") + "]"
		}
	}
	text := jsonvalue.ArrayValue([]jsonvalue.Value{value}).PythonString()
	return text[1 : len(text)-1]
}

func newWholeUsage(value wholeUsageObservation) TokenUsage {
	return TokenUsage{InputTokens: UsageCount{kind: UsageNull}, OutputTokens: UsageCount{kind: UsageNull}, whole: &value}
}
