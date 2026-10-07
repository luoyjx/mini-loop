package improvement

import (
	"errors"
	"math"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// ArchiveValue is the closed compatibility projection of historical JSONL rows.
// New proposals still enter through ProposalFields. Unknown legacy members and
// non-object rows survive reads without open Go objects or retained raw JSON.
// All fields are private; slice accessors detach their container.
type ArchiveValue struct {
	kind    ArchiveValueKind
	text    string // Strings use UTF-8 with surrogatepass for legacy lone surrogates.
	number  float64
	boolean bool
	items   []ArchiveValue
	members []archiveMember
}
type ArchiveValueKind uint8

const (
	ArchiveNull ArchiveValueKind = iota
	ArchiveText
	ArchiveInteger
	ArchiveFloat
	ArchiveBoolean
	ArchiveArray
	ArchiveObject
)

type archiveMember struct {
	name  string
	value ArchiveValue
}

func (v ArchiveValue) Kind() ArchiveValueKind  { return v.kind }
func (v ArchiveValue) Text() (string, bool)    { return v.text, v.kind == ArchiveText }
func (v ArchiveValue) Integer() (string, bool) { return v.text, v.kind == ArchiveInteger }
func (v ArchiveValue) Float() (float64, bool)  { return v.number, v.kind == ArchiveFloat }
func (v ArchiveValue) Bool() (bool, bool)      { return v.boolean, v.kind == ArchiveBoolean }
func (v ArchiveValue) Array() ([]ArchiveValue, bool) {
	if v.kind != ArchiveArray {
		return nil, false
	}
	return append([]ArchiveValue{}, v.items...), true
}
func (v ArchiveValue) Keys() []string {
	if v.kind != ArchiveObject {
		return nil
	}
	keys := make([]string, len(v.members))
	for i, m := range v.members {
		keys[i] = m.name
	}
	return keys
}
func (v ArchiveValue) Lookup(key string) (ArchiveValue, bool) {
	for _, m := range v.members {
		if m.name == key {
			return m.value, true
		}
	}
	return ArchiveValue{}, false
}

// The source integer limit is fixed by the pinned CPython runtime. Its recursion
// threshold depends on Python call-stack depth; the Go compatibility profile
// accepts 1000 nested containers. Tests pin 500/1005, not an exact stack boundary.
const archiveIntegerDigitLimit = 4300
const archiveContainerLimit = 1000

var errArchiveSyntax = errors.New("invalid archive JSON")
var ErrArchiveInteger = errors.New("archive integer exceeds the source 4300-digit limit")
var ErrArchiveDepth = errors.New("archive JSON exceeds the nesting limit")
var ErrArchiveNonfinite = errors.New("archive value cannot be serialized as finite JSON")

// MarshalJSON preserves legacy data and source member order. Nonfinite floats
// remain observable through Float but cannot enter a standard HTTP JSON response.
func (v ArchiveValue) MarshalJSON() ([]byte, error) { return appendArchiveValue(nil, v, false) }
func appendArchiveValue(out []byte, v ArchiveValue, allowNonfinite bool) ([]byte, error) {
	switch v.kind {
	case ArchiveNull:
		return append(out, "null"...), nil
	case ArchiveText:
		return appendArchiveText(out, v.text), nil
	case ArchiveInteger:
		return append(out, v.text...), nil
	case ArchiveFloat:
		if math.IsNaN(v.number) || math.IsInf(v.number, 0) {
			if !allowNonfinite {
				return nil, ErrArchiveNonfinite
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
	case ArchiveBoolean:
		return strconv.AppendBool(out, v.boolean), nil
	case ArchiveArray:
		out = append(out, '[')
		for i, item := range v.items {
			if i > 0 {
				out = append(out, ',')
			}
			var err error
			out, err = appendArchiveValue(out, item, allowNonfinite)
			if err != nil {
				return nil, err
			}
		}
		return append(out, ']'), nil
	case ArchiveObject:
		out = append(out, '{')
		for i, m := range v.members {
			if i > 0 {
				out = append(out, ',')
			}
			out = appendArchiveText(out, m.name)
			out = append(out, ':')
			var err error
			out, err = appendArchiveValue(out, m.value, allowNonfinite)
			if err != nil {
				return nil, err
			}
		}
		return append(out, '}'), nil
	}
	return nil, errArchiveSyntax
}
func appendArchiveHex(out []byte, r rune) []byte {
	const hex = "0123456789abcdef"
	return append(out, '\\', 'u', hex[(r>>12)&15], hex[(r>>8)&15], hex[(r>>4)&15], hex[r&15])
}
func archiveRune(raw string) (rune, int) {
	if len(raw) >= 3 && raw[0] == 0xed && raw[1] >= 0xa0 && raw[1] <= 0xbf && raw[2]&0xc0 == 0x80 {
		return rune(raw[0]&15)<<12 | rune(raw[1]&63)<<6 | rune(raw[2]&63), 3
	}
	return utf8.DecodeRuneInString(raw)
}
func appendArchiveRune(out []byte, r rune) []byte {
	if r >= 0xd800 && r <= 0xdfff {
		return append(out, 0xe0|byte(r>>12), 0x80|byte((r>>6)&63), 0x80|byte(r&63))
	}
	return utf8.AppendRune(out, r)
}
func appendArchiveText(out []byte, text string) []byte {
	out = append(out, '"')
	for len(text) > 0 {
		r, n := archiveRune(text)
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
					out = appendArchiveHex(out, high)
					out = appendArchiveHex(out, low)
				} else {
					out = appendArchiveHex(out, r)
				}
			} else {
				out = append(out, byte(r))
			}
		}
	}
	return append(out, '"')
}

