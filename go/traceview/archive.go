package traceview

import "github.com/luoyjx/mini-loop/go/internal/jsonvalue"

func replaceEvents(value, events jsonvalue.Value) jsonvalue.Value {
	fields := make([]jsonvalue.Field, 0, len(value.Keys()))
	for _, name := range value.Keys() {
		child, _ := value.Lookup(name)
		if name == "events" || name == "Events" {
			child = events
		}
		fields = append(fields, jsonvalue.Field{Name: name, Value: child})
	}
	return jsonvalue.ObjectValue(fields)
}
