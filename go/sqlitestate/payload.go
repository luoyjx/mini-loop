package sqlitestate

import (
	"errors"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

// sourcePayload renders a typed encoder's JSON as spaced UTF-8 without imposing
// a container cap on a whole transcript row. String tokens are lowered separately.
func sourcePayload(data []byte) (string, error) {
	out := make([]byte, 0, len(data))
	for index := 0; index < len(data); index++ {
		if data[index] == '"' {
			start := index
			index++
			for index < len(data) && data[index] != '"' {
				if data[index] == '\\' {
					index++
				}
				index++
			}
			if index >= len(data) {
				return "", errors.New("unterminated encoded JSON string")
			}
			value, err := jsonvalue.Decode(string(data[start : index+1]))
			if err != nil {
				return "", err
			}
			text, err := value.MarshalUTF8()
			if err != nil {
				return "", err
			}
			out = append(out, text...)
		} else {
			if data[index] == ' ' || data[index] == '\n' || data[index] == '\r' || data[index] == '\t' {
				continue
			}
			out = append(out, data[index])
			if data[index] == ',' || data[index] == ':' {
				out = append(out, ' ')
			}
		}
	}
	return string(out), nil
}
