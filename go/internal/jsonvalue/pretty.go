package jsonvalue

import "strings"

// LegacyStringIndent is Python's ensure_ascii=False, indent=2 string projection.
// Surrogatepass characters remain in the string until its final UTF-8 boundary.
func (v Value) LegacyStringIndent() (string, error) {
	data, err := appendLegacyStringIndent(nil, v, 0)
	return string(data), err
}
func appendLegacyStringText(out []byte, text string) []byte {
	out = append(out, '"')
	for len(text) > 0 {
		r, n := textRune(text)
		text = text[n:]
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
func appendLegacyStringIndent(out []byte, v Value, depth int) ([]byte, error) {
	if v.kind == Text {
		return appendLegacyStringText(out, v.text), nil
	}
	if v.kind != Array && v.kind != Object {
		return appendValue(out, v, true)
	}
	count := len(v.items)
	open, close := byte('['), byte(']')
	if v.kind == Object {
		count = len(v.members)
		open, close = '{', '}'
	}
	out = append(out, open)
	if count == 0 {
		return append(out, close), nil
	}
	for i := 0; i < count; i++ {
		if i > 0 {
			out = append(out, ',')
		}
		out = append(out, '\n')
		out = append(out, strings.Repeat("  ", depth+1)...)
		child := Value{}
		if v.kind == Object {
			out = appendLegacyStringText(out, v.members[i].name)
			out = append(out, ':', ' ')
			child = v.members[i].value
		} else {
			child = v.items[i]
		}
		var err error
		out, err = appendLegacyStringIndent(out, child, depth+1)
		if err != nil {
			return nil, err
		}
	}
	out = append(out, '\n')
	out = append(out, strings.Repeat("  ", depth)...)
	return append(out, close), nil
}
