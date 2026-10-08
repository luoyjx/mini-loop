package astcontext

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/luoyjx/mini-loop/go/internal/fnmatch"
	"github.com/luoyjx/mini-loop/go/internal/pytext"
)

const maxASTInputItems = 64
const maxASTInputCharacters = 2048
const maxASTGlobEntries = 10_000

func astStrings(values []string, label string) ([]string, error) {
	result := make([]string, 0, min(len(values), maxASTInputItems))
	for _, value := range values {
		if len(result) >= maxASTInputItems {
			return nil, fmt.Errorf("%s must contain at most %d items", label, maxASTInputItems)
		}
		if value == "" || strings.ContainsRune(value, 0) {
			return nil, fmt.Errorf("%s must be a non-empty array of strings", label)
		}
		if utf8.RuneCountInString(value) > maxASTInputCharacters {
			return nil, fmt.Errorf("%s entries must be at most %d characters", label, maxASTInputCharacters)
		}
		result = append(result, value)
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("%s must be a non-empty array of strings", label)
	}
	return result, nil
}

func astPathInput(raw string) error {
	if raw == "" || strings.ContainsRune(raw, 0) {
		return errors.New("paths must be non-empty strings without NUL bytes")
	}
	if utf8.RuneCountInString(raw) > maxASTInputCharacters {
		return fmt.Errorf("paths must be at most %d characters", maxASTInputCharacters)
	}
	return nil
}

func astHasGlob(raw string) bool { return strings.ContainsAny(raw, "*?[") }

func astExpandHome(raw string) (string, error) {
	if !strings.HasPrefix(raw, "~") {
		return raw, nil
	}
	name, suffix, _ := strings.Cut(raw, string(filepath.Separator))
	var home string
	if name == "~" {
		value, present := os.LookupEnv("HOME")
		if present {
			home = value
		} else {
			current, err := user.Current()
			if err != nil {
				return "", errors.New("Could not determine home directory.")
			}
			home = current.HomeDir
		}
	} else {
		account, err := user.Lookup(strings.TrimPrefix(name, "~"))
		if err != nil {
			return "", errors.New("Could not determine home directory.")
		}
		home = account.HomeDir
	}
	if strings.HasPrefix(home, "~") {
		return "", errors.New("Could not determine home directory.")
	}
	// Do not clean away symlink/.. order in the remainder before realpath.
	if suffix == "" {
		if home == "" {
			return ".", nil
		}
		return home, nil
	}
	if home == "" {
		return suffix, nil
	}
	return strings.TrimRight(home, string(filepath.Separator)) + string(filepath.Separator) + suffix, nil
}

// Python's non-strict realpath permits missing suffixes and suppresses ordinary
// metadata/readlink errors. Its symlink/.. order differs from lexical cleaning.
// Actual source access is later reopened through no-follow snapshot handles.
func astResolve(ctx context.Context, raw string) (string, error) {
	if strings.ContainsRune(raw, 0) {
		return "", errors.New("embedded null byte")
	}
	if !filepath.IsAbs(raw) {
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		raw = cwd + string(filepath.Separator) + raw
	}
	return astResolveAbsolute(ctx, raw, make(map[string]bool))
}

func astResolveAbsolute(ctx context.Context, path string, active map[string]bool) (string, error) {
	resolved := string(filepath.Separator)
	for _, part := range strings.Split(path, string(filepath.Separator)) {
		if err := snapshotContext(ctx); err != nil {
			return "", err
		}
		switch part {
		case "", ".":
			continue
		case "..":
			resolved = filepath.Dir(resolved)
			continue
		}
		next := filepath.Join(resolved, part)
		metadata, err := os.Lstat(next)
		if err != nil || metadata.Mode()&os.ModeSymlink == 0 {
			resolved = next
			continue
		}
		if active[next] {
			return "", fmt.Errorf("Symlink loop from %s", pytext.Repr(next))
		}
		target, err := os.Readlink(next)
		if err != nil {
			resolved = next
			continue
		}
		if !filepath.IsAbs(target) {
			target = resolved + string(filepath.Separator) + target
		}
		active[next] = true
		resolved, err = astResolveAbsolute(ctx, target, active)
		delete(active, next)
		if err != nil {
			return "", err
		}
	}
	return resolved, nil
}

func astWorkspace(ctx context.Context, raw string) (string, error) {
	if err := snapshotContext(ctx); err != nil {
		return "", err
	}
	expanded, err := astExpandHome(raw)
	if err != nil {
		return "", err
	}
	root, err := astResolve(ctx, expanded)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("workspace is not a directory: %s", root)
	}
	return root, nil
}

