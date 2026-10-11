package efficiency

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/luoyjx/mini-loop/go/internal/pytext"
)

type PassthroughObservationReducer struct{}
type DeterministicLosslessReducer struct{}

func builtinObservationDescriptor(id string, recoverable bool) Descriptor {
	c := DefaultDescriptor(id, "1", Observation)
	if recoverable {
		c.ContentTypes = []string{"text/*", "application/json"}
		c.Lossiness = Recoverable
		c.Recoverable = true
	}
	d, err := NewDescriptor(c)
	if err != nil {
		panic("invalid builtin observation descriptor: " + err.Error())
	}
	return d
}
func (PassthroughObservationReducer) Descriptor() Descriptor {
	return builtinObservationDescriptor("passthrough-observation", false)
}
func (DeterministicLosslessReducer) Descriptor() Descriptor {
	return builtinObservationDescriptor("deterministic-lossless", true)
}
func (PassthroughObservationReducer) Reduce(ctx context.Context, o MaskedObservation, _ ReductionOptions) (ObservationReduction, error) {
	if err := ctx.Err(); err != nil {
		return ObservationReduction{}, err
	}
	c, err := o.Snapshot()
	if err != nil {
		return ObservationReduction{}, err
	}
	return NewReduction(ReductionConfig{Content: c.Content})
}

var ansiEscape = regexp.MustCompile("(?:\\x1b\\][^\\x07]*(?:\\x07|\\x1b\\\\))|(?:\\x1b\\[[0-?]*[ -/]*[@-~])")

// asciiRegexFold covers Python re.IGNORECASE's ASCII ranges, including its four
// additional Unicode letters. Word/decimal predicates use pinned Python tables.
func asciiRegexFold(r rune) rune {
	if r >= 'A' && r <= 'Z' {
		return r + 32
	}
	switch r {
	case 'İ', 'ı':
		return 'i'
	case 'ſ':
		return 's'
	case 'K':
		return 'k'
	}
	return r
}
func extensionCharacter(r rune) bool {
	r = asciiRegexFold(r)
	return r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '+' || r == '-'
}
func criticalObservationLine(line string) bool {
	runes := []rune(line)
	for i, r := range runes {
		if _, ok := pytext.DecimalDigit(r); ok {
			return true
		}
		if i == 0 || pytext.Space(runes[i-1]) {
			if r == '/' || r == '\\' {
				return true
			}
			f := asciiRegexFold(r)
			if f >= 'a' && f <= 'z' && i+2 < len(runes) && runes[i+1] == ':' && (runes[i+2] == '/' || runes[i+2] == '\\') {
				return true
			}
		}
		if r == '.' {
			for end := i + 1; end < len(runes) && end <= i+12 && extensionCharacter(runes[end]); end++ {
				nextWord := end+1 < len(runes) && pytext.IsWord(runes[end+1])
				if pytext.IsWord(runes[end]) != nextWord {
					return true
				}
			}
		}
	}
	for start := 0; start < len(runes); {
		if !pytext.IsWord(runes[start]) {
			start++
			continue
		}
		end := start + 1
		for end < len(runes) && pytext.IsWord(runes[end]) {
			end++
		}
		word := make([]rune, end-start)
		for i, r := range runes[start:end] {
			word[i] = asciiRegexFold(r)
		}
		switch string(word) {
		case "error", "faile", "failed", "failure", "exception", "traceback", "panic", "fatal", "warn", "warning", "denied", "timeout", "exit":
			return true
		}
		start = end
	}
	return false
}
func (DeterministicLosslessReducer) Reduce(ctx context.Context, o MaskedObservation, _ ReductionOptions) (ObservationReduction, error) {
	if err := ctx.Err(); err != nil {
		return ObservationReduction{}, err
	}
	c, err := o.Snapshot()
	if err != nil {
		return ObservationReduction{}, err
	}
	ansiCount := 0
	withoutANSI := ansiEscape.ReplaceAllStringFunc(c.Content, func(string) string { ansiCount++; return "" })
	lines := strings.Split(withoutANSI, "\n")
	folded := make([]string, 0, len(lines))
	duplicates, blanks := 0, 0
	for index := 0; index < len(lines); {
		line := lines[index]
		end := index + 1
		if strings.Trim(line, "\r") == "" {
			for end < len(lines) && strings.Trim(lines[end], "\r") == "" {
				end++
			}
			folded = append(folded, line)
			blanks += end - index - 1
		} else {
			for end < len(lines) && lines[end] == line {
				end++
			}
			run := end - index
			if run > 1 && !criticalObservationLine(line) {
				plural := "s"
				if run == 2 {
					plural = ""
				}
				folded = append(folded, line, fmt.Sprintf("[repeated %d additional time%s]", run-1, plural))
				duplicates++
			} else {
				folded = append(folded, lines[index:end]...)
			}
		}
		index = end
	}
	warnings := []string{}
	if ansiCount > 0 {
		warnings = append(warnings, fmt.Sprintf("removed_ansi:%d", ansiCount))
	}
	if duplicates > 0 {
		warnings = append(warnings, fmt.Sprintf("folded_duplicate_runs:%d", duplicates))
	}
	if blanks > 0 {
		warnings = append(warnings, fmt.Sprintf("folded_blank_lines:%d", blanks))
	}
	return NewReduction(ReductionConfig{Content: strings.Join(folded, "\n"), Warnings: warnings})
}
