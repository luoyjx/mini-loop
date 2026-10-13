package protocol

import (
	"encoding/json"
	"errors"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

// InputObservation is a detached SDK input snapshot, never executable arguments.
// Standard JSON encoding still refuses its nonfinite numbers. LegacyJSON is only
// for inspection and archival projections, not HTTP request encoding.
type InputObservation struct {
	value      jsonvalue.Value
	mcpMapping *mcpMapping
}

func (observation InputObservation) LegacyJSON() ([]byte, error) {
	if observation.mcpMapping != nil {
		return observation.mcpMapping.marshal(false, true)
	}
	return observation.value.MarshalLegacyUTF8()
}
func (observation InputObservation) MarshalJSON() ([]byte, error) {
	if observation.mcpMapping != nil {
		return observation.mcpMapping.marshal(false, false)
	}
	return observation.value.MarshalJSON()
}

// MapStrings transforms a detached inspection value, never executable input.
func (observation InputObservation) MapStrings(mask func(string) string) InputObservation {
	if mask == nil {
		return observation
	}
	copy := InputObservation{value: observation.value.MapStrings(mask)}
	if observation.mcpMapping != nil {
		copy.mcpMapping = observation.mcpMapping.masked(mask)
	}
	return copy
}

func (observation InputObservation) SortedPythonJSON() (string, error) {
	if observation.mcpMapping != nil {
		data, err := observation.mcpMapping.marshal(true, true)
		if err != nil {
			return "", err
		}
		return formatPythonJSONBytes(data, false, false)
	}
	data, err := observation.value.Sorted().MarshalLegacyUTF8()
	if err != nil {
		return "", err
	}
	return formatPythonJSONBytes(data, false, false)
}
func (input ToolInput) Observation() (InputObservation, error) {
	if err := input.Validate(); err != nil {
		return InputObservation{}, err
	}
	if IsMCPToolName(input.name) {
		return InputObservation{value: input.mcpArguments, mcpMapping: input.mcpMapping}, nil
	}
	if input.name == ToolBash && (input.bashBinding != nil || input.bashCommand != nil || input.bashBackground != nil || input.bashApproval != nil) {
		return InputObservation{value: input.bashObservation()}, nil
	}
	if input.backgroundModel != nil {
		value, err := input.backgroundObservation()
		return InputObservation{value: value}, err
	}
	data, err := input.MarshalJSON()
	if err != nil {
		return InputObservation{}, err
	}
	value, err := jsonvalue.Decode(string(data))
	if err != nil {
		return InputObservation{}, err
	}
	return InputObservation{value: value}, nil
}
func observationFields(base, extra jsonvalue.Value) jsonvalue.Value {
	fields := make([]jsonvalue.Field, 0, len(base.Keys())+len(extra.Keys()))
	for _, key := range base.Keys() {
		v, _ := base.Lookup(key)
		fields = append(fields, jsonvalue.Field{Name: key, Value: v})
	}
	for _, key := range extra.Keys() {
		v, _ := extra.Lookup(key)
		fields = append(fields, jsonvalue.Field{Name: key, Value: v})
	}
	return jsonvalue.ObjectValue(fields)
}
func replaceObservationField(base jsonvalue.Value, key string, value jsonvalue.Value) jsonvalue.Value {
	fields := make([]jsonvalue.Field, 0, len(base.Keys()))
	for _, name := range base.Keys() {
		v, _ := base.Lookup(name)
		if name == key {
			v = value
		}
		fields = append(fields, jsonvalue.Field{Name: name, Value: v})
	}
	return jsonvalue.ObjectValue(fields)
}

// DecodeSDKServerToolInput preserves SDK observation numbers without granting any
// client dispatch authority. DecodeServerToolInput remains finite admission.
func DecodeSDKServerToolInput(data []byte) (ServerToolInput, error) {
	return decodeSDKServerToolInput(data, true)
}

func decodeSDKServerToolInput(data []byte, bounded bool) (ServerToolInput, error) {
	if bounded && len(data) > MaxWireBytes {
		return ServerToolInput{}, errors.New("server tool input exceeds wire bound")
	}
	value, err := jsonvalue.Decode(string(data))
	if err != nil {
		return ServerToolInput{}, err
	}
	if _, err = value.MarshalLegacyUTF8(); err != nil {
		return ServerToolInput{}, err
	}
	return ServerToolInput{value: value}, nil
}
func (input ServerToolInput) Observation() InputObservation {
	return InputObservation{value: input.value}
}

// MarshalObservationJSON projects SDK tool observations, retaining legacy numbers.
// MarshalJSON remains the finite HTTP boundary and never emits NaN or Infinity.
func (block Block) MarshalObservationJSON() ([]byte, error) {
	value, err := block.observationValue()
	if err != nil {
		return nil, err
	}
	return value.MarshalLegacyUTF8()
}
func (block Block) observationValue() (jsonvalue.Value, error) {
	if err := block.Validate(); err != nil {
		return jsonvalue.Value{}, err
	}
	var data []byte
	var observed jsonvalue.Value
	var err error
	switch block.kind {
	case BlockServerToolUse:
		copy := block.serverTool.clone()
		observed = copy.Input.value
		copy.Input = ServerToolInput{value: jsonvalue.NullValue()}
		data, err = copy.MarshalJSON()
	case BlockToolUse:
		snapshot, observeErr := block.toolUse.Input.Observation()
		if observeErr != nil {
			return jsonvalue.Value{}, observeErr
		}
		observed = snapshot.value
		if snapshot.mcpMapping != nil {
			projection, projectionErr := snapshot.LegacyJSON()
			if projectionErr != nil {
				return jsonvalue.Value{}, projectionErr
			}
			observed, err = jsonvalue.Decode(string(projection))
			if err != nil {
				return jsonvalue.Value{}, err
			}
		}
		var caller *wireOptional[ToolCaller]
		if block.callerPresent {
			caller = &wireOptional[ToolCaller]{value: block.toolUse.Caller}
		}
		data, err = json.Marshal(struct {
			Type   BlockKind                 `json:"type"`
			ID     string                    `json:"id"`
			Name   ToolName                  `json:"name"`
			Input  jsonvalue.Value           `json:"input"`
			Caller *wireOptional[ToolCaller] `json:"caller,omitempty"`
		}{BlockToolUse, block.toolUse.ID, block.toolUse.Name, jsonvalue.NullValue(), caller})
	default:
		data, err = block.MarshalJSON()
	}
	if err != nil {
		return jsonvalue.Value{}, err
	}
	value, err := jsonvalue.Decode(string(data))
	if err != nil {
		return jsonvalue.Value{}, err
	}
	if block.kind == BlockToolUse || block.kind == BlockServerToolUse {
		value = replaceObservationField(value, "input", observed)
	}
	return value, nil
}

// MarshalObservedBlocks encodes the SDK snapshot rather than an HTTP request.
func MarshalObservedBlocks(blocks []Block) ([]byte, error) {
	values := make([]jsonvalue.Value, len(blocks))
	for i, block := range blocks {
		v, err := block.observationValue()
		if err != nil {
			return nil, err
		}
		values[i] = v
	}
	return jsonvalue.ArrayValue(values).MarshalLegacyUTF8()
}

// WithSDKObservedInput replaces only decoded SDK input, preserving the closed
// block kind, caller metadata and lack of new execution authority.
func (block Block) WithSDKObservedInput(data []byte) (Block, error) {
	blocks, _ := BlockContent(block).Clone().Blocks()
	copy := blocks[0]
	switch copy.kind {
	case BlockToolUse:
		input, err := DecodeModelToolInput(copy.toolUse.Name, data)
		if err != nil {
			return Block{}, err
		}
		copy.toolUse.Input = input
	case BlockServerToolUse:
		input, err := DecodeSDKServerToolInput(data)
		if err != nil {
			return Block{}, err
		}
		copy.serverTool.Input = input
	default:
		return Block{}, errors.New("SDK input requires a tool observation block")
	}
	return copy, copy.Validate()
}
