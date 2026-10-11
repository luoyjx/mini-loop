//go:build darwin || linux

package reload

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestBuildCancellationKillsCompilerDescendants(t *testing.T) {
	root := buildFixture(t)
	workspace, err := NewBuildWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = workspace.Close() })
	pidPath := filepath.Join(root, "child.pid")
	toolchain := filepath.Join(root, "waiting-go")
	// The explicit test toolchain starts a helper that holds diagnostic pipes.
	script := "#!/bin/sh\nsleep 300 &\necho $! > \"$RELOAD_HELPER_PID_PATH\"\nwait\n"
	if err := os.WriteFile(toolchain, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	builder, err := NewBuilder(BuildConfig{ModuleDirectory: root, Toolchain: toolchain, Workspace: workspace, Environment: append(os.Environ(), "RELOAD_HELPER_PID_PATH="+pidPath)})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := builder.Build(ctx, nil); done <- err }()
	var pid int
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(pidPath)
		if err == nil {
			pid, err = strconv.Atoi(strings.TrimSpace(string(data)))
			if err == nil && pid > 0 {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if pid == 0 {
		t.Fatal("test compiler helper did not start")
	}
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	if err := workspace.Close(); !errors.Is(err, ErrBuildWorkspaceBusy) {
		t.Fatal("active compiler lease", err)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled compiler was not joined")
	}
	assertNoBuildOutputs(t, workspace.root)
	if err := workspace.Close(); err != nil {
		t.Fatal("cancelled compiler lease", err)
	}
	// macOS reaps this helper immediately. Linux may briefly retain a dead zombie;
	// a zombie has no running code or inherited file descriptors.
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
		if state, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid)); err == nil && strings.Contains(string(state), ") Z ") {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("compiler descendant survived cancellation", pid)
}
