package jsonvalue

import (
	"errors"
	"math"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// Value is an immutable closed projection of Python JSON data. Integer and float
// variants remain distinct; historical nonfinite/surrogate values remain observable
// but fail standard JSON serialization. Slice accessors detach their container.
type Value struct {
	kind    Kind
	text    string // Strings use UTF-8 with surrogatepass for legacy lone surrogates.
	number  float64
	boolean bool
	items   []Value
	members []member
}
type Kind uint8

const (
	Null Kind = iota
	Text
	Integer
	Float
	Boolean
	Array
	Object
)

type member struct {
	name  string
	value Value
}

func (v Value) Kind() Kind              { return v.kind }
func (v Value) Text() (string, bool)    { return v.text, v.kind == Text }
func (v Value) Integer() (string, bool) { return v.text, v.kind == Integer }
func (v Value) Float() (float64, bool)  { return v.number, v.kind == Float }
func (v Value) Bool() (bool, bool)      { return v.boolean, v.kind == Boolean }
func (v Value) Array() ([]Value, bool) {
	if v.kind != Array {
		return nil, false
	}
	return append([]Value{}, v.items...), true
}
func (v Value) Keys() []string {
	if v.kind != Object {
		return nil
	}
	keys := make([]string, len(v.members))
	for i, m := range v.members {
		keys[i] = m.name
	}
	return keys
}
func (v Value) Lookup(key string) (Value, bool) {
	for _, m := range v.members {
		if m.name == key {
			return m.value, true
		}
	}
	return Value{}, false
}

// The source integer limit is fixed by the pinned CPython runtime. Its recursion
// threshold depends on Python call-stack depth; the Go compatibility profile
// accepts 1000 nested containers. Tests pin 500/1005, not an exact stack boundary.
const integerDigitLimit = 4300
const containerLimit = 1000

var ErrSyntax = errors.New("invalid archive JSON")
var ErrInteger = errors.New("archive integer exceeds the source 4300-digit limit")
var ErrDepth = errors.New("archive JSON exceeds the nesting limit")
var ErrSurrogate = errors.New("archive text contains a non-scalar Unicode value")
var ErrNonfinite = errors.New("archive value cannot be serialized as finite JSON")

// MarshalJSON preserves legacy data and source member order. Nonfinite floats
// remain observable through Float but cannot enter a standard HTTP JSON response.
// Lone-surrogate text remains readable but also fails the source UTF-8 HTTP boundary.
func (v Value) MarshalJSON() ([]byte, error) { return appendValue(nil, v, false) }
func appendValue(out []byte, v Value, allowLegacy bool) ([]byte, error) {
	switch v.kind {
	case Null:
		return append(out, "null"...), nil
	case Text:
		if !allowLegacy && surrogateText(v.text) {
			return nil, ErrSurrogate
		}
		return appendText(out, v.text), nil
	case Integer:
		return append(out, v.text...), nil
	case Float:
		if math.IsNaN(v.number) || math.IsInf(v.number, 0) {
			if !allowLegacy {
				return nil, ErrNonfinite
			}
			s := "NaN"
			if math.IsInf(v.number, 1) {
				s = "Infinity"
			} else if math.IsInf(v.number, -1) {
				s = "-Infinity"
			}
			return append(out, s...), nil
		}
		format := byte('f')
		a := math.Abs(v.number)
		if a != 0 && (a < 1e-4 || a >= 1e16) {
			format = 'e'
		}
		number := strconv.FormatFloat(v.number, format, -1, 64)
		if format == 'f' && !strings.Contains(number, ".") {
			number += ".0"
		}
		return append(out, number...), nil
	case Boolean:
		return strconv.AppendBool(out, v.boolean), nil
	case Array:
		out = append(out, '[')
		for i, item := range v.items {
			if i > 0 {
				out = append(out, ',')
			}
			var err error
			out, err = appendValue(out, item, allowLegacy)
			if err != nil {
				return nil, err
			}
		}
		return append(out, ']'), nil
	case Object:
		out = append(out, '{')
		for i, m := range v.members {
			if i > 0 {
				out = append(out, ',')
			}
			if !allowLegacy && surrogateText(m.name) {
				return nil, ErrSurrogate
			}
			out = appendText(out, m.name)
			out = append(out, ':')
			var err error
			out, err = appendValue(out, m.value, allowLegacy)
			if err != nil {
				return nil, err
			}
		}
		return append(out, '}'), nil
	}
	return nil, ErrSyntax
}
func appendHex(out []byte, r rune) []byte {
	const hex = "0123456789abcdef"
	return append(out, '\\', 'u', hex[(r>>12)&15], hex[(r>>8)&15], hex[(r>>4)&15], hex[r&15])
}
func textRune(raw string) (rune, int) {
	if len(raw) >= 3 && raw[0] == 0xed && raw[1] >= 0xa0 && raw[1] <= 0xbf && raw[2]&0xc0 == 0x80 {
		return rune(raw[0]&15)<<12 | rune(raw[1]&63)<<6 | rune(raw[2]&63), 3
	}
	return utf8.DecodeRuneInString(raw)
}
func surrogateText(text string) bool {
	for len(text) > 0 {
		r, n := textRune(text)
		if r >= 0xd800 && r <= 0xdfff {
			return true
		}
		text = text[n:]
	}
	return false
}
func appendRune(out []byte, r rune) []byte {
	if r >= 0xd800 && r <= 0xdfff {
		return append(out, 0xe0|byte(r>>12), 0x80|byte((r>>6)&63), 0x80|byte(r&63))
	}
	return utf8.AppendRune(out, r)
}
func appendText(out []byte, text string) []byte {
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
			if r < 32 || r > 126 {
				if r > 0xffff {
					high, low := utf16.EncodeRune(r)
					out = appendHex(out, high)
					out = appendHex(out, low)
				} else {
					out = appendHex(out, r)
				}
			} else {
				out = append(out, byte(r))
			}
		}
	}
	return append(out, '"')
}

