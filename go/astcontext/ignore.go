package astcontext

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/luoyjx/mini-loop/go/internal/pytext"
)

// The snapshotter needs all three outcomes: a nested keep decision overrides
// an outer ignore frame, while unmatched continues searching outer frames.
type ignoreDisposition uint8

const (
	unmatched ignoreDisposition = iota
	ignored
	kept
)

type ignoreMatch struct {
	disposition ignoreDisposition
	index       int
}

type ignorePattern struct {
	decision       ignoreDisposition
	expression     *regexp.Regexp
	directoryGroup int
}

type ignoreSpec struct{ patterns []ignorePattern }

const maxIgnorePatternChars = 4096

func compileIgnore(lines []string) (ignoreSpec, error) {
	for _, raw := range lines {
		if utf8.RuneCountInString(raw) > maxIgnorePatternChars {
			return ignoreSpec{}, fmt.Errorf("ast-outline ignore patterns must be at most %d characters", maxIgnorePatternChars)
		}
		if raw == "" || strings.HasPrefix(raw, "#") {
			continue
		}
		line := strings.TrimPrefix(raw, "!")
		groups := 0
		for _, segment := range strings.Split(line, "/") {
			if segment == "**" {
				groups++
			} else {
				escaped := false
				for _, r := range segment {
					if escaped {
						escaped = false
					} else if r == '\\' {
						escaped = true
					} else if r == '*' {
						groups++
					}
				}
			}
			if groups > 2 {
				return ignoreSpec{}, errors.New("ast-outline ignore patterns allow at most two variable-star groups")
			}
		}
	}
	result := ignoreSpec{}
	for _, line := range lines {
		// PathSpec filters empty raw lines, but retains comments/null patterns;
		// their positions and count still affect matching indices and budgets.
		if line == "" {
			continue
		}
		pattern, err := compileIgnorePattern(line)
		if err != nil {
			return ignoreSpec{}, err
		}
		result.patterns = append(result.patterns, pattern)
	}
	return result, nil
}

func (spec ignoreSpec) check(path string) ignoreMatch {
	if strings.HasPrefix(path, "/") {
		path = path[1:]
	} else if strings.HasPrefix(path, "./") {
		path = path[2:]
	}
	result := ignoreMatch{index: -1}
	priority := 0
	for i := len(spec.patterns) - 1; i >= 0; i-- {
		pattern := spec.patterns[i]
		if pattern.expression == nil {
			continue
		}
		match := pattern.expression.FindStringSubmatchIndex(path)
		if match == nil {
			continue
		}
		current := 2
		if pattern.directoryGroup > 0 && match[pattern.directoryGroup*2] >= 0 {
			current = 1
		}
		if current > priority {
			result = ignoreMatch{pattern.decision, i}
			priority = current
		}
		if current == 2 {
			break
		}
	}
	return result
}

const directoryMark = `(?P<ps_d>/)`

// Python $ also accepts the position before a single final newline. Consuming
// that newline gives the same include/index decision without a lookahead.
const optionalDirectoryMark = `(?:` + directoryMark + `|\n?\z)`

