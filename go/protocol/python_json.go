package protocol

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf16"
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
	var out strings.Builder
	for i := 0; i < len(data); {
		if data[i] == '"' {
			end := i + 1
			for end < len(data) {
				if data[end] == '\\' {
					end += 2
					continue
				}
				if data[end] == '"' {
					break
				}
				end++
			}
			var text string
			if err := json.Unmarshal(data[i:end+1], &text); err != nil {
				return "", err
			}
			out.WriteByte('"')
			for _, r := range text {
				switch r {
				case '"':
					out.WriteString("\\\"")
				case '\\':
					out.WriteString("\\\\")
				case '\b':
					out.WriteString("\\b")
				case '\f':
					out.WriteString("\\f")
				case '\n':
					out.WriteString("\\n")
				case '\r':
					out.WriteString("\\r")
				case '\t':
					out.WriteString("\\t")
				default:
					if r < 32 || (ascii && r > 126) {
						if r > 0xffff {
							a, b := utf16.EncodeRune(r)
							fmt.Fprintf(&out, "\\u%04x\\u%04x", a, b)
						} else {
							fmt.Fprintf(&out, "\\u%04x", r)
						}
					} else {
						out.WriteRune(r)
					}
				}
			}
			out.WriteByte('"')
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
