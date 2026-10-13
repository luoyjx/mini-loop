package protocol

import (
	"errors"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

// SDKCallerObservation is immutable provider metadata. Its closed JSON value
// can retain SDK fallback roots and legacy numbers, never execution authority.
type SDKCallerObservation struct{ value jsonvalue.Value }

func (observation SDKCallerObservation) Value() jsonvalue.Value { return observation.value }
func (observation SDKCallerObservation) LegacyJSON() ([]byte, error) {
	return observation.value.MarshalLegacyUTF8()
}
func (observation SDKCallerObservation) MarshalJSON() ([]byte, error) {
	return observation.value.MarshalJSON()
}

// DecodeSDKToolCaller applies the fixed SDK's caller construction defaults.
// Public ToolCaller.UnmarshalJSON keeps explicit finite, declared admission.
func DecodeSDKToolCaller(data []byte) (ToolCaller, error) {
	return decodeObservedToolCaller(data, true, true)
}

func decodeObservedToolCaller(data []byte, bounded, defaults bool) (ToolCaller, error) {
	if bounded && len(data) > MaxWireBytes {
		return ToolCaller{}, errors.New("SDK caller exceeds wire bound")
	}
	value, err := jsonvalue.Decode(string(data))
	if err != nil {
		return ToolCaller{}, err
	}
	if defaults {
		value = sdkCallerDefaults(value, false)
	}
	observation := SDKCallerObservation{value: value}
	if _, err := observation.LegacyJSON(); err != nil {
		return ToolCaller{}, err
	}
	return ToolCaller{sdkObservation: &observation}, nil
}

func sdkCallerDefaults(value jsonvalue.Value, firstBranch bool) jsonvalue.Value {
	if items, ok := value.Array(); ok {
		for i, item := range items {
			// The SDK constructs array elements as the first DirectCaller
			// model, even when an element carries a code-execution tag.
			// Non-mapping elements, including nested arrays, are kept raw.
			if item.Kind() == jsonvalue.Object {
				items[i] = sdkCallerDefaults(item, true)
			}
		}
		return jsonvalue.ArrayValue(items)
	}
	if value.Kind() != jsonvalue.Object {
		return value
	}
	if _, present := value.Lookup("type"); !present {
		value = appendCallerField(value, "type", jsonvalue.NullValue())
	}
	typeValue, _ := value.Lookup("type")
	tag, isText := typeValue.Text()
	if !firstBranch && isText && (tag == string(CallerCodeExecution2025) || tag == string(CallerCodeExecution2026)) {
		if _, present := value.Lookup("tool_id"); !present {
			value = appendCallerField(value, "tool_id", jsonvalue.NullValue())
		}
	}
	return value
}

func appendCallerField(value jsonvalue.Value, name string, field jsonvalue.Value) jsonvalue.Value {
	fields := make([]jsonvalue.Field, 0, len(value.Keys())+1)
	for _, key := range value.Keys() {
		v, _ := value.Lookup(key)
		fields = append(fields, jsonvalue.Field{Name: key, Value: v})
	}
	return jsonvalue.ObjectValue(append(fields, jsonvalue.Field{Name: name, Value: field}))
}

// WithSDKObservedCaller replaces only caller metadata. Tool identity, input,
// outer block kind and current execution authority are unchanged.
func (block Block) WithSDKObservedCaller(data []byte) (Block, error) {
	caller, err := DecodeSDKToolCaller(data)
	if err != nil {
		return Block{}, err
	}
	return block.withObservedCaller(caller)
}

func (block Block) withObservedCaller(caller ToolCaller) (Block, error) {
	blocks, _ := BlockContent(block).Clone().Blocks()
	copy := blocks[0]
	var pointer *ToolCaller
	if caller.sdkObservation == nil || caller.sdkObservation.value.Kind() != jsonvalue.Null {
		pointer = &caller
	}
	if !copy.setCaller(pointer, true) {
		return Block{}, errors.New("SDK caller requires a caller-bearing observation block")
	}
	return copy, copy.Validate()
}

func (block Block) callerMetadata() (*ToolCaller, bool) {
	switch block.kind {
	case BlockToolUse:
		return block.toolUse.Caller, block.callerPresent
	case BlockServerToolUse:
		return block.serverTool.Caller, block.serverTool.callerPresent
	case BlockWebSearchToolResult:
		return block.webSearch.Caller, block.webSearch.callerPresent
	case BlockWebFetchToolResult:
		return block.webFetch.Caller, block.webFetch.callerPresent
	default:
		return nil, false
	}
}

// setCaller is used only on a detached block or during historical construction.
func (block *Block) setCaller(caller *ToolCaller, present bool) bool {
	switch block.kind {
	case BlockToolUse:
		block.toolUse.Caller, block.callerPresent = caller, present
	case BlockServerToolUse:
		block.serverTool.Caller, block.serverTool.callerPresent = caller, present
	case BlockWebSearchToolResult:
		block.webSearch.Caller, block.webSearch.callerPresent = caller, present
	case BlockWebFetchToolResult:
		block.webFetch.Caller, block.webFetch.callerPresent = caller, present
	default:
		return false
	}
	return true
}

func (caller ToolCaller) observationValue() (jsonvalue.Value, error) {
	if caller.sdkObservation != nil {
		return caller.sdkObservation.value, caller.Validate()
	}
	data, err := caller.MarshalJSON()
	if err != nil {
		return jsonvalue.Value{}, err
	}
	return jsonvalue.Decode(string(data))
}
