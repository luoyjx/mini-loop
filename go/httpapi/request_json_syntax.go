package httpapi

import (
	"bytes"
	"regexp"
	"unicode/utf8"
)

type requestJSONFailureKind uint8

const (
	jsonExpectedValue requestJSONFailureKind = iota
	jsonExpectedProperty
	jsonExpectedColon
	jsonExpectedComma
	jsonExtraData
	jsonUnterminatedString
	jsonInvalidEscape
	jsonInvalidUnicodeEscape
	jsonInvalidControl
	jsonNestingLimit
)

type requestJSONFailure struct {
	kind     requestJSONFailureKind
	position int
}

func (e requestJSONFailure) message() string {
	switch e.kind {
	case jsonExpectedValue:
		return "Expecting value"
	case jsonExpectedProperty:
		return "Expecting property name enclosed in double quotes"
	case jsonExpectedColon:
		return "Expecting ':' delimiter"
	case jsonExpectedComma:
		return "Expecting ',' delimiter"
	case jsonExtraData:
		return "Extra data"
	case jsonUnterminatedString:
		return "Unterminated string starting at"
	case jsonInvalidEscape:
		return `Invalid \escape`
	case jsonInvalidUnicodeEscape:
		return `Invalid \uXXXX escape`
	case jsonInvalidControl:
		return "Invalid control character at"
	default:
		return "JSON nesting limit exceeded"
	}
}

// Syntax diagnostics follow CPython JSON's prefix grammar and code-point offsets.
// Successful values still enter the existing closed typed decoder, never a map
// of arbitrary host values. Depth is the same explicit native diagnostic bound.
type requestJSONScanner struct {
	data     []byte
	position int
}

var requestJSONNumber = regexp.MustCompile(`^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?`)

func requestJSONSyntax(raw []byte) *requestJSONFailure {
	s := requestJSONScanner{data: raw}
	if failure := s.value(0); failure != nil {
		return failure
	}
	s.space()
	if s.position < len(raw) {
		return s.fail(jsonExtraData, s.position)
	}
	return nil
}
func (s *requestJSONScanner) fail(kind requestJSONFailureKind, position int) *requestJSONFailure {
	return &requestJSONFailure{kind, utf8.RuneCount(s.data[:position])}
}
func (s *requestJSONScanner) space() {
	for s.position < len(s.data) {
		switch s.data[s.position] {
		case ' ', '\t', '\n', '\r':
			s.position++
		default:
			return
		}
	}
}
func (s *requestJSONScanner) value(depth int) *requestJSONFailure {
	s.space()
	if depth > 256 {
		return s.fail(jsonNestingLimit, s.position)
	}
	if s.position == len(s.data) {
		return s.fail(jsonExpectedValue, s.position)
	}
	switch s.data[s.position] {
	case '"':
		return s.quoted()
	case '{', '[':
		return s.container(depth)
	}
	for _, literal := range []string{"null", "true", "false", "NaN", "Infinity", "-Infinity"} {
		if bytes.HasPrefix(s.data[s.position:], []byte(literal)) {
			s.position += len(literal)
			return nil
		}
	}
	if match := requestJSONNumber.FindIndex(s.data[s.position:]); match != nil {
		s.position += match[1]
		return nil
	}
	return s.fail(jsonExpectedValue, s.position)
}
func (s *requestJSONScanner) quoted() *requestJSONFailure {
	start := s.position
	s.position++
	for s.position < len(s.data) {
		c := s.data[s.position]
		if c == '"' {
			s.position++
			return nil
		}
		if c < 32 {
			return s.fail(jsonInvalidControl, s.position)
		}
		if c != '\\' {
			s.position++
			continue
		}
		slash := s.position
		s.position++
		if s.position == len(s.data) {
			return s.fail(jsonUnterminatedString, start)
		}
		switch s.data[s.position] {
		case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
			s.position++
		case 'u':
			index := s.position
			for n := 1; n <= 4; n++ {
				if index+n >= len(s.data) {
					return s.fail(jsonInvalidUnicodeEscape, index)
				}
				c := s.data[index+n]
				if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
					return s.fail(jsonInvalidUnicodeEscape, index)
				}
			}
			s.position += 5
		default:
			return s.fail(jsonInvalidEscape, slash)
		}
	}
	return s.fail(jsonUnterminatedString, start)
}
func (s *requestJSONScanner) container(depth int) *requestJSONFailure {
	object := s.data[s.position] == '{'
	end := byte(']')
	if object {
		end = '}'
	}
	s.position++
	s.space()
	if s.position < len(s.data) && s.data[s.position] == end {
		s.position++
		return nil
	}
	for {
		if object {
			s.space()
			if s.position == len(s.data) || s.data[s.position] != '"' {
				return s.fail(jsonExpectedProperty, s.position)
			}
			if failure := s.quoted(); failure != nil {
				return failure
			}
			s.space()
			if s.position == len(s.data) || s.data[s.position] != ':' {
				return s.fail(jsonExpectedColon, s.position)
			}
			s.position++
		}
		if failure := s.value(depth + 1); failure != nil {
			return failure
		}
		s.space()
		if s.position < len(s.data) && s.data[s.position] == end {
			s.position++
			return nil
		}
		if s.position == len(s.data) || s.data[s.position] != ',' {
			return s.fail(jsonExpectedComma, s.position)
		}
		s.position++
	}
}