func astSafePath(ctx context.Context, root, raw string) (string, error) {
	if err := snapshotContext(ctx); err != nil {
		return "", err
	}
	if err := astPathInput(raw); err != nil {
		return "", err
	}
	if astHasGlob(raw) {
		return "", errors.New("glob syntax is accepted only by show_symbol's bounded basename matcher")
	}
	expanded, err := astExpandHome(raw)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(expanded) {
		expanded = root + string(filepath.Separator) + expanded
	}
	path, err := astResolve(ctx, expanded)
	if err != nil {
		return "", err
	}
	if !contained(root, path) {
		return "", fmt.Errorf("path resolves outside workspace: %s", raw)
	}
	return path, nil
}

func (a *Adapter) safeGlobPaths(ctx context.Context, root, raw string) ([]string, error) {
	return a.safeGlobPathsLimit(ctx, root, raw, maxASTGlobEntries)
}

func (a *Adapter) safeGlobPathsLimit(ctx context.Context, root, raw string, limit int) ([]string, error) {
	if a == nil || !a.config.valid {
		return nil, errors.New("configured AST adapter is required")
	}
	if err := snapshotContext(ctx); err != nil {
		return nil, err
	}
	if err := astPathInput(raw); err != nil {
		return nil, err
	}
	expanded, err := astExpandHome(raw)
	if err != nil {
		return nil, err
	}
	// Path.parts collapses empty/dot components, retains dot-dot and a POSIX root.
	parts := []string{}
	for _, part := range strings.Split(expanded, string(filepath.Separator)) {
		if part != "" && part != "." {
			parts = append(parts, part)
		}
	}
	globIndex := -1
	for i, part := range parts {
		if astHasGlob(part) {
			globIndex = i
			break
		}
	}
	if globIndex < 0 {
		return nil, errors.New("basename glob syntax is required")
	}
	for _, part := range parts {
		if strings.Contains(part, "**") {
			return nil, errors.New("recursive glob '**' is not allowed")
		}
	}
	if globIndex != len(parts)-1 {
		return nil, errors.New("glob wildcards are allowed only in the final path component")
	}
	static := filepath.Join(parts[:globIndex]...)
	if filepath.IsAbs(expanded) {
		static = string(filepath.Separator) + static
	} else {
		static = filepath.Join(root, static)
	}
	prefix := filepath.Clean(static)
	if !contained(root, prefix) || (strings.HasPrefix(expanded, "//") && !strings.HasPrefix(expanded, "///")) {
		return nil, fmt.Errorf("glob resolves outside workspace: %s", raw)
	}
	relative, err := filepath.Rel(root, prefix)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(a.config.timeout)
	anchor, err := openSnapshotDirectory(ctx, root)
	if errors.Is(err, errSnapshotPlatform) {
		return nil, errors.New("secure ast-outline globs require dir_fd and O_NOFOLLOW support")
	}
	if err != nil {
		return nil, err
	}
	defer anchor.close()
	directory, err := anchor.parent(ctx, snapshotParts(relative))
	if err != nil {
		if reason := snapshotContext(ctx); reason != nil {
			return nil, reason
		}
		return nil, fmt.Errorf("glob static directory is unavailable or outside workspace: %s", raw)
	}
	defer directory.close()
	scan, err := directory.scanFile()
	if err != nil {
		return nil, err
	}
	defer scan.Close()
	patternText := parts[globIndex]
	pattern := fnmatch.Compile(patternText)
	paths := []string{}
	entries := 0
	for {
		batch, readErr := scan.Readdirnames(128)
		for _, name := range batch {
			if err := snapshotContext(ctx); err != nil {
				return nil, err
			}
			if time.Now().After(deadline) {
				return nil, fmt.Errorf("ast-outline glob expansion exceeded %ss", strconv.FormatFloat(a.config.parameters.TimeoutSeconds, 'g', 6, 64))
			}
			entries++
			if entries > limit {
				return nil, fmt.Errorf("glob directory contains more than %d entries", limit)
			}
			if strings.HasPrefix(name, ".") && !strings.HasPrefix(patternText, ".") {
				continue
			}
			if !pattern.Matches(name) {
				continue
			}
			metadata, err := directory.metadata(ctx, name)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return nil, err
			}
			if metadata.kind == snapshotRegular {
				paths = append(paths, filepath.Join(prefix, name))
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, readErr
		}
	}
	if err := snapshotContext(ctx); err != nil {
		return nil, err
	}
	sort.Strings(paths)
	return paths, nil
}
