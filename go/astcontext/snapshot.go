package astcontext

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/luoyjx/mini-loop/go/internal/pytext"
)

type snapshotRewrite struct{ snapshot, workspace string }

// The callback may consume the snapshot only during withSnapshot. It cannot
// extend the private temporary directory's lifetime beyond the invocation.
type sourceSnapshot struct {
	root     string
	paths    []string
	rewrites []snapshotRewrite
}

type snapshotBudget struct {
	entries, files                 int
	bytes, ignoreBytes             int64
	ignorePatterns, ignoreMatchOps int
}

type snapshotControl struct {
	name string
	data []byte
}
type snapshotFrame struct {
	anchor []string
	spec   ignoreSpec
}

type snapshotBuilder struct {
	ctx, caller context.Context
	seconds     float64
	root        string
	limits      snapshotLimits
	budget      snapshotBudget
	controls    map[string][]snapshotControl
}

func (a *Adapter) withSnapshot(ctx context.Context, root string, candidates []string, action func(sourceSnapshot) error) error {
	return a.withSnapshotLimits(ctx, root, candidates, defaultSnapshotLimits(), action)
}

func (a *Adapter) withSnapshotLimits(ctx context.Context, root string, candidates []string, limits snapshotLimits, action func(sourceSnapshot) error) (err error) {
	if a == nil || !a.config.valid {
		return errors.New("configured AST adapter is required")
	}
	if err := snapshotContext(ctx); err != nil {
		return err
	}
	if action == nil {
		return errors.New("snapshot consumer is required")
	}
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return errors.New("snapshot root must be a canonical absolute directory")
	}
	paths, err := snapshotTargets(root, candidates)
	if err != nil {
		return err
	}
	bounded, cancel := context.WithTimeout(ctx, a.config.timeout)
	defer cancel()
	parent, err := temporaryParent(root)
	if err != nil {
		return err
	}
	temporary, err := os.MkdirTemp(parent, "mini-loop-ast-")
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, os.RemoveAll(temporary)) }()
	snapshotRoot := filepath.Join(temporary, "workspace")
	if err := os.Mkdir(snapshotRoot, 0700); err != nil {
		return err
	}
	builder := snapshotBuilder{ctx: bounded, caller: ctx, seconds: a.config.parameters.TimeoutSeconds,
		root: snapshotRoot, limits: limits, controls: make(map[string][]snapshotControl)}
	if err := builder.populate(root, paths); err != nil {
		return err
	}
	selected := make([]string, len(paths))
	for i, path := range paths {
		selected[i] = filepath.Join(snapshotRoot, path)
	}
	if err := builder.check(); err != nil {
		return err
	}
	return action(sourceSnapshot{root: snapshotRoot, paths: selected,
		rewrites: []snapshotRewrite{{snapshot: snapshotRoot, workspace: root}}})
}

func (b *snapshotBuilder) populate(workspace string, paths []string) error {
	anchor, err := openSnapshotDirectory(b.ctx, workspace)
	if err != nil {
		return b.filesystemError(err)
	}
	defer anchor.close()
	ordered := slices.Clone(paths)
	sort.Slice(ordered, func(i, j int) bool {
		left, right := snapshotParts(ordered[i]), snapshotParts(ordered[j])
		if len(left) != len(right) {
			return len(left) < len(right)
		}
		return ordered[i] < ordered[j]
	})
	for _, path := range ordered {
		if err := b.target(anchor, snapshotParts(path)); err != nil {
			return err
		}
	}
	return nil
}

func snapshotTargets(root string, candidates []string) ([]string, error) {
	result := []string{}
	seen := make(map[string]bool)
	for _, candidate := range candidates {
		if !filepath.IsAbs(candidate) || filepath.Clean(candidate) != candidate || !contained(root, candidate) {
			return nil, errors.New("snapshot target is outside workspace")
		}
		relative, err := filepath.Rel(root, candidate)
		if err != nil {
			return nil, err
		}
		for _, part := range snapshotParts(relative) {
			if err := snapshotComponent(part); err != nil {
				return nil, err
			}
		}
		if !seen[relative] {
			result = append(result, relative)
			seen[relative] = true
		}
	}
	if len(result) == 0 {
		return nil, errors.New("at least one snapshot target is required")
	}
	return result, nil
}

