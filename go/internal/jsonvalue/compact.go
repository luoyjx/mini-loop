package jsonvalue

// LegacyString matches compact json.dumps(ensure_ascii=False) as a Python string.
// Surrogatepass code points remain observable; this is not UTF-8 wire encoding.
// MarshalLegacyUTF8 remains the strict final byte boundary.
func (v Value) LegacyString() (string, error) {
	data, err := appendLegacyString(nil, v)
	return string(data), err
}

func appendLegacyString(out []byte, v Value) ([]byte, error) {
	switch v.kind {
	case Text:
		return appendLegacyStringText(out, v.text), nil
	case Array:
		out = append(out, '[')
		for i, child := range v.items {
			if i > 0 {
				out = append(out, ',')
			}
			var err error
			out, err = appendLegacyString(out, child)
			if err != nil {
				return nil, err
			}
		}
		return append(out, ']'), nil
	case Object:
		out = append(out, '{')
		for i, member := range v.members {
			if i > 0 {
				out = append(out, ',')
			}
			out = appendLegacyStringText(out, member.name)
			out = append(out, ':')
			var err error
			out, err = appendLegacyString(out, member.value)
			if err != nil {
				return nil, err
			}
		}
		return append(out, '}'), nil
	default:
		return appendValue(out, v, true)
	}
}
