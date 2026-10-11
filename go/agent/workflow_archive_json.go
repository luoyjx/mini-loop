package agent

import (
	"fmt"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

// observationMembers replaces only the inert payload. Typed metadata still passes
// its ordinary decoder; historical scalars never become live context fields.
func observationMembers(value, payload jsonvalue.Value) jsonvalue.Value {
	fields := make([]jsonvalue.Field, 0, len(value.Keys()))
	for _, key := range value.Keys() {
		member, _ := value.Lookup(key)
		if key == "payload" {
			member = payload
		}
		fields = append(fields, jsonvalue.Field{Name: key, Value: member})
	}
	return jsonvalue.ObjectValue(fields)
}

func observationHeader(data []byte) ([]byte, jsonvalue.Value, error) {
	value, err := jsonvalue.Decode(string(data))
	if err != nil {
		return nil, jsonvalue.Value{}, err
	}
	payload, ok := value.Lookup("payload")
	if value.Kind() != jsonvalue.Object || !ok || payload.Kind() != jsonvalue.Object {
		return nil, jsonvalue.Value{}, fmt.Errorf("workflow observation requires object payload")
	}
	header, err := observationMembers(value, jsonvalue.ObjectValue(nil)).MarshalJSON()
	return header, payload, err
}

func (event WorkflowEvent) validateObservation() error {
	copy := event.Clone()
	empty := jsonvalue.ObjectValue(nil)
	copy.observation = &empty
	_, err := copy.MarshalJSON()
	return err
}

// MarshalWorkflowArchiveJSON matches the Source SSE/archive JSON vocabulary,
// including nonfinite numbers and escaped surrogate text. It is deliberately
// separate from standard MarshalJSON, which continues refusing those scalars.
// Callers may mask decoded keys and values before any escaping takes place.
func (record SessionEventRecord) MarshalWorkflowArchiveJSON(mask func(string) string) ([]byte, error) {
	payload, ok := record.Event.workflow.ObservationPayload()
	if !ok {
		return nil, fmt.Errorf("record is not a workflow observation")
	}
	copy := record
	copy.Event.workflow = record.Event.workflow.Clone()
	empty := jsonvalue.ObjectValue(nil)
	copy.Event.workflow.observation = &empty
	header, err := copy.MarshalJSON()
	if err != nil {
		return nil, err
	}
	value, err := jsonvalue.Decode(string(header))
	if err != nil {
		return nil, err
	}
	value = observationMembers(value, payload)
	if mask != nil {
		value = value.MapStrings(mask)
	}
	return jsonvalue.AppendLegacyDefault(value)
}
