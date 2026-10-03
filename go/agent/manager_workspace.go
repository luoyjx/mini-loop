package agent

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/luoyjx/mini-loop/go/workspace"
)

func expandedWorkspacePath(path string) (string, error) {
	if strings.HasPrefix(path, "~") {
		name, rest, _ := strings.Cut(path[1:], string(filepath.Separator))
		var home string
		if name == "" {
			value, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			home = value
		} else {
			account, err := user.Lookup(name)
			if err != nil {
				return "", err
			}
			home = account.HomeDir
		}
		// Keep the suffix lexical until ResolvePath sees it: filepath.Join
		// would erase symlink/.. before resolving the symlink, changing policy.
		path = home
		if rest != "" {
			path += string(filepath.Separator) + rest
		}
	}
	return workspace.ResolvePath(path)
}
func insideDirectory(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
func (manager *SessionManager) bindWorkspace(requested string) (string, error) {
	refuse := func(detail string) (string, error) { return "", &WorkspaceBindingError{BindingForbidden, detail} }
	if len(manager.config.BindableRoots) == 0 {
		return refuse("workspace binding is disabled: configure bindable roots")
	}
	path, err := expandedWorkspacePath(requested)
	if err != nil {
		return "", &WorkspaceBindingError{BindingInvalid, err.Error()}
	}
	if insideDirectory(manager.config.WorkspaceRoot, path) {
		return refuse(fmt.Sprintf("cannot bind %s: it is inside the manager's own workspace root, which holds other sessions' state", requested))
	}
	allowed := false
	for _, root := range manager.config.BindableRoots {
		if insideDirectory(root, path) {
			allowed = true
			break
		}
	}
	if !allowed {
		return refuse(fmt.Sprintf("cannot bind %s: outside every bindable root", requested))
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return "", &WorkspaceBindingError{BindingInvalid, fmt.Sprintf("cannot bind %s: not an existing directory", requested)}
	}
	return path, nil
}
