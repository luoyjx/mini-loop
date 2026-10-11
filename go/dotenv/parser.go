// Package dotenv implements pinned Source text/environment phases and explicit
// local file discovery/loading. It never mutates process environment or cwd.
package dotenv

import (
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/luoyjx/mini-loop/go/internal/pytext"
)

const SourceVersion = "1.2.2"

type Name string
type Original struct {
	Text string `json:"original"`
	Line int    `json:"line"`
}
type Binding struct {
	Key   *Name   `json:"key"`
	Value *string `json:"value"`
	Original
	Error bool `json:"error"`
}

type reader struct {
	text           string
	position, line int
}

func (r *reader) advance(end int) {
	part := r.text[r.position:end]
	for i := 0; i < len(part); i++ {
		if part[i] == '\r' {
			r.line++
			if i+1 < len(part) && part[i+1] == '\n' {
				i++
			}
		} else if part[i] == '\n' {
			r.line++
		}
	}
	r.position = end
}

func (r *reader) space(multiline bool) {
	end := r.position
	for end < len(r.text) {
		c, n := utf8.DecodeRuneInString(r.text[end:])
		if !pytext.IsSpace(c) || !multiline && (c == '\r' || c == '\n') {
			break
		}
		end += n
	}
	r.advance(end)
}

func (r *reader) key() (*Name, bool) {
	start := r.position
	if start < len(r.text) && r.text[start] == '#' {
		return nil, true
	}
	if start < len(r.text) && r.text[start] == '\'' {
		end := strings.IndexByte(r.text[start+1:], '\'')
		if end <= 0 {
			return nil, false
		}
		end += start + 1
		key := Name(r.text[start+1 : end])
		r.advance(end + 1)
		return &key, true
	}
	end := start
	for end < len(r.text) {
		c, n := utf8.DecodeRuneInString(r.text[end:])
		if c == '=' || c == '#' || pytext.IsSpace(c) {
			break
		}
		end += n
	}
	if end == start {
		return nil, false
	}
	key := Name(r.text[start:end])
	r.advance(end)
	return &key, true
}

func (r *reader) quoted(quote byte) (string, bool) {
	start := r.position
	fallback := -1
	for end := start + 1; end < len(r.text); end++ {
		if r.text[end] == '\\' && end+1 < len(r.text) && r.text[end+1] == quote {
			fallback = end + 1
			end++
			continue
		}
		if r.text[end] == quote {
			value := r.text[start+1 : end]
			r.advance(end + 1)
			return escapes(value, quote), true
		}
	}
	// Source's regex backtracks an escaped final quote into a literal backslash
	// followed by the closing quote when no later close is possible.
	if fallback >= 0 {
		value := r.text[start+1 : fallback]
		r.advance(fallback + 1)
		return escapes(value, quote), true
	}
	return "", false
}

func escapes(text string, quote byte) string {
	var out strings.Builder
	for i := 0; i < len(text); i++ {
		if text[i] == '\\' && i+1 < len(text) {
			c := text[i+1]
			replacement, ok := byte(0), false
			if c == '\\' || c == '\'' || quote == '"' && c == '"' {
				replacement, ok = c, true
			}
			if quote == '"' {
				switch c {
				case 'a':
					replacement, ok = '\a', true
				case 'b':
					replacement, ok = '\b', true
				case 'f':
					replacement, ok = '\f', true
				case 'n':
					replacement, ok = '\n', true
				case 'r':
					replacement, ok = '\r', true
				case 't':
					replacement, ok = '\t', true
				case 'v':
					replacement, ok = '\v', true
				}
			}
			if ok {
				out.WriteByte(replacement)
				i++
				continue
			}
		}
		out.WriteByte(text[i])
	}
	return out.String()
}

func (r *reader) value() (string, bool) {
	if r.position < len(r.text) && (r.text[r.position] == '\'' || r.text[r.position] == '"') {
		return r.quoted(r.text[r.position])
	}
	end := r.position
	for end < len(r.text) && r.text[end] != '\r' && r.text[end] != '\n' {
		end++
	}
	value := r.text[r.position:end]
	r.advance(end)
	for i, c := range value {
		if c == '#' && i > 0 {
			prior, _ := utf8.DecodeLastRuneInString(value[:i])
			if pytext.IsSpace(prior) {
				value = strings.TrimRightFunc(value[:i], pytext.IsSpace)
				break
			}
		}
	}
	return strings.TrimRightFunc(value, pytext.IsSpace), true
}

func (r *reader) binding() Binding {
	start, line := r.position, r.line
	finish := func(key *Name, value *string, bad bool) Binding {
		return Binding{Key: key, Value: value, Original: Original{Text: r.text[start:r.position], Line: line}, Error: bad}
	}
	fail := func() Binding {
		end := r.position
		for end < len(r.text) && r.text[end] != '\r' && r.text[end] != '\n' {
			end++
		}
		// Recovery's Source alternation takes CR before CRLF, unlike normal EOL.
		if end < len(r.text) {
			end++
		}
		r.advance(end)
		return finish(nil, nil, true)
	}
	r.space(true)
	if r.position == len(r.text) {
		return finish(nil, nil, false)
	}
	if strings.HasPrefix(r.text[r.position:], "export") {
		end := r.position + 6
		if end < len(r.text) {
			c, _ := utf8.DecodeRuneInString(r.text[end:])
			if pytext.IsSpace(c) && c != '\r' && c != '\n' {
				r.advance(end)
				r.space(false)
			}
		}
	}
	key, ok := r.key()
	if !ok {
		return fail()
	}
	r.space(false)
	var value *string
	if r.position < len(r.text) && r.text[r.position] == '=' {
		r.advance(r.position + 1)
		r.space(false)
		v, ok := r.value()
		if !ok {
			return fail()
		}
		value = &v
	}
	r.space(false)
	if r.position < len(r.text) && r.text[r.position] == '#' {
		end := r.position
		for end < len(r.text) && r.text[end] != '\r' && r.text[end] != '\n' {
			end++
		}
		r.advance(end)
	}
	r.space(false)
	if r.position < len(r.text) {
		end := r.position
		if r.text[end] == '\r' {
			end++
			if end < len(r.text) && r.text[end] == '\n' {
				end++
			}
		} else if r.text[end] == '\n' {
			end++
		} else {
			return fail()
		}
		r.advance(end)
	}
	return finish(key, value, false)
}

// Parse keeps recoverable syntax errors as bindings, including exact source
// spans and start lines. Invalid UTF-8 cannot enter the scalar text boundary.
func Parse(text string) ([]Binding, error) {
	if !utf8.ValidString(text) {
		return nil, errors.New("dotenv text must be valid UTF-8")
	}
	r := reader{text: text, line: 1}
	bindings := []Binding{}
	for r.position < len(text) {
		bindings = append(bindings, r.binding())
	}
	return bindings, nil
}
