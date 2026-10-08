package trajectory

import "github.com/luoyjx/mini-loop/go/internal/jsonvalue"

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
