package jsonvalue

import (
	"errors"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

var errPartialEOF = errors.New("incomplete partial JSON scalar")

// DecodePartial implements the SDK's jiter partial_mode=True boundary. Completed
// members survive EOF; incomplete scalar members are omitted. It deliberately
// does not change Decode's strict archival/full-document contract. The caller
// owns the wire-size bound. No partial text or raw JSON enters a domain record.
func DecodePartial(data string) (Value, error) {
	r := partialReader{reader: reader{data: data}}
	return r.partialValue(0)
}

type partialReader struct{ reader }

func (r *partialReader) partialValue(depth int) (Value, error) {
	r.space()
	if r.pos == len(r.data) {
		return Value{}, errPartialEOF
	}
	if depth > 200 {
		return Value{}, ErrDepth
	}
	switch r.data[r.pos] {
	case '{', '[':
		return r.partialContainer(depth)
	case '"':
		text, err := r.partialString()
		return Value{kind: Text, text: text}, err
	}
	remaining := r.data[r.pos:]
	for _, literal := range []string{"null", "true", "false", "NaN", "Infinity", "-Infinity"} {
		if len(remaining) < len(literal) && strings.HasPrefix(literal, remaining) {
			return Value{}, errPartialEOF
		}
	}
	start := r.pos
	v, err := r.reader.value(depth)
	if err != nil {
		if errors.Is(err, ErrSyntax) && r.pos == len(r.data) {
			return Value{}, errPartialEOF
		}
		return Value{}, err
	}
	// jiter rejects leading zeroes even when trailing nonnumeric data is ignored.
	if r.data[start] == '-' {
		start++
	}
	if start < len(r.data) && r.data[start] == '0' && r.pos < len(r.data) && r.data[r.pos] >= '0' && r.data[r.pos] <= '9' {
		return Value{}, ErrSyntax
	}
	return v, nil
}

func (r *partialReader) partialContainer(depth int) (Value, error) {
	object := r.data[r.pos] == '{'
	r.pos++
	v := Value{kind: Array, items: []Value{}}
	end := byte(']')
	if object {
		v = Value{kind: Object}
		end = '}'
	}
	r.space()
	if r.pos == len(r.data) {
		return v, nil
	}
	if r.take(end) {
		return v, nil
	}
	positions := map[string]int{}
	for {
		key := ""
		if object {
			var err error
			key, err = r.partialString()
			if errors.Is(err, errPartialEOF) {
				return v, nil
			}
			if err != nil {
				return Value{}, err
			}
			r.space()
			if r.pos == len(r.data) {
				return v, nil
			}
			if !r.take(':') {
				return Value{}, ErrSyntax
			}
		}
		item, err := r.partialValue(depth + 1)
		if errors.Is(err, errPartialEOF) {
			return v, nil
		}
		if err != nil {
			return Value{}, err
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
		r.space()
		if r.take(end) {
			return v, nil
		}
		// jiter's partial container stops after a completed value if its next
		// separator is absent or unexpected. Errors after an actual comma remain errors.
		if !r.take(',') {
			return v, nil
		}
		r.space()
		if r.pos == len(r.data) {
			return v, nil
		}
		if r.data[r.pos] == end {
			return Value{}, ErrSyntax
		}
	}
}

func (r *partialReader) partialHex() (rune, error) {
	// jiter waits for all four hex bytes before validating an escape.
	if len(r.data)-r.pos < 4 {
		return 0, errPartialEOF
	}
	var value rune
	for i := 0; i < 4; i++ {
		if r.pos == len(r.data) {
			return 0, errPartialEOF
		}
		c := r.data[r.pos]
		r.pos++
		var digit rune
		switch {
		case c >= '0' && c <= '9':
			digit = rune(c - '0')
		case c >= 'a' && c <= 'f':
			digit = rune(c - 'a' + 10)
		case c >= 'A' && c <= 'F':
			digit = rune(c - 'A' + 10)
		default:
			return 0, ErrSyntax
		}
		value = value*16 + digit
	}
	return value, nil
}

func (r *partialReader) partialString() (string, error) {
	if r.pos == len(r.data) {
		return "", errPartialEOF
	}
	if r.data[r.pos] != '"' {
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
			if c >= utf8.RuneSelf {
				r.pos--
				if !utf8.FullRuneInString(r.data[r.pos:]) {
					return "", errPartialEOF
				}
				decoded, size := utf8.DecodeRuneInString(r.data[r.pos:])
				if decoded == utf8.RuneError && size == 1 {
					return "", ErrSyntax
				}
				out = append(out, r.data[r.pos:r.pos+size]...)
				r.pos += size
			} else {
				out = append(out, c)
			}
			continue
		}
		if r.pos == len(r.data) {
			return "", errPartialEOF
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
			high, err := r.partialHex()
			if err != nil {
				return "", err
			}
			if high >= 0xd800 && high <= 0xdbff {
				for _, next := range []byte{'\\', 'u'} {
					if r.pos == len(r.data) {
						return "", errPartialEOF
					}
					if r.data[r.pos] != next {
						return "", ErrSyntax
					}
					r.pos++
				}
				low, err := r.partialHex()
				if err != nil {
					return "", err
				}
				if low < 0xdc00 || low > 0xdfff {
					return "", ErrSyntax
				}
				high = utf16.DecodeRune(high, low)
			} else if high >= 0xdc00 && high <= 0xdfff {
				return "", ErrSyntax
			}
			out = appendRune(out, high)
		default:
			return "", ErrSyntax
		}
	}
	return "", errPartialEOF
}
