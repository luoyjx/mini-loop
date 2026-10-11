package reload

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var ErrBuildWorkspaceBusy = errors.New("build workspace still has active users")
var ErrBuildWorkspaceClosed = errors.New("build workspace is closed or uninitialized")

// BuildWorkspace owns one private parent for compiler scratch and child artifacts.
// Builder attempts/artifacts and bound observers lease it. Close refuses active
// users; callers join/close those users before deleting the parent. It is a local
// operator capability, not a model-selected source exclusion or execution sandbox.
type BuildWorkspace struct {
	mu     sync.Mutex
	root   string
	users  int
	closed bool
}

func NewBuildWorkspace(parent string) (*BuildWorkspace, error) {
	root, err := os.MkdirTemp(parent, "miniloop-reload-workspace-")
	if err != nil {
		return nil, err
	}
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, errors.Join(err, os.RemoveAll(root))
	}
	canonical, err = filepath.Abs(canonical)
	if err != nil {
		return nil, errors.Join(err, os.RemoveAll(root))
	}
	return &BuildWorkspace{root: canonical}, nil
}

func (workspace *BuildWorkspace) acquire() (string, error) {
	workspace.mu.Lock()
	defer workspace.mu.Unlock()
	if workspace.closed || workspace.root == "" {
		return "", ErrBuildWorkspaceClosed
	}
	workspace.users++
	return workspace.root, nil
}
func (workspace *BuildWorkspace) release() {
	workspace.mu.Lock()
	defer workspace.mu.Unlock()
	workspace.users--
}
func (workspace *BuildWorkspace) Close() error {
	workspace.mu.Lock()
	defer workspace.mu.Unlock()
	if workspace.closed {
		return nil
	}
	if workspace.root == "" {
		return ErrBuildWorkspaceClosed
	}
	if workspace.users != 0 {
		return ErrBuildWorkspaceBusy
	}
	if err := os.RemoveAll(workspace.root); err != nil {
		return err
	}
	workspace.closed = true
	return nil
}

func insideBuildWorkspace(root, path string) bool {
	if root == "" || !filepath.IsAbs(path) {
		return false
	}
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

// workspaceObserver releases its exclusion lease only after the native/polling
// subscription joins; queued removal notifications remain excluded throughout.
type workspaceObserver struct {
	Observer
	workspace *BuildWorkspace
	once      sync.Once
	err       error
}

func (observer *workspaceObserver) Close() error {
	observer.once.Do(func() { observer.err = observer.Observer.Close(); observer.workspace.release() })
	return observer.err
}
