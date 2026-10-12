package protocol

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

// PythonJSON serializes concrete boundary values using json.dumps's separators
// and string escaping. It is used for parity budgets, never to retain dynamic
// JSON in a domain object. Canonical key order is supplied by typed structs and
// encoding/json's sorted string maps.
func PythonJSON[T any](value T, ascii, compact bool) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return formatPythonJSONBytes(data, ascii, compact)
}

func formatPythonJSONBytes(data []byte, ascii, compact bool) (string, error) {
	var out strings.Builder
	out.Grow(len(data))
	for i := 0; i < len(data); {
		if data[i] == '"' {
			end := i + 1
			for end < len(data) {
				// Only quote/backslash affect the boundary of this encoded string.
				// Unmarshal below still validates and lowers all escape sequences.
				next := bytes.IndexAny(data[end:], "\"\\")
				if next < 0 {
					end = len(data)
					break
				}
				end += next
				if data[end] == '\\' {
					end += 2
					continue
				}
				break
			}
			var text string
			if err := json.Unmarshal(data[i:end+1], &text); err != nil {
				return "", err
			}
			// Reuse the closed text codec's span writes. Non-string tokens stay
			// byte-for-byte below, retaining legacy number spelling and separators.
			value := jsonvalue.TextValue(text)
			var encoded []byte
			var err error
			if ascii {
				encoded, err = value.MarshalJSON()
			} else {
				encoded, err = value.MarshalUTF8()
			}
			if err != nil {
				return "", err
			}
			out.Write(encoded)
			i = end + 1
		} else {
			out.WriteByte(data[i])
			if !compact && (data[i] == ',' || data[i] == ':') {
				out.WriteByte(' ')
			}
			i++
		}
	}
	return out.String(), nil
}