func snapshotParts(path string) []string {
	if path == "." {
		return nil
	}
	return strings.Split(path, string(filepath.Separator))
}

func (b *snapshotBuilder) destination(parts []string) string {
	return filepath.Join(b.root, filepath.Join(parts...))
}

func (b *snapshotBuilder) check() error {
	if err := b.caller.Err(); err != nil {
		return err
	}
	if b.ctx.Err() != nil {
		return fmt.Errorf("ast-outline source snapshot exceeded %ss", strconv.FormatFloat(b.seconds, 'g', 6, 64))
	}
	return nil
}

func (b *snapshotBuilder) filesystemError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		if reason := b.check(); reason != nil {
			return reason
		}
	}
	var pathError *os.PathError
	var errno syscall.Errno
	if errors.As(err, &pathError) && errors.As(pathError.Err, &errno) {
		return snapshotPathFailure{cause: pathError, errno: errno}
	}
	return err
}

// Keep the native errno cause while projecting Source's OSError text. Descriptor
// operations report the selected child component, not a pathname re-resolution.
type snapshotPathFailure struct {
	cause *os.PathError
	errno syscall.Errno
}

func (failure snapshotPathFailure) Error() string {
	message := failure.errno.Error()
	if len(message) > 0 {
		message = strings.ToUpper(message[:1]) + message[1:]
	}
	return fmt.Sprintf("[Errno %d] %s: %s", failure.errno, message, pytext.Repr(failure.cause.Path))
}
func (failure snapshotPathFailure) Unwrap() error { return failure.cause }

func (b *snapshotBuilder) fileCapacity(bytes int64) error {
	if b.budget.files >= b.limits.files {
		return fmt.Errorf("ast-outline snapshot exceeds %d files", b.limits.files)
	}
	if bytes > b.limits.bytes-b.budget.bytes {
		return fmt.Errorf("ast-outline snapshot exceeds %d bytes", b.limits.bytes)
	}
	return nil
}

func (b *snapshotBuilder) writeControl(data []byte, destination string) error {
	if err := b.fileCapacity(int64(len(data))); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
		return err
	}
	file, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(data)
	if writeErr == nil {
		writeErr = file.Chmod(0400)
	}
	err = errors.Join(writeErr, file.Close())
	if err != nil {
		return err
	}
	b.budget.files++
	b.budget.bytes += int64(len(data))
	return nil
}

