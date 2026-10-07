package jsonvalue

import (
	"github.com/luoyjx/mini-loop/go/internal/pytext"
	"math"
	"strconv"
	"strings"
)

// Truth is Python truthiness for the closed JSON variants, including legacy NaN.
func (v Value) Truth() bool {
	switch v.kind {
	case Text, Integer:
		return v.text != "" && (v.kind != Integer || v.text != "0")
	case Float:
		return v.number != 0
	case Boolean:
		return v.boolean
	case Array:
		return len(v.items) > 0
	case Object:
		return len(v.members) > 0
	}
	return false
}

// PythonString projects legacy response content through str(value). Nested text
// uses Python repr quoting, including surrogatepass escaping. It grants no authority.
func (v Value) PythonString() string {
	if v.kind == Text {
		return v.text
	}
	return string(appendPythonRepr(nil, v))
}
func appendPythonRepr(out []byte, v Value) []byte {
	switch v.kind {
	case Null:
		return append(out, "None"...)
	case Boolean:
		if v.boolean {
			return append(out, "True"...)
		}
		return append(out, "False"...)
	case Text:
		return appendPythonText(out, v.text)
	case Float:
		if math.IsNaN(v.number) {
			return append(out, "nan"...)
		}
		if math.IsInf(v.number, 1) {
			return append(out, "inf"...)
		}
		if math.IsInf(v.number, -1) {
			return append(out, "-inf"...)
		}
	case Array:
		out = append(out, '[')
		for i, item := range v.items {
			if i > 0 {
				out = append(out, ',', ' ')
			}
			out = appendPythonRepr(out, item)
		}
		return append(out, ']')
	case Object:
		out = append(out, '{')
		for i, item := range v.members {
			if i > 0 {
				out = append(out, ',', ' ')
			}
			out = appendPythonText(out, item.name)
			out = append(out, ':', ' ')
			out = appendPythonRepr(out, item.value)
		}
		return append(out, '}')
	}
	data, _ := appendValue(out, v, true)
	return data
}
func appendPythonText(out []byte, text string) []byte {
	quote := byte('\'')
	if strings.Contains(text, "'") && !strings.Contains(text, `"`) {
		quote = '"'
	}
	out = append(out, quote)
	for len(text) > 0 {
		r, n := textRune(text)
		text = text[n:]
		switch {
		case r == rune(quote) || r == '\\':
			out = append(out, '\\', byte(r))
		case r == '\n':
			out = append(out, '\\', 'n')
		case r == '\r':
			out = append(out, '\\', 'r')
		case r == '\t':
			out = append(out, '\\', 't')
		case !pytext.Printable(r):
			width, prefix := 2, byte('x')
			if r > 0xff {
				width, prefix = 4, 'u'
			}
			if r > 0xffff {
				width, prefix = 8, 'U'
			}
			hex := strconv.FormatInt(int64(r), 16)
			out = append(out, '\\', prefix)
			out = append(out, strings.Repeat("0", width-len(hex))...)
			out = append(out, hex...)
		default:
			out = appendRune(out, r)
		}
	}
	return append(out, quote)
}

// TextPrefix counts surrogatepass code points exactly as the legacy text decoder.
func TextPrefix(text string, count int) string {
	offset := 0
	for offset < len(text) && count > 0 {
		_, n := textRune(text[offset:])
		offset += n
		count--
	}
	return text[:offset]
}

// AppendLegacyIndent matches json.dumps(indent=2), retaining member order and
// ASCII escaping, including historical nonfinite/surrogate values.
func AppendLegacyIndent(v Value) ([]byte, error) { return appendIndented(nil, v, 0) }
func appendIndented(out []byte, v Value, depth int) ([]byte, error) {
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
		value := Value{}
		if v.kind == Object {
			out = appendText(out, v.members[i].name)
			out = append(out, ':', ' ')
			value = v.members[i].value
		} else {
			value = v.items[i]
		}
		var err error
		out, err = appendIndented(out, value, depth+1)
		if err != nil {
			return nil, err
		}
	}
	out = append(out, '\n')
	out = append(out, strings.Repeat("  ", depth)...)
	return append(out, close), nil
}
