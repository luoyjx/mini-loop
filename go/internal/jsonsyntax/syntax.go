package jsonsyntax

import (
	"bytes"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"regexp"
)

type Code uint8

const (
	ExpectedValue Code = iota
	ExpectedProperty
	ExpectedColon
	ExpectedComma
	ExtraData
	UnterminatedString
	InvalidEscape
	InvalidUnicodeEscape
	InvalidControl
	NestingLimit
	IntegerLimit
)

type Failure struct {
	kind     Code
	position int
}

func (code Code) Message() string {
	switch code {
	case ExpectedValue:
		return "Expecting value"
	case ExpectedProperty:
		return "Expecting property name enclosed in double quotes"
	case ExpectedColon:
		return "Expecting ':' delimiter"
	case ExpectedComma:
		return "Expecting ',' delimiter"
	case ExtraData:
		return "Extra data"
	case UnterminatedString:
		return "Unterminated string starting at"
	case InvalidEscape:
		return `Invalid \escape`
	case InvalidUnicodeEscape:
		return `Invalid \uXXXX escape`
	case InvalidControl:
		return "Invalid control character at"
	default:
		return "JSON nesting limit exceeded"
	}
}

// Syntax diagnostics follow CPython JSON's prefix grammar and code-point offsets.
// Successful values still enter the existing closed typed decoder, never a map
// of arbitrary host values. Container and integer limits belong to the caller profile.
type scanner struct {
	data     []byte
	position int
	limits   Limits
}

var number = regexp.MustCompile(`^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?`)

func Scan(raw []byte, limits Limits) *Failure {
	s := scanner{data: raw, limits: limits}
	if failure := s.value(0); failure != nil {
		return failure
	}
	s.space()
	if s.position < len(raw) {
		return s.fail(ExtraData, s.position)
	}
	return nil
}
func (s *scanner) fail(kind Code, position int) *Failure {
	return &Failure{kind, jsonvalue.RuneCount(string(s.data[:position]))}
}
func (s *scanner) space() {
	for s.position < len(s.data) {
		switch s.data[s.position] {
		case ' ', '\t', '\n', '\r':
			s.position++
		default:
			return
		}
	}
}
func (s *scanner) value(depth int) *Failure {
	s.space()
	if s.position == len(s.data) {
		return s.fail(ExpectedValue, s.position)
	}
	switch s.data[s.position] {
	case '"':
		return s.quoted()
	case '{', '[':
		if depth >= s.limits.Containers {
			return s.fail(NestingLimit, s.position)
		}
		return s.container(depth)
	}
	for _, literal := range []string{"null", "true", "false", "NaN", "Infinity", "-Infinity"} {
		if bytes.HasPrefix(s.data[s.position:], []byte(literal)) {
			s.position += len(literal)
			return nil
		}
	}
	if match := number.FindIndex(s.data[s.position:]); match != nil {
		if integerTooLong(s.data[s.position:s.position+match[1]], s.limits.IntegerDigits) {
			return s.fail(IntegerLimit, s.position)
		}
		s.position += match[1]
		return nil
	}
	return s.fail(ExpectedValue, s.position)
}
func (s *scanner) quoted() *Failure {
	start := s.position
	s.position++
	for s.position < len(s.data) {
		c := s.data[s.position]
		if c == '"' {
			s.position++
			return nil
		}
		if c < 32 {
			return s.fail(InvalidControl, s.position)
		}
		if c != '\\' {
			s.position++
			continue
		}
		slash := s.position
		s.position++
		if s.position == len(s.data) {
			return s.fail(UnterminatedString, start)
		}
		switch s.data[s.position] {
		case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
			s.position++
		case 'u':
			index := s.position
			for n := 1; n <= 4; n++ {
				if index+n >= len(s.data) {
					return s.fail(InvalidUnicodeEscape, index)
				}
				c := s.data[index+n]
				if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
					return s.fail(InvalidUnicodeEscape, index)
				}
			}
			s.position += 5
		default:
			return s.fail(InvalidEscape, slash)
		}
	}
	return s.fail(UnterminatedString, start)
}
func (s *scanner) container(depth int) *Failure {
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
				return s.fail(ExpectedProperty, s.position)
			}
			if failure := s.quoted(); failure != nil {
				return failure
			}
			s.space()
			if s.position == len(s.data) || s.data[s.position] != ':' {
				return s.fail(ExpectedColon, s.position)
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
			return s.fail(ExpectedComma, s.position)
		}
		s.position++
	}
}

// Limits are selected by the caller's wire profile, not by untrusted JSON.
type Limits struct{ Containers, IntegerDigits int }

func (e Failure) Code() Code    { return e.kind }
func (e Failure) Position() int { return e.position }
func integerTooLong(raw []byte, limit int) bool {
	if bytes.ContainsAny(raw, ".eE") {
		return false
	}
	if len(raw) > 0 && raw[0] == '-' {
		raw = raw[1:]
	}
	return len(raw) > limit
}

func (e Failure) Message() string { return e.kind.Message() }

// NumberLength returns the CPython number prefix length; zero means no match.
func NumberLength(raw []byte) int {
	if match := number.FindIndex(raw); match != nil {
		return match[1]
	}
	return 0
}
