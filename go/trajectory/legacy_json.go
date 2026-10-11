package trajectory

import (
	"encoding/json"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

// Only fields needed by a streaming classification enter its typed envelope.
// The original record bytes remain available to visitors and document assembly.
func scanEnvelope(line []byte) ([]byte, error) {
	value, err := jsonvalue.Decode(string(line))
	if err != nil {
		return nil, err
	}
	if value.Kind() != jsonvalue.Object {
		return nil, ErrInvalid
	}
	fields := []jsonvalue.Field{}
	for _, name := range []string{"record_type", "type", "denied"} {
		if child, ok := value.Lookup(name); ok {
			fields = append(fields, jsonvalue.Field{Name: name, Value: child})
		}
	}
	if child, ok := value.Lookup("error"); ok {
		fields = append(fields, jsonvalue.Field{Name: "error", Value: jsonvalue.BoolValue(child.Truth())})
	}
	return jsonvalue.ObjectValue(fields).MarshalJSON()
}

// scanTypedRecord discards fields not used by the streaming summary. Archival
// metadata and metrics remain available from the original line to JSON readers.
func scanTypedRecord(line []byte, start bool) ([]byte, error) {
	value, err := jsonvalue.Decode(string(line))
	if err != nil {
		return nil, err
	}
	names := []string{"record_type", "trajectory_id", "trace_id", "group_id", "session", "owner",
		"status", "ended_at", "duration_ms", "output", "error", "metrics"}
	if start {
		names = []string{"record_type", "schema_version", "trajectory_id", "trace_id", "group_id", "session", "owner",
			"run_index", "started_at", "input", "metadata"}
	}
	projection := value.Select(names...)
	fields := make([]jsonvalue.Field, 0, len(projection.Keys()))
	for _, name := range projection.Keys() {
		child, _ := projection.Lookup(name)
		if name == "metadata" {
			// Historical summary fields are retained separately, not decoded into
			// producer settings. Native text fields are restored from closed text.
			continue
		}
		if name == "metrics" {
			counters := []jsonvalue.Field{}
			for _, key := range []string{"event_count", "model_calls", "tool_calls", "tool_errors", "errors"} {
				count, _ := child.Lookup(key)
				if count.Kind() == jsonvalue.Integer {
					if encoded, err := count.MarshalJSON(); err == nil {
						var native int
						if json.Unmarshal(encoded, &native) == nil {
							counters = append(counters, jsonvalue.Field{Name: key, Value: count})
						}
					}
				}
			}
			child = jsonvalue.ObjectValue(counters)
		}
		fields = append(fields, jsonvalue.Field{Name: name, Value: child})
	}
	return jsonvalue.ObjectValue(fields).MarshalJSON()
}
