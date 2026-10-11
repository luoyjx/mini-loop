// Package workspace implements the workspace-bound file effects behind the tool gate.
package workspace

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf8"

	"github.com/luoyjx/mini-loop/go/protocol"
)

const ReadCharCap = 2_000_000
const OutputCap = 50_000

// Files keeps an immutable, resolved root. Every operation resolves its input
// again at execution; a permission check alone never authorizes a file path.
type Files struct{ root string }

type PathEscapeError struct{ Path string }

func (err *PathEscapeError) Error() string {
	return fmt.Sprintf("Path escapes workspace: %s. Paths are relative to the workspace root; an absolute path only works when it points inside the workspace.", err.Path)
}

func NewFiles(root string) (*Files, error) {
	if root == "" {
		return nil, errors.New("workspace root is required")
	}
	resolved, err := ResolvePath(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(resolved, 0o777); err != nil {
		return nil, err
	}
	return &Files{root: resolved}, nil
}

func (files *Files) Root() string { return files.root }

// ResolvePath preserves symlink/.. order and permits a missing final suffix,
// matching pathlib.resolve. Cleaning before resolution changes that order.
func ResolvePath(path string) (string, error) {
	if strings.ContainsRune(path, 0) {
		return "", errors.New("embedded null byte")
	}
	if !filepath.IsAbs(path) {
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		path = cwd + string(filepath.Separator) + path
	}
	return resolveAbsolute(path, make(map[string]bool))
}

func resolveAbsolute(path string, active map[string]bool) (string, error) {
	resolved := string(filepath.Separator)
	for _, part := range strings.Split(path, string(filepath.Separator)) {
		switch part {
		case "", ".":
			continue
		case "..":
			resolved = filepath.Dir(resolved)
			continue
		}
		next := filepath.Join(resolved, part)
		info, err := os.Lstat(next)
		if err != nil {
			if os.IsNotExist(err) || errors.Is(err, syscall.ENOTDIR) {
				resolved = next
				continue
			}
			return "", err
		}
		if info.Mode()&os.ModeSymlink == 0 {
			resolved = next
			continue
		}
		if active[next] {
			return "", fmt.Errorf("Symlink loop from %s", quotePath(next))
		}
		target, err := os.Readlink(next)
		if err != nil {
			return "", err
		}
		if !filepath.IsAbs(target) {
			target = resolved + string(filepath.Separator) + target
		}
		active[next] = true
		resolved, err = resolveAbsolute(target, active)
		delete(active, next)
		if err != nil {
			return "", err
		}
	}
	return resolved, nil
}

func (files *Files) Resolve(path string) (string, error) {
	target := path
	if !filepath.IsAbs(target) {
		target = files.root + string(filepath.Separator) + target
	}
	resolved, err := ResolvePath(target)
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(files.root, resolved)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", &PathEscapeError{Path: path}
	}
	return resolved, nil
}

func (files *Files) Read(ctx context.Context, input protocol.ReadFileInput) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	path, err := files.Resolve(input.Path)
	if err != nil {
		return "", err
	}
	file, err := os.Open(path)
	if err != nil {
		return "", fileError(err)
	}
	defer file.Close()
	reader := textReader{reader: bufio.NewReader(file)}
	offset := 0
	if input.Offset != nil && *input.Offset > 0 {
		offset = *input.Offset
	}
	skipped, hitEOF := 0, false
	for skipped < offset && !hitEOF {
		sawContent := false
		for {
			if err := ctx.Err(); err != nil {
				return "", err
			}
			r, err := reader.readRune()
			if err == io.EOF {
				hitEOF = true
				break
			}
			if err != nil {
				return "", fileError(err)
			}
			sawContent = true
			if r == '\n' {
				break
			}
		}
		if sawContent {
			skipped++
		}
	}
	data := make([]rune, 0, 4096)
	for !hitEOF && len(data) <= ReadCharCap {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		r, err := reader.readRune()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fileError(err)
		}
		data = append(data, r)
	}
	if len(data) == 0 && offset > 0 {
		return fmt.Sprintf("... (nothing at offset %d: the file ends after %d lines)", offset, skipped), nil
	}
	truncated := len(data) > ReadCharCap
	if truncated {
		data = data[:ReadCharCap]
	}
	lines := splitLines(data)
	if input.Limit != nil && max(*input.Limit, 0) < len(lines) {
		limit := max(*input.Limit, 0)
		tail := ""
		if truncated {
			tail = ", read truncated"
		}
		lines = append(lines[:limit], fmt.Sprintf("... (%d more lines%s)", len(lines)-limit, tail))
	} else if truncated {
		lines = append([]string{fmt.Sprintf("... (file exceeds %s characters from this offset; read further with a larger `offset`)", comma(ReadCharCap))}, lines...)
	}
	return capOutput(strings.Join(lines, "\n")), nil
}

