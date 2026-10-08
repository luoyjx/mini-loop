package protocol

import (
	"encoding/json"
	"errors"
	"unicode/utf8"
)

// ErrResultUTF8 marks the same final encoding refusal as the Source SDK. In-memory
// model results may retain Python surrogatepass text; JSON must never replace it.
var ErrResultUTF8 = errors.New("tool result cannot be encoded as UTF-8")

type wireResultText string

func (text wireResultText) MarshalJSON() ([]byte, error) {
	if !utf8.ValidString(string(text)) {
		return nil, ErrResultUTF8
	}
	return json.Marshal(string(text))
}