func compileIgnorePattern(raw string) (ignorePattern, error) {
	line := raw
	if !strings.HasSuffix(line, `\ `) {
		line = strings.TrimRightFunc(line, pytext.IsSpace)
	}
	if line == "" || line == "/" || strings.HasPrefix(line, "#") {
		return ignorePattern{}, nil
	}
	decision := ignored
	if strings.HasPrefix(line, "!") {
		decision = kept
		line = line[1:]
	}
	segments := strings.Split(line, "/")
	isDirectory := segments[len(segments)-1] == ""
	if segments[0] == "" {
		segments = segments[1:]
	} else if len(segments) == 1 || len(segments) == 2 && segments[1] == "" {
		if segments[0] != "**" {
			segments = append([]string{"**"}, segments...)
		}
	}
	if len(segments) == 0 {
		return ignorePattern{}, fmt.Errorf("Invalid git pattern: %s", pytext.Repr(raw))
	}
	if segments[len(segments)-1] == "" {
		segments[len(segments)-1] = "**"
	}
	compact := make([]string, 0, len(segments))
	for _, segment := range segments {
		if segment == "**" && len(compact) > 0 && compact[len(compact)-1] == "**" {
			continue
		}
		compact = append(compact, segment)
	}
	segments = compact
	var expression string
	if len(segments) == 1 && segments[0] == "**" {
		if isDirectory {
			expression = directoryMark
		} else {
			expression = "."
		}
	} else if len(segments) == 2 && segments[0] == "**" && segments[1] == "*" {
		expression = "."
	} else if len(segments) == 3 && segments[0] == "**" && segments[1] == "*" && segments[2] == "**" {
		if isDirectory {
			expression = directoryMark
		} else {
			expression = "/"
		}
	} else {
		var builder strings.Builder
		needSlash := false
		for i, segment := range segments {
			if segment == "**" {
				if i == 0 {
					builder.WriteString(`^(?:.+/)?`)
				} else if i < len(segments)-1 {
					builder.WriteString(`(?:/.+)?`)
					needSlash = true
				} else if isDirectory {
					builder.WriteString(directoryMark)
				} else {
					builder.WriteByte('/')
				}
				continue
			}
			if i == 0 {
				builder.WriteByte('^')
			}
			if needSlash {
				builder.WriteByte('/')
			}
			if segment == "*" {
				builder.WriteString(`[^/]+`)
			} else {
				translated, err := translateIgnoreSegment(segment)
				if errors.Is(err, errUnclosedIgnoreClass) {
					return ignorePattern{}, nil
				}
				if errors.Is(err, errTrailingIgnoreEscape) {
					return ignorePattern{}, fmt.Errorf("Invalid git pattern: %s", pytext.Repr(raw))
				}
				if err != nil {
					return ignorePattern{}, err
				}
				builder.WriteString(translated)
			}
			if i == len(segments)-1 {
				builder.WriteString(optionalDirectoryMark)
			}
			needSlash = true
		}
		expression = builder.String()
	}
	compiled, err := regexp.Compile(expression)
	if err != nil {
		return ignorePattern{}, err
	}
	return ignorePattern{decision, compiled, compiled.SubexpIndex("ps_d")}, nil
}

var errUnclosedIgnoreClass = errors.New("unclosed ignore bracket expression")
var errTrailingIgnoreEscape = errors.New("trailing ignore escape")

func translateIgnoreSegment(segment string) (string, error) {
	runes := []rune(segment)
	var out strings.Builder
	for i := 0; i < len(runes); i++ {
		switch r := runes[i]; r {
		case '\\':
			i++
			if i == len(runes) {
				return "", errTrailingIgnoreEscape
			}
			out.WriteString(regexp.QuoteMeta(string(runes[i])))
		case '*':
			out.WriteString(`[^/]*`)
		case '?':
			out.WriteString(`[^/]`)
		case '[':
			begin := i + 1
			end := begin
			negated := end < len(runes) && (runes[end] == '!' || runes[end] == '^')
			if negated {
				end++
				begin++
			}
			if end < len(runes) && runes[end] == ']' {
				end++
			}
			for end < len(runes) && runes[end] != ']' {
				end++
			}
			if end == len(runes) {
				return "", errUnclosedIgnoreClass
			}
			out.WriteByte('[')
			if negated {
				out.WriteByte('^')
			}
			for j := begin; j < end; j++ {
				if j+2 < end && runes[j+1] == '-' {
					if runes[j] > runes[j+2] {
						return "", errors.New("invalid git ignore character range")
					}
					fmt.Fprintf(&out, `\x{%x}-\x{%x}`, runes[j], runes[j+2])
					j += 2
				} else {
					fmt.Fprintf(&out, `\x{%x}`, runes[j])
				}
			}
			out.WriteByte(']')
			i = end
		default:
			out.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	return out.String(), nil
}

// Ignore controls use UTF-8 errors=ignore and Python splitlines; the snapshot
// still copies the original control bytes unchanged for the external CLI.
func ignoreLines(data []byte) []string {
	var decoded []rune
	for len(data) > 0 {
		r, size, issue := pytext.DecodeRune(data)
		if issue == pytext.ValidUTF8 {
			decoded = append(decoded, r)
		}
		data = data[size:]
	}
	lines := []string{}
	start := 0
	for i := 0; i < len(decoded); i++ {
		r := decoded[i]
		if r == '\n' || r == '\r' || r == '\v' || r == '\f' || r >= 0x1c && r <= 0x1e || r == 0x85 || r == 0x2028 || r == 0x2029 {
			lines = append(lines, string(decoded[start:i]))
			if r == '\r' && i+1 < len(decoded) && decoded[i+1] == '\n' {
				i++
			}
			start = i + 1
		}
	}
	if start < len(decoded) {
		lines = append(lines, string(decoded[start:]))
	}
	return lines
}