func (files *Files) Write(ctx context.Context, input protocol.WriteFileInput) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !utf8.ValidString(input.Content) {
		return "", errors.New("content is not valid UTF-8")
	}
	path, err := files.Resolve(input.Path)
	if err != nil {
		return "", err
	}
	if err := makeParents(filepath.Dir(path)); err != nil {
		return "", fileError(err)
	}
	// Parent creation may have crossed a changed symlink; check again before
	// opening the sibling temporary file. This is not OS shell confinement.
	path, err = files.Resolve(input.Path)
	if err != nil {
		return "", err
	}
	if err := atomicWrite(ctx, path, []byte(input.Content), os.Rename); err != nil {
		return "", fileError(err)
	}
	return fmt.Sprintf("Wrote %d bytes to %s", utf8.RuneCountInString(input.Content), input.Path), nil
}

// WriteTookEffect compares the source text without materializing the file. It
// uses the same strict UTF-8/universal-newline reader as Python text reads.
// Errors remain undetermined at the verifier boundary, never evidence to retry.
func (files *Files) WriteTookEffect(ctx context.Context, input protocol.WriteFileInput) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	path, err := files.Resolve(input.Path)
	if err != nil {
		return false, err
	}
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer file.Close()
	reader := textReader{reader: bufio.NewReader(file)}
	expectedText := strings.NewReader(input.Content)
	equal := true
	for {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		actual, err := reader.readStrictRune()
		if err == io.EOF {
			_, _, tail := expectedText.ReadRune()
			return equal && tail == io.EOF, nil
		}
		if err != nil {
			return false, err
		}
		expected, _, tail := expectedText.ReadRune()
		if tail != nil || actual != expected {
			equal = false
		}
	}
}

func (files *Files) Edit(ctx context.Context, input protocol.EditFileInput) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	path, err := files.Resolve(input.Path)
	if err != nil {
		return "", err
	}
	file, err := os.Open(path)
	if err != nil {
		return "", fileError(err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", fileError(err)
	}
	tooLarge := func(size int64) error {
		return fmt.Errorf("%s is %s bytes; too large to edit in one pass (limit %s). Rewrite it with write_file.", input.Path, comma(size), comma(ReadCharCap))
	}
	if info.Size() > ReadCharCap {
		return "", tooLarge(info.Size())
	}
	data, err := io.ReadAll(io.LimitReader(file, ReadCharCap+1))
	if err != nil {
		return "", fileError(err)
	}
	if len(data) > ReadCharCap {
		return "", tooLarge(int64(len(data)))
	}
	if err := validateUTF8(data); err != nil {
		return "", err
	}
	content := strings.ReplaceAll(strings.ReplaceAll(string(data), "\r\n", "\n"), "\r", "\n")
	occurrences := strings.Count(content, input.OldText)
	if occurrences == 0 {
		return "", fmt.Errorf("Text not found in %s. Re-read the file before retrying -- its current content may differ from what you expect, and old_text must match exactly, whitespace included.", input.Path)
	}
	if occurrences > 1 {
		return "", fmt.Errorf("old_text matches %d places in %s; the edit is ambiguous. Include enough surrounding context to identify exactly one occurrence.", occurrences, input.Path)
	}
	updated := strings.Replace(content, input.OldText, input.NewText, 1)
	if !utf8.ValidString(updated) {
		return "", errors.New("content is not valid UTF-8")
	}
	path, err = files.Resolve(input.Path)
	if err != nil {
		return "", err
	}
	if err := atomicWrite(ctx, path, []byte(updated), os.Rename); err != nil {
		return "", fileError(err)
	}
	return "Edited " + input.Path, nil
}

