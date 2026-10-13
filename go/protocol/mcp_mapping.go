package protocol

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"sort"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

// mcpMapping is an immutable, ordered observation of Python dict preparation.
// Non-text keys never become MCP client arguments. Private scalar keys and
// closed values retain source equality and fault stages without an open map.
type mcpMapping struct{ entries []mcpMappingEntry }
type mcpMappingEntry struct {
	key   mcpMappingKey
	value jsonvalue.Value
}
type mcpMappingKey struct{ value jsonvalue.Value }

// MCPKeywordBindingFailure is settled after permission, before any MCP client
// invocation. It represents Python **kwargs binding, not a batch-copy fault.
type MCPKeywordBindingFailure struct{}

func (MCPKeywordBindingFailure) Error() string       { return "keywords must be strings" }
func (MCPKeywordBindingFailure) SourceClass() string { return "TypeError" }
func (input ToolInput) MCPKeywordBindingFailure() (MCPKeywordBindingFailure, bool) {
	return MCPKeywordBindingFailure{}, IsMCPToolName(input.name) && input.mcpMapping != nil
}

// MCPMappingComparisonFailure records a source key-ordering fault in sorted
// observation/journal projection. It contains no executable values or grants.
type MCPMappingComparisonFailure struct{ left, right string }

func (f MCPMappingComparisonFailure) Error() string {
	return fmt.Sprintf("'<' not supported between instances of '%s' and '%s'", f.left, f.right)
}
func (MCPMappingComparisonFailure) SourceClass() string { return "TypeError" }

func mcpKeyType(value jsonvalue.Value) string {
	switch value.Kind() {
	case jsonvalue.Null:
		return "NoneType"
	case jsonvalue.Text:
		return "str"
	case jsonvalue.Integer:
		return "int"
	case jsonvalue.Float:
		return "float"
	case jsonvalue.Boolean:
		return "bool"
	case jsonvalue.Array:
		return "list"
	default:
		return "dict"
	}
}

func (key mcpMappingKey) identity() (string, bool) {
	value := key.value
	switch value.Kind() {
	case jsonvalue.Text:
		text, _ := value.Text()
		return "text:" + text, true
	case jsonvalue.Null:
		return "null", true
	case jsonvalue.Boolean:
		truth, _ := value.Bool()
		if truth {
			return "number:1", true
		}
		return "number:0", true
	case jsonvalue.Integer:
		text, _ := value.Integer()
		integer, _ := new(big.Int).SetString(text, 10)
		return "number:" + integer.String(), true
	case jsonvalue.Float:
		number, _ := value.Float()
		if math.IsNaN(number) {
			return "", false
		}
		if math.IsInf(number, 1) {
			return "positive-infinity", true
		}
		if math.IsInf(number, -1) {
			return "negative-infinity", true
		}
		return "number:" + new(big.Rat).SetFloat64(number).RatString(), true
	default:
		return "", false
	}
}

func (key mcpMappingKey) text() (string, error) {
	if text, ok := key.value.Text(); ok {
		return text, nil
	}
	if key.value.Kind() == jsonvalue.Null {
		return "null", nil
	}
	if truth, ok := key.value.Bool(); ok {
		if truth {
			return "true", nil
		}
		return "false", nil
	}
	data, err := key.value.MarshalLegacyUTF8()
	return string(data), err
}

func (mapping *mcpMapping) marshal(sorted, legacy bool) ([]byte, error) {
	entries := append([]mcpMappingEntry(nil), mapping.entries...)
	var failure error
	if sorted {
		sort.SliceStable(entries, func(i, j int) bool {
			if failure != nil {
				return false
			}
			a, b := entries[i].key.value, entries[j].key.value
			if a.Kind() == jsonvalue.Text && b.Kind() == jsonvalue.Text {
				left, _ := a.Text()
				right, _ := b.Text()
				return left < right
			}
			comparison, ordered, err := a.PythonNumberCompare(b)
			if err != nil {
				failure = MCPMappingComparisonFailure{mcpKeyType(a), mcpKeyType(b)}
				return false
			}
			return ordered && comparison < 0
		})
		if failure != nil {
			return nil, failure
		}
	}
	out := []byte{'{'}
	for i, entry := range entries {
		if !legacy {
			if _, err := entry.key.value.MarshalJSON(); err != nil {
				return nil, err
			}
		}
		name, err := entry.key.text()
		if err != nil {
			return nil, err
		}
		key, err := jsonvalue.TextValue(name).MarshalLegacyUTF8()
		if err != nil {
			return nil, err
		}
		value := entry.value
		if sorted {
			value = value.Sorted()
		}
		var data []byte
		if legacy {
			data, err = value.MarshalLegacyUTF8()
		} else {
			data, err = value.MarshalJSON()
		}
		if err != nil {
			return nil, err
		}
		if i > 0 {
			out = append(out, ',')
		}
		out = append(out, key...)
		out = append(out, ':')
		out = append(out, data...)
	}
	return append(out, '}'), nil
}

