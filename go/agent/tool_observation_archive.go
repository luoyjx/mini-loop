package agent

import (
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
)

// MarshalArchiveJSON preserves deferred Bash binding observations in the event
// store. It does not relax standard request JSON or reconstruct live authority.
func (record SessionEventRecord) MarshalArchiveJSON() ([]byte, error) {
	if record.Event.kind != EventToolUse || record.Event.toolUse.Name != protocol.ToolBash {
		return record.MarshalJSON()
	}
	observation, err := record.Event.toolUse.Input.Observation()
	if err != nil {
		return nil, err
	}
	inputJSON, err := observation.LegacyJSON()
	if err != nil {
		return nil, err
	}
	input, err := jsonvalue.Decode(string(inputJSON))
	if err != nil {
		return nil, err
	}
	if _, err = input.MarshalJSON(); err == nil {
		return record.MarshalJSON()
	}
	copy := record
	copy.Event = record.Event.clone()
	copy.Event.toolUse.Input = protocol.BashToolInput(protocol.BashInput{Command: "archive-placeholder"})
	data, err := copy.MarshalJSON()
	if err != nil {
		return nil, err
	}
	value, err := jsonvalue.Decode(string(data))
	if err != nil {
		return nil, err
	}
	return replaceToolObservationInput(value, input).MarshalLegacyUTF8()
}
func replaceToolObservationInput(value, input jsonvalue.Value) jsonvalue.Value {
	fields := make([]jsonvalue.Field, 0, len(value.Keys()))
	for _, key := range value.Keys() {
		v, _ := value.Lookup(key)
		if key == "input" {
			v = input
		}
		fields = append(fields, jsonvalue.Field{Name: key, Value: v})
	}
	return jsonvalue.ObjectValue(fields)
}
func decodeLegacyToolObservationEvent(value jsonvalue.Value) (SessionEventRecord, error) {
	input, _ := value.Lookup("input")
	metadata, err := replaceToolObservationInput(value, jsonvalue.NullValue()).MarshalJSON()
	if err != nil {
		return SessionEventRecord{}, err
	}
	record, err := DecodeStoredEvent(metadata)
	if err != nil {
		return SessionEventRecord{}, err
	}
	original, err := input.MarshalLegacyUTF8()
	if err != nil {
		return SessionEventRecord{}, err
	}
	observed, err := protocol.DecodeModelToolInput(record.Event.toolUse.Name, original)
	if err != nil {
		return SessionEventRecord{}, err
	}
	record.Event.toolUse.Input = observed
	return record, nil
}