type archiveValueReader struct {
	data string
	pos  int
}

func decodeArchiveValue(data string) (ArchiveValue, error) {
	r := archiveValueReader{data: data}
	v, err := r.value(0)
	r.space()
	if err == nil && r.pos != len(data) {
		err = errArchiveSyntax
	}
	return v, err
}
func (r *archiveValueReader) space() {
	for r.pos < len(r.data) && strings.ContainsRune(" \t\r\n", rune(r.data[r.pos])) {
		r.pos++
	}
}
func (r *archiveValueReader) take(c byte) bool {
	r.space()
	if r.pos < len(r.data) && r.data[r.pos] == c {
		r.pos++
		return true
	}
	return false
}
func (r *archiveValueReader) value(depth int) (ArchiveValue, error) {
	r.space()
	if r.pos == len(r.data) {
		return ArchiveValue{}, errArchiveSyntax
	}
	if depth > archiveContainerLimit {
		return ArchiveValue{}, ErrArchiveDepth
	}
	c := r.data[r.pos]
	if c == '"' {
		text, err := r.string()
		return ArchiveValue{kind: ArchiveText, text: text}, err
	}
	if c == '[' || c == '{' {
		object := c == '{'
		r.pos++
		v := ArchiveValue{kind: ArchiveArray, items: []ArchiveValue{}}
		end := byte(']')
		if object {
			v = ArchiveValue{kind: ArchiveObject}
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
					return v, errArchiveSyntax
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
					v.members = append(v.members, archiveMember{key, item})
				}
			} else {
				v.items = append(v.items, item)
			}
			if r.take(end) {
				return v, nil
			}
			if !r.take(',') {
				return v, errArchiveSyntax
			}
		}
	}
	for _, literal := range []string{"null", "true", "false", "NaN", "Infinity", "-Infinity"} {
		if strings.HasPrefix(r.data[r.pos:], literal) {
			r.pos += len(literal)
			switch literal {
			case "null":
				return ArchiveValue{}, nil
			case "true", "false":
				return ArchiveValue{kind: ArchiveBoolean, boolean: literal == "true"}, nil
			case "NaN":
				return ArchiveValue{kind: ArchiveFloat, number: math.NaN()}, nil
			case "Infinity":
				return ArchiveValue{kind: ArchiveFloat, number: math.Inf(1)}, nil
			default:
				return ArchiveValue{kind: ArchiveFloat, number: math.Inf(-1)}, nil
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
		return ArchiveValue{}, errArchiveSyntax
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
			return ArchiveValue{}, errArchiveSyntax
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
			return ArchiveValue{}, errArchiveSyntax
		}
	}
	raw := r.data[start:r.pos]
	if integer {
		if len(strings.TrimPrefix(raw, "-")) > archiveIntegerDigitLimit {
			return ArchiveValue{}, ErrArchiveInteger
		}
		if raw == "-0" {
			raw = "0"
		}
		return ArchiveValue{kind: ArchiveInteger, text: raw}, nil
	}
	number, err := strconv.ParseFloat(raw, 64)
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		return ArchiveValue{}, errArchiveSyntax
	}
	return ArchiveValue{kind: ArchiveFloat, number: number}, nil
}
func (r *archiveValueReader) hex() (rune, error) {
	if r.pos+4 > len(r.data) {
		return 0, errArchiveSyntax
	}
	n, err := strconv.ParseUint(r.data[r.pos:r.pos+4], 16, 16)
	r.pos += 4
	if err != nil {
		return 0, errArchiveSyntax
	}
	return rune(n), nil
}
func (r *archiveValueReader) string() (string, error) {
	if r.pos == len(r.data) || r.data[r.pos] != '"' {
		return "", errArchiveSyntax
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
				return "", errArchiveSyntax
			}
			out = append(out, c)
			continue
		}
		if r.pos == len(r.data) {
			return "", errArchiveSyntax
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
			out = appendArchiveRune(out, high)
		default:
			return "", errArchiveSyntax
		}
	}
	return "", errArchiveSyntax
}