func (mapping *mcpMapping) masked(mask func(string) string) *mcpMapping {
	copy := &mcpMapping{}
	positions := make(map[string]int)
	for _, entry := range mapping.entries {
		entry.key.value = entry.key.value.MapStrings(mask)
		entry.value = entry.value.MapStrings(mask)
		identity, comparable := entry.key.identity()
		if index, present := positions[identity]; comparable && present {
			copy.entries[index].value = entry.value
			continue
		}
		if comparable {
			positions[identity] = len(copy.entries)
		}
		copy.entries = append(copy.entries, entry)
	}
	return copy
}

func (input ToolInput) prepareMCPModelBatch() (ToolInput, error) {
	if input.mcpMapping != nil || input.mcpArguments.Kind() == jsonvalue.Object {
		return input, nil
	}
	value := input.mcpArguments
	if !value.Truth() {
		return DecodeModelToolInput(input.name, []byte(`{}`))
	}
	rows, array := value.Array()
	if !array {
		if value.Kind() == jsonvalue.Text {
			return ToolInput{}, ModelBatchInputFailure{kind: batchWrongPairLength, index: 0, length: 1}
		}
		return ToolInput{}, ModelBatchInputFailure{kind: batchNotIterable, rootKind: BashArgumentKind(mcpKeyType(value))}
	}
	mapping := &mcpMapping{}
	positions := make(map[string]int)
	allText := true
	for index, row := range rows {
		pair, sequence := row.Array()
		if text, textual := row.Text(); textual {
			pair = nil
			for _, r := range text {
				pair = append(pair, jsonvalue.TextValue(string(r)))
			}
			sequence = true
		}
		if !sequence {
			return ToolInput{}, ModelBatchInputFailure{kind: batchElementNotSequence, index: index}
		}
		if len(pair) != 2 {
			return ToolInput{}, ModelBatchInputFailure{kind: batchWrongPairLength, index: index, length: len(pair)}
		}
		if pair[0].Kind() == jsonvalue.Array || pair[0].Kind() == jsonvalue.Object {
			return ToolInput{}, ModelBatchInputFailure{kind: batchUnhashableKey, rootKind: BashArgumentKind(mcpKeyType(pair[0]))}
		}
		key := mcpMappingKey{pair[0]}
		allText = allText && pair[0].Kind() == jsonvalue.Text
		identity, comparable := key.identity()
		if prior, present := positions[identity]; comparable && present {
			mapping.entries[prior].value = pair[1]
			continue
		}
		if comparable {
			positions[identity] = len(mapping.entries)
		}
		mapping.entries = append(mapping.entries, mcpMappingEntry{key, pair[1]})
	}
	if allText {
		fields := make([]jsonvalue.Field, 0, len(mapping.entries))
		for _, entry := range mapping.entries {
			text, _ := entry.key.value.Text()
			fields = append(fields, jsonvalue.Field{Name: text, Value: entry.value})
		}
		input.mcpArguments = jsonvalue.ObjectValue(fields)
	} else {
		input.mcpMapping = mapping
	}
	return input, input.Validate()
}

func (mapping *mcpMapping) validate() error {
	for _, entry := range mapping.entries {
		if entry.key.value.Kind() == jsonvalue.Array || entry.key.value.Kind() == jsonvalue.Object {
			return errors.New("MCP mapping observation requires scalar keys")
		}
	}
	_, err := mapping.marshal(false, true)
	return err
}
