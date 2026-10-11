package userresources

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

type OwnerID string

// DirectoryBinding is an immutable operator snapshot, never an HTTP receipt.
type DirectoryBinding struct {
	owner                OwnerID
	key                  DirectoryKey
	root, skills, memory string
}

func (b DirectoryBinding) Owner() OwnerID    { return b.owner }
func (b DirectoryBinding) Key() DirectoryKey { return b.key }
func (b DirectoryBinding) Root() string      { return b.root }
func (b DirectoryBinding) Skills() string    { return b.skills }
func (b DirectoryBinding) Memory() string    { return b.memory }

// DirectoryResolver owns only directory bindings; no skill/memory store is created.
type DirectoryResolver struct {
	root     string
	permit   chan struct{}
	bindings map[OwnerID]DirectoryBinding
}

var errDirectory = errors.New("user resource directory is unavailable")
var errLink = errors.New("user resource directory must not be a symlink")
var errOutside = errors.New("user resource directory resolves outside the configured root")

// resolveRoot follows trusted configuration links, including dangling targets,
// with Python resolve(strict=False) component order (link/.. is not pre-cleaned).
func resolveRoot(ctx context.Context, path string) (string, error) {
	if !utf8.ValidString(path) || strings.ContainsRune(path, 0) {
		return "", errDirectory
	}
	if !filepath.IsAbs(path) {
		cwd, e := os.Getwd()
		if e != nil {
			return "", errDirectory
		}
		path = cwd + string(os.PathSeparator) + path
	}
	queue := strings.Split(path, string(os.PathSeparator))
	var stack []string
	links := 0
	for len(queue) > 0 {
		if e := ctx.Err(); e != nil {
			return "", e
		}
		part := queue[0]
		queue = queue[1:]
		if part == "" || part == "." {
			continue
		}
		if part == ".." {
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			continue
		}
		candidate := string(os.PathSeparator) + strings.Join(append(append([]string(nil), stack...), part), string(os.PathSeparator))
		info, e := os.Lstat(candidate)
		if e != nil && !os.IsNotExist(e) {
			return "", errDirectory
		}
		if e == nil && info.Mode()&os.ModeSymlink != 0 {
			links++
			if links > 128 {
				return "", errDirectory
			}
			target, e := os.Readlink(candidate)
			if e != nil {
				return "", errDirectory
			}
			if filepath.IsAbs(target) {
				stack = nil
			}
			queue = append(strings.Split(target, string(os.PathSeparator)), queue...)
			continue
		}
		stack = append(stack, part)
	}
	return string(os.PathSeparator) + strings.Join(stack, string(os.PathSeparator)), nil
}
func NewDirectoryResolver(ctx context.Context, root string) (*DirectoryResolver, error) {
	path, e := resolveRoot(ctx, root)
	if e != nil {
		return nil, e
	}
	if e = os.MkdirAll(path, 0700); e != nil {
		return nil, errDirectory
	}
	if e = os.Chmod(path, 0700); e != nil {
		return nil, errDirectory
	}
	if e = ctx.Err(); e != nil {
		return nil, e
	}
	r := &DirectoryResolver{root: path, permit: make(chan struct{}, 1), bindings: make(map[OwnerID]DirectoryBinding)}
	r.permit <- struct{}{}
	return r, nil
}
func (r *DirectoryResolver) directory(ctx context.Context, path string) (string, error) {
	if e := ctx.Err(); e != nil {
		return "", e
	}
	info, e := os.Lstat(path)
	if e == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", errLink
	}
	if e != nil && !os.IsNotExist(e) {
		return "", errDirectory
	}
	if e = os.MkdirAll(path, 0700); e != nil {
		return "", errDirectory
	}
	if e = os.Chmod(path, 0700); e != nil {
		return "", errDirectory
	}
	resolved, e := filepath.EvalSymlinks(path)
	if e != nil {
		return "", errDirectory
	}
	relative, e := filepath.Rel(r.root, resolved)
	if e != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return "", errOutside
	}
	return resolved, nil
}
func (r *DirectoryResolver) ForOwner(ctx context.Context, owner OwnerID) (DirectoryBinding, error) {
	if r == nil || r.permit == nil {
		return DirectoryBinding{}, errDirectory
	}
	key, e := OwnerDirectoryKey(string(owner))
	if e != nil {
		return DirectoryBinding{}, e
	}
	select {
	case <-ctx.Done():
		return DirectoryBinding{}, ctx.Err()
	case <-r.permit:
	}
	defer func() { r.permit <- struct{}{} }()
	if e = ctx.Err(); e != nil {
		return DirectoryBinding{}, e
	}
	if b, ok := r.bindings[owner]; ok {
		return b, nil
	}
	root, e := r.directory(ctx, filepath.Join(r.root, string(key)))
	if e != nil {
		return DirectoryBinding{}, e
	}
	skills, e := r.directory(ctx, filepath.Join(root, "skills"))
	if e != nil {
		return DirectoryBinding{}, e
	}
	memory, e := r.directory(ctx, filepath.Join(root, "memory"))
	if e != nil {
		return DirectoryBinding{}, e
	}
	if e = ctx.Err(); e != nil {
		return DirectoryBinding{}, e
	}
	b := DirectoryBinding{owner, key, root, skills, memory}
	r.bindings[owner] = b
	return b, nil
}
