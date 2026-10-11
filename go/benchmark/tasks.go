package benchmark

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf8"

	"github.com/luoyjx/mini-loop/go/internal/pytext"
	"github.com/luoyjx/mini-loop/go/protocol"
)

// Judge and Setup are trusted, human-admitted instrument callbacks. A model
// response or improvement draft must never become either callback.
type Judge func(ctx context.Context, workspace, final string) (bool, error)
type Setup func(ctx context.Context, workspace string) error

type TaskConfig struct {
	Name, Prompt string
	Expect       Judge
	Setup        Setup
	// Nil retains installed tools; a nonnil empty slice removes every tool.
	ToolNames *[]protocol.ToolName
}

// Task captures the workload specification. Callbacks own their closed-over
// state; names and whitelist storage are detached and immutable here.
type Task struct {
	name, prompt string
	expect       Judge
	setup        Setup
	selected     bool
	toolNames    []protocol.ToolName
}

var ErrInvalidTask = errors.New("benchmark task requires a judge")
var ErrInvalidText = errors.New("benchmark file is not valid UTF-8")

func NewTask(config TaskConfig) (Task, error) {
	if config.Expect == nil {
		return Task{}, ErrInvalidTask
	}
	task := Task{name: config.Name, prompt: config.Prompt, expect: config.Expect, setup: config.Setup}
	if config.ToolNames != nil {
		task.selected = true
		task.toolNames = append([]protocol.ToolName(nil), (*config.ToolNames)...)
	}
	return task, nil
}

func (task Task) Name() string   { return task.name }
func (task Task) Prompt() string { return task.prompt }
func (task Task) ToolNames() ([]protocol.ToolName, bool) {
	return append([]protocol.ToolName(nil), task.toolNames...), task.selected
}
func (task Task) Prepare(ctx context.Context, workspace string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if task.expect == nil {
		return ErrInvalidTask
	}
	if task.setup != nil {
		if err := task.setup(ctx, workspace); err != nil {
			return err
		}
	}
	return ctx.Err()
}
func (task Task) Judge(ctx context.Context, workspace, final string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if task.expect == nil {
		return false, ErrInvalidTask
	}
	passed, err := task.expect(ctx, workspace, final)
	if err != nil {
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return passed, nil
}

func textJudge(token string) Judge {
	return func(_ context.Context, _, final string) (bool, error) { return strings.Contains(final, token), nil }
}
func targetExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	// Python pathlib.exists ignores these errnos, including a broken symlink
	// or symlink loop. Other I/O failures reach the judge fault boundary.
	for _, missing := range []error{syscall.ENOENT, syscall.ENOTDIR, syscall.EBADF, syscall.ELOOP} {
		if errors.Is(err, missing) {
			return false, nil
		}
	}
	return false, err
}
func fileJudge(name string, inspect func(string) bool) Judge {
	return func(_ context.Context, workspace, _ string) (bool, error) {
		path := filepath.Join(workspace, name)
		exists, err := targetExists(path)
		if err != nil || !exists {
			return false, err
		}
		if inspect == nil {
			return true, nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return false, err
		}
		// Path.read_text uses strict UTF-8 in the pinned source environment,
		// unlike read_file's replacement decoder. Preserve universal newlines.
		if !utf8.Valid(data) {
			return false, ErrInvalidText
		}
		text := strings.ReplaceAll(strings.ReplaceAll(string(data), "\r\n", "\n"), "\r", "\n")
		return inspect(text), nil
	}
}
func lineCount(text string) int {
	count, trailing := 0, false
	for _, r := range text {
		switch r {
		case '\n', '\v', '\f', '\x1c', '\x1d', '\x1e', '\u0085', '\u2028', '\u2029':
			count++
			trailing = true
		default:
			trailing = false
		}
	}
	if text != "" && !trailing {
		count++
	}
	return count
}
func seedLongLog(ctx context.Context, workspace string) error {
	var text strings.Builder
	for i := 1; i <= 6000; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		fmt.Fprintf(&text, "line-%05d value token-%05d %s\n", i, i, strings.Repeat("x", 24))
	}
	return os.WriteFile(filepath.Join(workspace, "data.log"), []byte(text.String()), 0666)
}

// DefaultTasks and HeldoutTasks reproduce the source's admitted prompts and
// observable-effect judges, including its deliberately permissive substrings.
// Each call returns fresh list storage. Setup failures are not passing effects.
func DefaultTasks() []Task {
	return []Task{
		{name: "write-file", prompt: "Create a file named greeting.txt containing the word hello.", expect: fileJudge("greeting.txt", func(text string) bool { return strings.Contains(pytext.Lower(text), "hello") })},
		{name: "arithmetic", prompt: "What is 5 + 7? Answer with the number.", expect: textJudge("12")},
		{name: "edit-config", prompt: "Create config.ini with a [net] section containing `retries = 3`.", expect: fileJudge("config.ini", func(text string) bool { return strings.Contains(text, "retries = 3") })},
		{name: "page-long-log", prompt: "data.log has 6000 numbered lines and is too large to read in one go. Report the exact token-NNNNN value that appears on line 4321.", expect: textJudge("token-04321"), setup: seedLongLog},
		{name: "page-long-log-readonly", prompt: "data.log has 6000 numbered lines and is too large to read in one go. Using the available file tools, report the exact token-NNNNN value that appears on line 4321.", expect: textJudge("token-04321"), setup: seedLongLog, selected: true, toolNames: []protocol.ToolName{protocol.ToolReadFile, protocol.ToolGlob}},
	}
}
func HeldoutTasks() []Task {
	return []Task{
		{name: "append-log", prompt: "Create notes.log containing three lines: alpha, beta, gamma.", expect: fileJudge("notes.log", func(text string) bool { return lineCount(text) >= 3 })},
		{name: "word-count", prompt: "How many words are in 'the quick brown fox jumps'? Answer with the number.", expect: textJudge("5")},
		{name: "nested-file", prompt: "Create the file src/app/main.txt containing ok.", expect: fileJudge(filepath.Join("src", "app", "main.txt"), nil)},
	}
}