func (b *snapshotBuilder) copyRegular(source *os.File, destination string) error {
	metadata, err := source.Stat()
	if err != nil {
		return err
	}
	if !metadata.Mode().IsRegular() {
		return errors.New("ast-outline snapshot accepts regular files only")
	}
	if err := b.fileCapacity(metadata.Size()); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
		return err
	}
	b.budget.files++
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer output.Close()
	buffer := make([]byte, 128*1024)
	for {
		if err := b.check(); err != nil {
			return err
		}
		n, readErr := source.Read(buffer)
		b.budget.bytes += int64(n)
		if b.budget.bytes > b.limits.bytes {
			return fmt.Errorf("ast-outline snapshot exceeds %d bytes", b.limits.bytes)
		}
		if _, err := output.Write(buffer[:n]); err != nil {
			return err
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	if err := output.Chmod(0400); err != nil {
		return err
	}
	return output.Close()
}

func (b *snapshotBuilder) readControls(directory *snapshotDirectory, anchor []string) ([]snapshotControl, error) {
	key := strings.Join(anchor, "/")
	if controls, ok := b.controls[key]; ok {
		return controls, nil
	}
	controls := []snapshotControl{}
	for _, name := range snapshotIgnoreNames {
		if err := b.check(); err != nil {
			return nil, err
		}
		metadata, err := directory.metadata(b.ctx, name)
		if err != nil {
			if reason := b.check(); reason != nil {
				return nil, reason
			}
			continue
		}
		if metadata.kind != snapshotRegular {
			continue
		}
		remaining := b.limits.ignoreBytes - b.budget.ignoreBytes
		if metadata.size > remaining {
			return nil, fmt.Errorf("ast-outline ignore files exceed %d bytes", b.limits.ignoreBytes)
		}
		file, err := directory.regular(b.ctx, name)
		if err != nil {
			if reason := b.check(); reason != nil {
				return nil, reason
			}
			continue
		}
		data, readErr := io.ReadAll(io.LimitReader(file, remaining+1))
		err = errors.Join(readErr, file.Close())
		if err != nil {
			return nil, err
		}
		if err := b.check(); err != nil {
			return nil, err
		}
		if int64(len(data)) > remaining {
			return nil, fmt.Errorf("ast-outline ignore files exceed %d bytes", b.limits.ignoreBytes)
		}
		lines := ignoreLines(data)
		if err := validateIgnoreLines(lines); err != nil {
			return nil, err
		}
		if len(lines) > b.limits.ignorePatterns-b.budget.ignorePatterns {
			return nil, fmt.Errorf("ast-outline ignore files exceed %d patterns", b.limits.ignorePatterns)
		}
		b.budget.ignoreBytes += int64(len(data))
		b.budget.ignorePatterns += len(lines)
		controls = append(controls, snapshotControl{name: name, data: data})
	}
	b.controls[key] = controls
	return controls, nil
}

func snapshotControlLines(controls []snapshotControl) []string {
	lines := []string{}
	for _, control := range controls {
		lines = append(lines, ignoreLines(control.data)...)
	}
	return lines
}

func (b *snapshotBuilder) copyControls(controls []snapshotControl, destination string) error {
	for _, control := range controls {
		path := filepath.Join(destination, control.name)
		if _, err := os.Stat(path); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := b.writeControl(control.data, path); err != nil {
			return err
		}
	}
	return nil
}

func (b *snapshotBuilder) ignored(parts []string, isDirectory bool, frames []snapshotFrame) (bool, error) {
	for i := len(frames) - 1; i >= 0; i-- {
		if err := b.check(); err != nil {
			return false, err
		}
		frame := frames[i]
		if len(parts) < len(frame.anchor) || !slices.Equal(parts[:len(frame.anchor)], frame.anchor) {
			continue
		}
		b.budget.ignoreMatchOps += len(frame.spec.patterns)
		if b.budget.ignoreMatchOps > b.limits.ignoreMatchOps {
			return false, fmt.Errorf("ast-outline ignore matching exceeds %d pattern checks", b.limits.ignoreMatchOps)
		}
		rendered := strings.Join(parts[len(frame.anchor):], "/")
		if isDirectory {
			rendered += "/"
		}
		switch frame.spec.check(rendered).disposition {
		case ignored:
			return true, nil
		case kept:
			return false, nil
		}
	}
	return false, nil
}

func (b *snapshotBuilder) projectRoot(root *snapshotDirectory, parts []string) ([]string, error) {
	current, err := root.parent(b.ctx, nil)
	if err != nil {
		return nil, b.filesystemError(err)
	}
	defer func() { current.close() }()
	var nearest []string
	found := false
	for i := 0; ; i++ {
		if err := b.check(); err != nil {
			return nil, err
		}
		metadata, err := current.metadata(b.ctx, ".git")
		if err == nil && metadata.kind != snapshotSymlink {
			nearest = slices.Clone(parts[:i])
			found = true
		}
		if i == len(parts) {
			break
		}
		next, err := current.child(b.ctx, parts[i])
		if err != nil {
			return nil, b.filesystemError(err)
		}
		current.close()
		current = next
	}
	if !found {
		return slices.Clone(parts), nil
	}
	return nearest, nil
}

func (b *snapshotBuilder) copyDirectory(directory *snapshotDirectory, parts, project []string, frames []snapshotFrame, depth int) error {
	if err := b.check(); err != nil {
		return err
	}
	if depth > b.limits.depth {
		return fmt.Errorf("ast-outline snapshot exceeds %d directory levels", b.limits.depth)
	}
	destination := b.destination(parts)
	if err := os.MkdirAll(destination, 0700); err != nil {
		return err
	}
	controls, err := b.readControls(directory, parts)
	if err != nil {
		return err
	}
	if !slices.Equal(parts, project) && len(controls) > 0 {
		spec, err := compileIgnore(snapshotControlLines(controls))
		if err != nil {
			return err
		}
		frames = append(slices.Clone(frames), snapshotFrame{anchor: slices.Clone(parts), spec: spec})
	}
	if err := b.copyControls(controls, destination); err != nil {
		return err
	}
	names, err := directory.names(b.ctx, b.limits.entries-b.budget.entries)
	if errors.Is(err, errSnapshotEntryLimit) {
		return fmt.Errorf("ast-outline snapshot exceeds %d entries", b.limits.entries)
	}
	if err != nil {
		return b.filesystemError(err)
	}
	b.budget.entries += len(names)
	for _, name := range names {
		if slices.Contains(snapshotIgnoreNames, name) {
			continue
		}
		childParts := append(slices.Clone(parts), name)
		if err := b.copyEntry(directory, name, childParts, project, frames, depth+1); err != nil {
			return err
		}
	}
	return nil
}

func (b *snapshotBuilder) copyEntry(parent *snapshotDirectory, name string, parts, project []string, frames []snapshotFrame, depth int) error {
	if err := b.check(); err != nil {
		return err
	}
	metadata, err := parent.metadata(b.ctx, name)
	if err != nil {
		return b.filesystemError(err)
	}
	destination := b.destination(parts)
	switch metadata.kind {
	case snapshotDir:
		if snapshotVCS(name) {
			return nil
		}
		ignored, err := b.ignored(parts, true, frames)
		if err != nil || ignored {
			return err
		}
		child, err := parent.child(b.ctx, name)
		if err != nil {
			return b.filesystemError(err)
		}
		defer child.close()
		if info, err := os.Stat(destination); err == nil && !info.IsDir() {
			return errors.New("ast-outline snapshot path changed type")
		}
		return b.copyDirectory(child, parts, project, frames, depth)
	case snapshotRegular:
		if !snapshotSupportedName(name) {
			return nil
		}
		ignored, err := b.ignored(parts, false, frames)
		if err != nil || ignored {
			return err
		}
		if info, err := os.Stat(destination); err == nil {
			if !info.Mode().IsRegular() {
				return errors.New("ast-outline snapshot path changed type")
			}
			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		file, err := parent.regular(b.ctx, name)
		if err != nil {
			return b.filesystemError(err)
		}
		defer file.Close()
		return b.copyRegular(file, destination)
	default:
		return nil
	}
}

func (b *snapshotBuilder) target(root *snapshotDirectory, parts []string) error {
	if err := b.check(); err != nil {
		return err
	}
	parentParts, name := parts, ""
	if len(parts) > 0 {
		parentParts, name = parts[:len(parts)-1], parts[len(parts)-1]
	}
	parent, err := root.parent(b.ctx, parentParts)
	if err != nil {
		return b.filesystemError(err)
	}
	defer parent.close()
	metadata := snapshotMetadata{kind: snapshotDir}
	if name != "" {
		metadata, err = parent.metadata(b.ctx, name)
		if err != nil {
			return b.filesystemError(err)
		}
	}
	destination := b.destination(parts)
	switch metadata.kind {
	case snapshotSymlink:
		return errors.New("ast-outline snapshot target may not be a symlink")
	case snapshotRegular:
		if _, err := os.Stat(destination); err == nil {
			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		file, err := parent.regular(b.ctx, name)
		if err != nil {
			return b.filesystemError(err)
		}
		defer file.Close()
		return b.copyRegular(file, destination)
	case snapshotDir:
		project, err := b.projectRoot(root, parts)
		if err != nil {
			return err
		}
		projectDir, err := root.parent(b.ctx, project)
		if err != nil {
			return b.filesystemError(err)
		}
		controls, err := b.readControls(projectDir, project)
		projectDir.close()
		if err != nil {
			return err
		}
		lines := append(slices.Clone(snapshotDefaultIgnores), snapshotControlLines(controls)...)
		spec, err := compileIgnore(lines)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Join(b.destination(project), ".git"), 0700); err != nil {
			return err
		}
		if err := b.copyControls(controls, b.destination(project)); err != nil {
			return err
		}
		var child *snapshotDirectory
		if name == "" {
			child, err = parent.parent(b.ctx, nil)
		} else {
			child, err = parent.child(b.ctx, name)
		}
		if err != nil {
			return b.filesystemError(err)
		}
		defer child.close()
		return b.copyDirectory(child, parts, project, []snapshotFrame{{anchor: project, spec: spec}}, len(parts))
	default:
		return errors.New("ast-outline snapshot target is not a regular file or directory")
	}
}
