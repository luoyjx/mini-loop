package trajectory

import (
	"bytes"
	"encoding/json"
	"fmt"
	"unicode/utf8"
)

var contentFields = map[string]bool{"content": true, "error": true, "input": true, "message": true, "model_input": true, "model_output": true, "output": true, "prompt": true, "summary": true, "system": true, "text": true}

// This transformation exists only at the JSON boundary. Domain events remain
// closed typed variants; no open JSON tree is stored in the service.
func protect(raw []byte, key string) ([]byte, error) {
	value := bytes.TrimSpace(raw)
	if contentFields[key] {
		if bytes.Equal(value, []byte("null")) {
			return []byte("null"), nil
		}
		label := ""
		switch value[0] {
		case '"':
			var text string
			if err := json.Unmarshal(value, &text); err != nil {
				return nil, err
			}
			label = fmt.Sprintf("[redacted: %d chars]", utf8.RuneCountInString(text))
		case '{':
			var object map[string]json.RawMessage
			if err := json.Unmarshal(value, &object); err != nil {
				return nil, err
			}
			label = fmt.Sprintf("[redacted: dict, %d item(s)]", len(object))
		case '[':
			var array []json.RawMessage
			if err := json.Unmarshal(value, &array); err != nil {
				return nil, err
			}
			label = fmt.Sprintf("[redacted: list, %d item(s)]", len(array))
		default:
			kind := "int"
			if bytes.ContainsAny(value, ".eE") {
				kind = "float"
			}
			if bytes.Equal(value, []byte("true")) || bytes.Equal(value, []byte("false")) {
				kind = "bool"
			}
			label = "[redacted: " + kind + ", 1 item(s)]"
		}
		return json.Marshal(label)
	}
	if len(value) == 0 {
		return nil, ErrInvalid
	}
	switch value[0] {
	case '{':
		var object map[string]json.RawMessage
		if err := json.Unmarshal(value, &object); err != nil {
			return nil, err
		}
		for name, child := range object {
			encoded, err := protect(child, name)
			if err != nil {
				return nil, err
			}
			object[name] = encoded
		}
		return json.Marshal(object)
	case '[':
		var array []json.RawMessage
		if err := json.Unmarshal(value, &array); err != nil {
			return nil, err
		}
		for i, child := range array {
			encoded, err := protect(child, "")
			if err != nil {
				return nil, err
			}
			array[i] = encoded
		}
		return json.Marshal(array)
	default:
		if value[0] == '"' {
			var text string
			if err := json.Unmarshal(value, &text); err != nil {
				return nil, err
			}
			return json.Marshal(text)
		}
		return append([]byte(nil), value...), nil
	}
}
