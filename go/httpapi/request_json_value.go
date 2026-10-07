package httpapi

import (
	"bytes"
	"encoding/json"
	"strconv"
	"unicode/utf16"
	"unicode/utf8"
)

// requestRune accepts scalar UTF-8 and the three-byte surrogatepass spelling.
// Non-scalars remain boundary data; they never enter validated service strings.
func requestRune(raw []byte) (rune, int, bool) {
	value, size := utf8.DecodeRune(raw)
	if value != utf8.RuneError || size != 1 {
		return value, size, size > 0
	}
	if len(raw) >= 3 && raw[0] == 0xed && raw[1] >= 0xa0 && raw[1] <= 0xbf && raw[2] >= 0x80 && raw[2] <= 0xbf {
		return rune(raw[0]&15)<<12 | rune(raw[1]&63)<<6 | rune(raw[2]&63), 3, true
	}
	return 0, 0, false
}
func appendRequestRune(out []byte, value rune) []byte {
	if value >= 0xd800 && value <= 0xdfff {
		return append(out, byte(0xe0|(value>>12)), byte(0x80|((value>>6)&63)), byte(0x80|(value&63)))
	}
	return utf8.AppendRune(out, value)
}
func requestRuneCount(raw []byte) int {
	count := 0
	for len(raw) > 0 {
		_, size, ok := requestRune(raw)
		if !ok {
			size = 1
		}
		raw = raw[size:]
		count++
	}
	return count
}

// Lower only after the syntax pass. Duplicate values are overwritten before
// Unicode screening, so discarded malformed strings cannot poison a request.
type requestJSONValueReader struct{ requestJSONScanner }

func readRequestJSONValue(raw []byte) (ValidationInput, error) {
	r := requestJSONValueReader{requestJSONScanner{data: raw}}
	v, err := r.value(0)
	if err != nil {
		return ValidationInput{}, err
	}
	r.space()
	if r.position != len(raw) {
		return ValidationInput{}, errPersonalSkillRequest
	}
	return v, nil
}
func (r *requestJSONValueReader) value(depth int) (ValidationInput, error) {
	r.space()
	if depth > 256 || r.position == len(r.data) {
		return ValidationInput{}, errPersonalSkillRequest
	}
	switch r.data[r.position] {
	case '"':
		text, err := r.text()
		return ValidationInput{kind: validationText, text: text}, err
	case '{', '[':
		object := r.data[r.position] == '{'
		end := byte(']')
		kind := validationArray
		if object {
			end = '}'
			kind = validationObject
		}
		r.position++
		r.space()
		v := ValidationInput{kind: kind}
		if !object {
			v.items = []ValidationInput{}
		}
		positions := make(map[string]int)
		if r.position < len(r.data) && r.data[r.position] == end {
			r.position++
			return v, nil
		}
		for {
			key := ""
			if object {
				r.space()
				var err error
				key, err = r.text()
				if err != nil {
					return v, err
				}
				r.space()
				if r.position == len(r.data) || r.data[r.position] != ':' {
					return v, errPersonalSkillRequest
				}
				r.position++
			}
			item, err := r.value(depth + 1)
			if err != nil {
				return v, err
			}
			if object {
				if index, exists := positions[key]; exists {
					v.members[index].value = item
				} else {
					positions[key] = len(v.members)
					v.members = append(v.members, validationMember{key, item})
				}
			} else {
				v.items = append(v.items, item)
			}
			r.space()
			if r.position == len(r.data) {
				return v, errPersonalSkillRequest
			}
			if r.data[r.position] == end {
				r.position++
				return v, nil
			}
			if r.data[r.position] != ',' {
				return v, errPersonalSkillRequest
			}
			r.position++
		}
	}
	for _, literal := range []string{"null", "true", "false"} {
		if bytes.HasPrefix(r.data[r.position:], []byte(literal)) {
			r.position += len(literal)
			if literal == "null" {
				return ValidationInput{}, nil
			}
			return ValidationInput{kind: validationBool, boolean: literal == "true"}, nil
		}
	}
	if match := requestJSONNumber.FindIndex(r.data[r.position:]); match != nil {
		number := json.Number(string(r.data[r.position : r.position+match[1]]))
		r.position += match[1]
		return ValidationInput{kind: validationNumber, number: number}, nil
	}
	return ValidationInput{}, errPersonalSkillRequest
}
func (r *requestJSONValueReader) hexRune() (rune, error) {
	if r.position+4 > len(r.data) {
		return 0, errPersonalSkillRequest
	}
	value, err := strconv.ParseUint(string(r.data[r.position:r.position+4]), 16, 16)
	if err != nil {
		return 0, errPersonalSkillRequest
	}
	r.position += 4
	return rune(value), nil
}
func (r *requestJSONValueReader) text() (string, error) {
	if r.position == len(r.data) || r.data[r.position] != '"' {
		return "", errPersonalSkillRequest
	}
	r.position++
	out := []byte{}
	for r.position < len(r.data) {
		c := r.data[r.position]
		r.position++
		if c == '"' {
			return string(out), nil
		}
		if c != '\\' {
			r.position--
			value, size, ok := requestRune(r.data[r.position:])
			if !ok || value < 32 {
				return "", errPersonalSkillRequest
			}
			out = appendRequestRune(out, value)
			r.position += size
			continue
		}
		if r.position == len(r.data) {
			return "", errPersonalSkillRequest
		}
		escape := r.data[r.position]
		r.position++
		switch escape {
		case '"', '\\', '/':
			out = append(out, escape)
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
			value, err := r.hexRune()
			if err != nil {
				return "", err
			}
			if value >= 0xd800 && value <= 0xdbff && r.position+6 <= len(r.data) && r.data[r.position] == '\\' && r.data[r.position+1] == 'u' {
				saved := r.position
				r.position += 2
				low, err := r.hexRune()
				if err != nil {
					return "", err
				}
				if low >= 0xdc00 && low <= 0xdfff {
					value = utf16.DecodeRune(value, low)
				} else {
					r.position = saved
				}
			}
			out = appendRequestRune(out, value)
		default:
			return "", errPersonalSkillRequest
		}
	}
	return "", errPersonalSkillRequest
}
func (v ValidationInput) hasSurrogate() bool {
	if v.kind == validationText {
		return !utf8.ValidString(v.text)
	}
	for _, item := range v.items {
		if item.hasSurrogate() {
			return true
		}
	}
	for _, member := range v.members {
		if !utf8.ValidString(member.key) || member.value.hasSurrogate() {
			return true
		}
	}
	return false
}
