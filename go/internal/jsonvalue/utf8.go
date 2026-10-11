package jsonvalue

import (
	"strings"
	"unicode/utf8"
)

// MarshalUTF8 matches Python ensure_ascii=False with compact separators. It
// preserves integer/float identity and rejects nonfinite and surrogate values.
// Existing ASCII archival renderers remain unchanged.
func (v Value) MarshalUTF8() ([]byte, error) { return appendUTF8(nil, v, false) }

// MarshalUTF8Spaced matches json.dumps(ensure_ascii=False) with default spacing.
// It retains literal Unicode and rejects text that SQLite cannot encode as UTF-8.
func (v Value) MarshalUTF8Spaced() ([]byte, error) {
	compact, err := v.MarshalUTF8()
	if err != nil {
		return nil, err
	}
	return spacedSeparators(compact), nil
}

// MarshalLegacyUTF8 matches Python archival ensure_ascii=False: nonfinite
// numbers are retained, but unpaired surrogate text cannot become UTF-8 bytes.
func (v Value) MarshalLegacyUTF8() ([]byte, error) { return appendUTF8(nil, v, true) }

func appendUTF8(out []byte, v Value, legacy bool) ([]byte, error) {
	switch v.kind {
	case Text:
		if surrogateText(v.text) {
			return nil, ErrSurrogate
		}
		return appendTextUTF8(out, v.text), nil
	case Array:
		out = append(out, '[')
		for i, item := range v.items {
			if i > 0 {
				out = append(out, ',')
			}
			var err error
			out, err = appendUTF8(out, item, legacy)
			if err != nil {
				return nil, err
			}
		}
		return append(out, ']'), nil
	case Object:
		out = append(out, '{')
		for i, m := range v.members {
			if surrogateText(m.name) {
				return nil, ErrSurrogate
			}
			if i > 0 {
				out = append(out, ',')
			}
			out = appendTextUTF8(out, m.name)
			out = append(out, ':')
			var err error
			out, err = appendUTF8(out, m.value, legacy)
			if err != nil {
				return nil, err
			}
		}
		return append(out, '}'), nil
	default:
		return appendValue(out, v, legacy)
	}
}
func appendTextUTF8(out []byte, text string) []byte {
	if !utf8.ValidString(text) {
		// Raw native strings historically replace malformed bytes with RuneError.
		// Surrogatepass refusal is checked by the caller before this projection.
		return appendTextUTF8Runes(out, text)
	}
	const escaped = "\"\\\x00\x01\x02\x03\x04\x05\x06\x07\x08\x09\x0a\x0b\x0c\x0d\x0e\x0f\x10\x11\x12\x13\x14\x15\x16\x17\x18\x19\x1a\x1b\x1c\x1d\x1e\x1f"
	out = append(out, '"')
	for len(text) > 0 {
		end := strings.IndexAny(text, escaped)
		if end < 0 {
			out = append(out, text...)
			break
		}
		out = append(out, text[:end]...)
		c := text[end]
		switch c {
		case '"', '\\':
			out = append(out, '\\', c)
		case '\b':
			out = append(out, '\\', 'b')
		case '\f':
			out = append(out, '\\', 'f')
		case '\n':
			out = append(out, '\\', 'n')
		case '\r':
			out = append(out, '\\', 'r')
		case '\t':
			out = append(out, '\\', 't')
		default:
			out = appendHex(out, rune(c))
		}
		text = text[end+1:]
	}
	return append(out, '"')
}

func appendTextUTF8Runes(out []byte, text string) []byte {
	out = append(out, '"')
	for _, r := range text {
		switch r {
		case '"', '\\':
			out = append(out, '\\', byte(r))
		case '\b':
			out = append(out, '\\', 'b')
		case '\f':
			out = append(out, '\\', 'f')
		case '\n':
			out = append(out, '\\', 'n')
		case '\r':
			out = append(out, '\\', 'r')
		case '\t':
			out = append(out, '\\', 't')
		default:
			if r < 32 {
				out = appendHex(out, r)
			} else {
				out = appendRune(out, r)
			}
		}
	}
	return append(out, '"')
}