type reader struct {
	data string
	pos  int
}

func Decode(data string) (Value, error) {
	r := reader{data: data}
	v, err := r.value(0)
	r.space()
	if err == nil && r.pos != len(data) {
		err = ErrSyntax
	}
	return v, err
}
func (r *reader) space() {
	for r.pos < len(r.data) && strings.ContainsRune(" \t\r\n", rune(r.data[r.pos])) {
		r.pos++
	}
}
func (r *reader) take(c byte) bool {
	r.space()
	if r.pos < len(r.data) && r.data[r.pos] == c {
		r.pos++
		return true
	}
	return false
}
func (r *reader) value(depth int) (Value, error) {
	r.space()
	if r.pos == len(r.data) {
		return Value{}, ErrSyntax
	}
	if depth > containerLimit {
		return Value{}, ErrDepth
	}
	c := r.data[r.pos]
	if c == '"' {
		text, err := r.string()
		return Value{kind: Text, text: text}, err
	}
	if c == '[' || c == '{' {
		object := c == '{'
		r.pos++
		v := Value{kind: Array, items: []Value{}}
		end := byte(']')
		if object {
			v = Value{kind: Object}
			end = '}'
		}
		if r.take(end) {
			return v, nil
		}
		positions := map[string]int{}
		for {
			key := ""
			if object {
				r.space()
				var err error
				key, err = r.string()
				if err != nil {
					return v, err
				}
				if !r.take(':') {
					return v, ErrSyntax
				}
			}
			item, err := r.value(depth + 1)
			if err != nil {
				return v, err
			}
			if object {
				if i, ok := positions[key]; ok {
					v.members[i].value = item
				} else {
					positions[key] = len(v.members)
					v.members = append(v.members, member{key, item})
				}
			} else {
				v.items = append(v.items, item)
			}
			if r.take(end) {
				return v, nil
			}
			if !r.take(',') {
				return v, ErrSyntax
			}
		}
	}
	for _, literal := range []string{"null", "true", "false", "NaN", "Infinity", "-Infinity"} {
		if strings.HasPrefix(r.data[r.pos:], literal) {
			r.pos += len(literal)
			switch literal {
			case "null":
				return Value{}, nil
			case "true", "false":
				return Value{kind: Boolean, boolean: literal == "true"}, nil
			case "NaN":
				return Value{kind: Float, number: math.NaN()}, nil
			case "Infinity":
				return Value{kind: Float, number: math.Inf(1)}, nil
			default:
				return Value{kind: Float, number: math.Inf(-1)}, nil
			}
		}
	}
	start := r.pos
	if r.pos < len(r.data) && r.data[r.pos] == '-' {
		r.pos++
	}
	digits := r.pos
	if r.pos < len(r.data) && r.data[r.pos] == '0' {
		r.pos++
	} else {
		for r.pos < len(r.data) && r.data[r.pos] >= '0' && r.data[r.pos] <= '9' {
			r.pos++
		}
	}
	if r.pos == digits {
		return Value{}, ErrSyntax
	}
	integer := true
	if r.pos < len(r.data) && r.data[r.pos] == '.' {
		integer = false
		r.pos++
		digits = r.pos
		for r.pos < len(r.data) && r.data[r.pos] >= '0' && r.data[r.pos] <= '9' {
			r.pos++
		}
		if r.pos == digits {
			return Value{}, ErrSyntax
		}
	}
	if r.pos < len(r.data) && (r.data[r.pos] == 'e' || r.data[r.pos] == 'E') {
		integer = false
		r.pos++
		if r.pos < len(r.data) && (r.data[r.pos] == '+' || r.data[r.pos] == '-') {
			r.pos++
		}
		digits = r.pos
		for r.pos < len(r.data) && r.data[r.pos] >= '0' && r.data[r.pos] <= '9' {
			r.pos++
		}
		if r.pos == digits {
			return Value{}, ErrSyntax
		}
	}
	raw := r.data[start:r.pos]
	if integer {
		if len(strings.TrimPrefix(raw, "-")) > integerDigitLimit {
			return Value{}, ErrInteger
		}
		if raw == "-0" {
			raw = "0"
		}
		return Value{kind: Integer, text: raw}, nil
	}
	number, err := strconv.ParseFloat(raw, 64)
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		return Value{}, ErrSyntax
	}
	return Value{kind: Float, number: number}, nil
}
func (r *reader) hex() (rune, error) {
	if r.pos+4 > len(r.data) {
		return 0, ErrSyntax
	}
	n, err := strconv.ParseUint(r.data[r.pos:r.pos+4], 16, 16)
	r.pos += 4
	if err != nil {
		return 0, ErrSyntax
	}
	return rune(n), nil
}
func (r *reader) string() (string, error) {
	if r.pos == len(r.data) || r.data[r.pos] != '"' {
		return "", ErrSyntax
	}
	r.pos++
	out := []byte{}
	for r.pos < len(r.data) {
		c := r.data[r.pos]
		r.pos++
		if c == '"' {
			return string(out), nil
		}
		if c != '\\' {
			if c < 32 {
				return "", ErrSyntax
			}
			out = append(out, c)
			continue
		}
		if r.pos == len(r.data) {
			return "", ErrSyntax
		}
		c = r.data[r.pos]
		r.pos++
		switch c {
		case '"', '\\', '/':
			out = append(out, c)
		case 'b':
			out = append(out, '\b')
		case 'f':
			out = append(out, '\f')
		case 'n':
			out = append(out, '\n')
		case 'r':
			out = append(out, '\r')
		case 't':
			out = append(out, '\t')
		case 'u':
			high, err := r.hex()
			if err != nil {
				return "", err
			}
			if high >= 0xd800 && high <= 0xdbff && r.pos+6 <= len(r.data) && r.data[r.pos:r.pos+2] == "\\u" {
				saved := r.pos
				r.pos += 2
				low, err := r.hex()
				if err != nil {
					return "", err
				}
				if low >= 0xdc00 && low <= 0xdfff {
					high = utf16.DecodeRune(high, low)
				} else {
					r.pos = saved
				}
			}
			out = appendRune(out, high)
		default:
			return "", ErrSyntax
		}
	}
	return "", ErrSyntax
}

// AppendLegacy is the Python JSONL projection: nonfinite numbers and escaped
// lone surrogates are allowed. Standard MarshalJSON stays strict.
func AppendLegacy(out []byte, v Value) ([]byte, error) { return appendValue(out, v, true) }
