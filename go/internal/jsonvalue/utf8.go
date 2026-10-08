package jsonvalue

// MarshalUTF8 matches Python ensure_ascii=False with compact separators. It
// preserves integer/float identity and rejects nonfinite and surrogate values.
// Existing ASCII archival renderers remain unchanged.
func (v Value) MarshalUTF8() ([]byte, error) { return appendUTF8(nil, v, false) }

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
