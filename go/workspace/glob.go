package workspace

import (
	"context"
	"errors"
	"io"
	"os"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/luoyjx/mini-loop/go/protocol"
)

const globTruncated = "... (matches truncated)"

const maxOpenGlobDirectories = 16

var enumerationComplete = errors.New("glob output budget exhausted")

// globYield preserves enumeration order until the output budget is exhausted.
// Sorting and deduplication belong to the final projection, after accounting.
type globYield func(string) error

type globSearch struct {
	ctx             context.Context
	root            string
	patterns        map[string]filenamePattern
	openDirectories int
}

func (files *Files) Glob(ctx context.Context, input protocol.GlobInput) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	search := globSearch{ctx: ctx, root: files.root, patterns: make(map[string]filenamePattern)}
	matches := make(map[string]struct{})
	total, truncated := 0, false
	budget := OutputCap - utf8.RuneCountInString(globTruncated) - 1
	first := true
	skipFirstEmpty := input.Pattern == "" || strings.HasPrefix(input.Pattern, "**")
	err := search.expand(input.Pattern, false, func(match string) error {
		if first {
			first = false
			if skipFirstEmpty && match == "" {
				return nil
			}
		}
		if _, err := files.Resolve(match); err != nil {
			var escape *PathEscapeError
			if errors.As(err, &escape) {
				return nil
			}
			return err
		}
		cost := utf8.RuneCountInString(match) + 1
		if total+cost > budget {
			truncated = true
			return enumerationComplete
		}
		total += cost
		matches[match] = struct{}{}
		return nil
	})
	if err != nil && !errors.Is(err, enumerationComplete) {
		return "", err
	}
	if len(matches) == 0 {
		return "(no matches)", nil
	}
	lines := make([]string, 0, len(matches)+1)
	for match := range matches {
		lines = append(lines, match)
	}
	sort.Strings(lines)
	if truncated {
		lines = append(lines, globTruncated)
	}
	return capOutput(strings.Join(lines, "\n")), nil
}

func hasGlobMagic(path string) bool { return strings.ContainsAny(path, "*?[") }

// These joins retain "./", duplicate separators, and trailing separators in
// the same places as posixpath; filepath.Join would clean the model's spelling.
func globJoin(directory, name string) string {
	if strings.HasPrefix(name, "/") || directory == "" {
		return name
	}
	if strings.HasSuffix(directory, "/") {
		return directory + name
	}
	return directory + "/" + name
}

func globSplit(path string) (string, string) {
	index := strings.LastIndexByte(path, '/')
	if index < 0 {
		return "", path
	}
	directory := path[:index+1]
	if strings.Trim(directory, "/") != "" {
		directory = strings.TrimRight(directory, "/")
	}
	return directory, path[index+1:]
}

func pathExists(path string) bool  { _, err := os.Lstat(path); return err == nil }
func isDirectory(path string) bool { info, err := os.Stat(path); return err == nil && info.IsDir() }

func (search *globSearch) expand(pattern string, directoriesOnly bool, yield globYield) error {
	if err := search.ctx.Err(); err != nil {
		return err
	}
	directory, basename := globSplit(pattern)
	if !hasGlobMagic(pattern) {
		exists := pathExists(globJoin(search.root, pattern))
		if basename == "" {
			exists = isDirectory(globJoin(search.root, directory))
		}
		if exists {
			return yield(pattern)
		}
		return nil
	}
	if directory == "" {
		if basename == "**" {
			return search.recursive(search.root, directoriesOnly, yield)
		}
		return search.matchDirectory(search.root, basename, directoriesOnly, yield)
	}
	inDirectory := func(directory string) error {
		path := globJoin(search.root, directory)
		project := func(name string) error { return yield(globJoin(directory, name)) }
		switch {
		case basename == "**":
			return search.recursive(path, directoriesOnly, project)
		case hasGlobMagic(basename):
			return search.matchDirectory(path, basename, directoriesOnly, project)
		case basename != "":
			if pathExists(globJoin(path, basename)) {
				return project(basename)
			}
		default:
			if isDirectory(path) {
				return project("")
			}
		}
		return nil
	}
	if directory != pattern && hasGlobMagic(directory) {
		return search.expand(directory, true, inDirectory)
	}
	return inDirectory(directory)
}

func (search *globSearch) matchDirectory(directory, pattern string, directoriesOnly bool, yield globYield) error {
	compiled, ok := search.patterns[pattern]
	if !ok {
		compiled = compileFilenamePattern(pattern)
		search.patterns[pattern] = compiled
	}
	includeHidden := strings.HasPrefix(pattern, ".")
	return search.entries(directory, directoriesOnly, func(name string) error {
		if !includeHidden && strings.HasPrefix(name, ".") {
			return nil
		}
		if compiled.matches(name) {
			return yield(name)
		}
		return nil
	})
}

func (search *globSearch) recursive(directory string, directoriesOnly bool, yield globYield) error {
	if err := yield(""); err != nil {
		return err
	}
	return search.descendants(directory, directoriesOnly, yield)
}

func (search *globSearch) descendants(directory string, directoriesOnly bool, yield globYield) error {
	return search.entries(directory, directoriesOnly, func(name string) error {
		if strings.HasPrefix(name, ".") {
			return nil
		}
		if err := yield(name); err != nil {
			return err
		}
		return search.descendants(globJoin(directory, name), directoriesOnly, func(child string) error {
			return yield(globJoin(name, child))
		})
	})
}

// File.ReadDir retains native order, unlike os.ReadDir's sorted projection.
// Reading fixed-size batches avoids collecting the entire tree before capping.
// Python glob suppresses directory I/O errors; context cancellation propagates.
func (search *globSearch) entries(directory string, directoriesOnly bool, yield globYield) error {
	if err := search.ctx.Err(); err != nil {
		return err
	}
	var handle *os.File
	consumed := 0
	closeDirectory := func() {
		if handle != nil {
			_ = handle.Close()
			handle = nil
			search.openDirectories--
		}
	}
	defer closeDirectory()
	for {
		if handle == nil {
			var err error
			handle, err = os.Open(directory)
			if err != nil {
				return nil
			}
			search.openDirectories++
			// Deep traversals close the parent between batches. Resume its
			// native cursor by skipping entries already consumed, retaining
			// enumeration order without holding one descriptor per depth.
			remaining := consumed
			for remaining > 0 {
				if err := search.ctx.Err(); err != nil {
					return err
				}
				skipped, err := handle.ReadDir(min(64, remaining))
				remaining -= len(skipped)
				if err != nil {
					return nil
				}
			}
		}
		entries, readErr := handle.ReadDir(64)
		consumed += len(entries)
		if search.openDirectories >= maxOpenGlobDirectories {
			closeDirectory()
		}
		for _, entry := range entries {
			if err := search.ctx.Err(); err != nil {
				return err
			}
			if directoriesOnly && !isDirectory(globJoin(directory, entry.Name())) {
				continue
			}
			if err := yield(entry.Name()); err != nil {
				return err
			}
		}
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			return nil
		}
	}
}
