// Package shell executes foreground workspace commands. Host execution is the
// default; a workspace working directory does not confine a shell.
package shell

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/luoyjx/mini-loop/go/internal/pytext"
)

const CaptureLimit = 5_000_000
const OutputCap = 50_000

// Result contains masked full captured streams. Projection is optional because
// a secret split across pipes can require replacing both streams together.
type Result struct {
	Stdout       string
	Stderr       string
	ExitCode     *int
	TimedOut     bool
	Overflowed   bool
	DurationMS   int64
	Error        *string
	Projection   *string
	CaptureLimit int
}

// Metadata does not retain output a second time in observers/events.
type Metadata struct {
	ExitCode   *int
	TimedOut   bool
	Overflowed bool
	DurationMS int64
}

func (metadata Metadata) Clone() Metadata {
	if metadata.ExitCode != nil {
		code := *metadata.ExitCode
		metadata.ExitCode = &code
	}
	return metadata
}
func (result Result) Metadata() Metadata {
	return (Metadata{result.ExitCode, result.TimedOut, result.Overflowed, result.DurationMS}).Clone()
}
func (result Result) Failed() bool {
	return result.Error != nil && *result.Error != "" || result.TimedOut || result.ExitCode != nil && *result.ExitCode != 0
}
func (result Result) Render() string {
	out := result.Stdout + result.Stderr
	if result.Projection != nil {
		out = *result.Projection
	}
	out = pytext.Strip(out)
	rendered := ""
	if out != "" {
		rendered = capOutput(out)
	}
	if result.Overflowed && rendered != "" {
		rendered += fmt.Sprintf("\n[output exceeded %s bytes; capture stopped and the command was ended]", comma(result.CaptureLimit))
	}
	appendNote := func(note string) string {
		if rendered == "" {
			return note
		}
		return rendered + "\n" + note
	}
	if result.Error != nil {
		return appendNote(*result.Error)
	}
	if result.ExitCode != nil && *result.ExitCode != 0 && !result.Overflowed {
		return appendNote(fmt.Sprintf("(exit %d)", *result.ExitCode))
	}
	if rendered == "" {
		return "(no output)"
	}
	return rendered
}
func comma(value int) string {
	text := fmt.Sprint(value)
	for i := len(text) - 3; i > 0; i -= 3 {
		text = text[:i] + "," + text[i:]
	}
	return text
}
func capOutput(text string) string {
	count := utf8.RuneCountInString(text)
	if count <= OutputCap {
		return text
	}
	note := fmt.Sprintf("\n[truncated: %s characters capped at %s]", comma(count), comma(OutputCap))
	head := OutputCap / 2
	tail := OutputCap - head - utf8.RuneCountInString(note) - 64
	runes := []rune(text)
	return string(runes[:head]) + fmt.Sprintf("\n[... %s characters omitted from the middle ...]\n", comma(count-head-tail)) + string(runes[count-tail:]) + note
}

// LooksDangerous preserves Python's case-sensitive, normalized typo blocklist.
// It is not shell confinement or an authorization boundary.
func LooksDangerous(command string) bool {
	normalized := strings.Join(strings.FieldsFunc(command, pytext.IsSpace), " ")
	for _, pattern := range [...]string{"rm -rf /", "sudo", "shutdown", "reboot", "> /dev/", ":(){", "mkfs", "dd if="} {
		if strings.Contains(normalized, pattern) {
			return true
		}
	}
	return false
}