type renameFile func(string, string) error

func makeParents(path string) error {
	if err := os.MkdirAll(path, 0o777); err != nil {
		// pathlib.mkdir(exist_ok=True) still reports EEXIST when the final
		// parent is a file. MkdirAll reports ENOTDIR for the same request.
		if info, statErr := os.Stat(path); statErr == nil && !info.IsDir() {
			return &os.PathError{Op: "mkdir", Path: path, Err: syscall.EEXIST}
		}
		var pathError *os.PathError
		if errors.As(err, &pathError) {
			return &os.PathError{Op: "mkdir", Path: path, Err: pathError.Err}
		}
		return err
	}
	return nil
}

func atomicWrite(ctx context.Context, path string, content []byte, rename renameFile) error {
	var entropy [16]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return err
	}
	temporary := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+"."+hex.EncodeToString(entropy[:])+".tmp")
	file, err := os.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o666)
	if err != nil {
		return err
	}
	defer os.Remove(temporary)
	_, err = file.Write(content)
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := rename(temporary, path); err != nil {
		return err
	}
	if directory, err := os.Open(filepath.Dir(path)); err == nil {
		_ = directory.Sync() // Best effort, as in Python's durable helper.
		_ = directory.Close()
	}
	return nil
}

func quotePath(path string) string {
	quote := "'"
	if strings.Contains(path, "'") && !strings.Contains(path, "\"") {
		quote = "\""
	}
	path = strings.NewReplacer("\\", "\\\\", "\n", "\\n", "\r", "\\r", "\t", "\\t", quote, "\\"+quote).Replace(path)
	return quote + path + quote
}

func fileError(err error) error {
	var pathError *os.PathError
	var errno syscall.Errno
	var paths string
	if errors.As(err, &pathError) && errors.As(pathError.Err, &errno) {
		paths = quotePath(pathError.Path)
	} else {
		var linkError *os.LinkError
		if !errors.As(err, &linkError) || !errors.As(linkError.Err, &errno) {
			return err
		}
		paths = quotePath(linkError.Old) + " -> " + quotePath(linkError.New)
	}
	message := errno.Error()
	if message != "" {
		message = strings.ToUpper(message[:1]) + message[1:]
	}
	return fmt.Errorf("[Errno %d] %s: %s", errno, message, paths)
}

func comma[T ~int | ~int64](number T) string {
	text := fmt.Sprint(number)
	for i := len(text) - 3; i > 0; i -= 3 {
		text = text[:i] + "," + text[i:]
	}
	return text
}

func capOutput(text string) string {
	runes := []rune(text)
	if len(runes) <= OutputCap {
		return text
	}
	note := fmt.Sprintf("\n[truncated: %s characters capped at %s]", comma(len(runes)), comma(OutputCap))
	return string(runes[:OutputCap-utf8.RuneCountInString(note)]) + note
}

func splitLines(data []rune) []string {
	var lines []string
	start := 0
	for i, r := range data {
		if r == '\n' || r == '\r' || r == '\v' || r == '\f' || r == 0x1c || r == 0x1d || r == 0x1e || r == 0x85 || r == 0x2028 || r == 0x2029 {
			lines = append(lines, string(data[start:i]))
			start = i + 1
		}
	}
	if start < len(data) {
		lines = append(lines, string(data[start:]))
	}
	return lines
}
