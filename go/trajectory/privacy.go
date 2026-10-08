package trajectory

import (
	"fmt"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

var contentFields = map[string]bool{"content": true, "error": true, "input": true, "message": true, "model_input": true, "model_output": true, "output": true, "prompt": true, "summary": true, "system": true, "text": true}

// Content protection runs on closed values before UTF-8 encoding. Redacted
// surrogate text can disappear here; a surviving surrogate still fails the writer.
func protectValue(value jsonvalue.Value, key string) jsonvalue.Value {
	if contentFields[key] {
		if value.Kind() == jsonvalue.Null {
			return value
		}
		kind, size := "", 1
		switch value.Kind() {
		case jsonvalue.Text:
			text, _ := value.Text()
			return jsonvalue.TextValue(fmt.Sprintf("[redacted: %d chars]", jsonvalue.RuneCount(text)))
		case jsonvalue.Object:
			kind, size = "dict", len(value.Keys())
		case jsonvalue.Array:
			items, _ := value.Array()
			kind, size = "list", len(items)
		case jsonvalue.Integer:
			kind = "int"
		case jsonvalue.Float:
			kind = "float"
		case jsonvalue.Boolean:
			kind = "bool"
		}
		return jsonvalue.TextValue(fmt.Sprintf("[redacted: %s, %d item(s)]", kind, size))
	}
	if value.Kind() == jsonvalue.Object {
		fields := make([]jsonvalue.Field, 0, len(value.Keys()))
		for _, name := range value.Keys() {
			child, _ := value.Lookup(name)
			fields = append(fields, jsonvalue.Field{Name: name, Value: protectValue(child, name)})
		}
		return jsonvalue.ObjectValue(fields)
	}
	if items, ok := value.Array(); ok {
		for i, child := range items {
			items[i] = protectValue(child, "")
		}
		return jsonvalue.ArrayValue(items)
	}
	return value
}
