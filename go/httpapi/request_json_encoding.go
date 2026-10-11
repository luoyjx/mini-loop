package httpapi

import (
	"bytes"
	"encoding/binary"
	"errors"
	"unicode/utf16"
	"unicode/utf8"
)

type requestJSONEncoding uint8

const (
	requestUTF8 requestJSONEncoding = iota
	requestUTF16LE
	requestUTF16BE
	requestUTF32LE
	requestUTF32BE
)

var errRequestJSONEncoding = errors.New("invalid JSON request encoding")

// BOM precedence and the initial NUL heuristic match json.detect_encoding.
// Charset parameters do not override byte detection in the source request path.
func detectRequestJSONEncoding(raw []byte) (requestJSONEncoding, int) {
	switch {
	case bytes.HasPrefix(raw, []byte{0, 0, 0xfe, 0xff}):
		return requestUTF32BE, 4
	case bytes.HasPrefix(raw, []byte{0xff, 0xfe, 0, 0}):
		return requestUTF32LE, 4
	case bytes.HasPrefix(raw, []byte{0xfe, 0xff}):
		return requestUTF16BE, 2
	case bytes.HasPrefix(raw, []byte{0xff, 0xfe}):
		return requestUTF16LE, 2
	case bytes.HasPrefix(raw, []byte{0xef, 0xbb, 0xbf}):
		return requestUTF8, 3
	}
	if len(raw) >= 4 {
		if raw[0] == 0 {
			if raw[1] == 0 {
				return requestUTF32BE, 0
			}
			return requestUTF16BE, 0
		}
		if raw[1] == 0 {
			if raw[2] == 0 && raw[3] == 0 {
				return requestUTF32LE, 0
			}
			return requestUTF16LE, 0
		}
	} else if len(raw) == 2 {
		if raw[0] == 0 {
			return requestUTF16BE, 0
		}
		if raw[1] == 0 {
			return requestUTF16LE, 0
		}
	}
	return requestUTF8, 0
}

func decodeRequestJSONEncoding(raw []byte) ([]byte, error) {
	encoding, skip := detectRequestJSONEncoding(raw)
	raw = raw[skip:]
	if encoding == requestUTF8 {
		for remaining := raw; len(remaining) > 0; {
			_, size, ok := requestRune(remaining)
			if !ok {
				return nil, errRequestJSONEncoding
			}
			remaining = remaining[size:]
		}
		return raw, nil
	}
	width := 2
	if encoding == requestUTF32LE || encoding == requestUTF32BE {
		width = 4
	}
	if len(raw)%width != 0 {
		return nil, errRequestJSONEncoding
	}
	decoded := make([]byte, 0, len(raw))
	for index := 0; index < len(raw); index += width {
		var value rune
		if width == 4 {
			var code uint32
			if encoding == requestUTF32LE {
				code = binary.LittleEndian.Uint32(raw[index:])
			} else {
				code = binary.BigEndian.Uint32(raw[index:])
			}
			if code > utf8.MaxRune {
				return nil, errRequestJSONEncoding
			}
			value = rune(code)
		} else {
			var code uint16
			if encoding == requestUTF16LE {
				code = binary.LittleEndian.Uint16(raw[index:])
			} else {
				code = binary.BigEndian.Uint16(raw[index:])
			}
			value = rune(code)
			if value >= 0xd800 && value <= 0xdbff && index+2 < len(raw) {
				var low uint16
				if encoding == requestUTF16LE {
					low = binary.LittleEndian.Uint16(raw[index+2:])
				} else {
					low = binary.BigEndian.Uint16(raw[index+2:])
				}
				if low >= 0xdc00 && low <= 0xdfff {
					value = utf16.DecodeRune(value, rune(low))
					index += 2
				}
			}
		}
		decoded = appendRequestRune(decoded, value)
	}
	return decoded, nil
}
