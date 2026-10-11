package protocol

import (
	"math"
	"math/big"
	"strings"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

type sdkUsageReprModel uint8

const (
	sdkUsageReprRoot sdkUsageReprModel = iota
	sdkUsageReprCache
	sdkUsageReprOutput
	sdkUsageReprServer
)

// These temporary construction values never become counts or stored payloads.
// The final whole-usage observation stores only its closed kind and repr text.
func sdkUsageReprConstruct(value jsonvalue.Value, model sdkUsageReprModel, validateIntegerFields bool) jsonvalue.Value {
	fields := sdkUsageFields
	integers := []string{"cache_creation_input_tokens", "cache_read_input_tokens"}
	switch model {
	case sdkUsageReprCache:
		fields = []string{"ephemeral_1h_input_tokens", "ephemeral_5m_input_tokens"}
		integers = fields
	case sdkUsageReprOutput:
		fields = []string{"thinking_tokens"}
		integers = fields
	case sdkUsageReprServer:
		fields = []string{"web_fetch_requests", "web_search_requests"}
		integers = fields
	}
	if validateIntegerFields && model != sdkUsageReprRoot {
		for _, key := range integers {
			item, present := value.Lookup(key)
			if _, valid := sdkUsageReprInteger(item); !present || !valid {
				validateIntegerFields = false
				break
			}
		}
	}
	result := make([]jsonvalue.Field, 0, len(value.Keys())+len(fields))
	for _, key := range fields {
		item, _ := value.Lookup(key)
		if validateIntegerFields {
			for _, integerKey := range integers {
				if key == integerKey {
					if normalized, valid := sdkUsageReprInteger(item); valid {
						item = normalized
					}
				}
			}
		}
		if model == sdkUsageReprRoot {
			nested := sdkUsageReprRoot
			switch key {
			case "cache_creation":
				nested = sdkUsageReprCache
			case "output_tokens_details":
				nested = sdkUsageReprOutput
			case "server_tool_use":
				nested = sdkUsageReprServer
			}
			if nested != sdkUsageReprRoot {
				if item.Kind() == jsonvalue.Object {
					item = sdkUsageReprConstruct(item, nested, true)
				}
				if item.Kind() == jsonvalue.Array {
					items, _ := item.Array()
					for i, entry := range items {
						if entry.Kind() == jsonvalue.Object {
							items[i] = sdkUsageReprConstruct(entry, nested, false)
						}
					}
					item = jsonvalue.ArrayValue(items)
				}
			}
		}
		result = append(result, jsonvalue.Field{Name: key, Value: item})
	}
	for _, key := range value.Keys() {
		known := false
		for _, name := range fields {
			if name == key {
				known = true
				break
			}
		}
		if !known {
			item, _ := value.Lookup(key)
			result = append(result, jsonvalue.Field{Name: key, Value: item})
		}
	}
	return jsonvalue.ObjectValue(result)
}

// Python/Pydantic accepts arbitrary JSON integers and integer text; float-to-int
// validation retains its signed 64-bit boundary. No metering conversion occurs.
func sdkUsageReprInteger(value jsonvalue.Value) (jsonvalue.Value, bool) {
	switch value.Kind() {
	case jsonvalue.Integer:
		return value, true
	case jsonvalue.Boolean:
		flag, _ := value.Bool()
		if flag {
			return jsonvalue.IntegerValue(1), true
		}
		return jsonvalue.IntegerValue(0), true
	case jsonvalue.Float:
		number, _ := value.Float()
		if math.Trunc(number) == number && number >= -0x1p63 && number < 0x1p63 {
			return jsonvalue.IntegerValue(int64(number)), true
		}
	case jsonvalue.Text:
		raw, _ := value.Text()
		raw = strings.TrimSpace(raw)
		integer, fraction, decimal := strings.Cut(raw, ".")
		if decimal {
			if fraction == "" {
				return value, false
			}
			for i := range fraction {
				if fraction[i] != '0' {
					return value, false
				}
			}
			raw = integer
		}
		for i := range raw {
			if raw[i] == '_' && (i == 0 || i+1 == len(raw) || raw[i-1] < '0' || raw[i-1] > '9' || raw[i+1] < '0' || raw[i+1] > '9') {
				return value, false
			}
		}
		raw = strings.ReplaceAll(raw, "_", "")
		if len(strings.TrimLeft(raw, "+-")) > 4300 {
			return value, false
		}
		if integer, ok := new(big.Int).SetString(raw, 10); ok {
			normalized, err := jsonvalue.Decode(integer.String())
			if err == nil {
				return normalized, true
			}
		}
	}
	return value, false
}
