//go:build darwin || linux

package shell

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/secrets"
	"github.com/luoyjx/mini-loop/go/spill"
)

func TestWorkspaceRebindKeepsSandboxCredentialsCaptureAndDeadline(t *testing.T) {
	registry := secrets.New(secrets.Config{})
	registry.RegisterValue("WORKTREE_TEST_TOKEN", "worktree-credential")
	old := makeExecutor(t, Config{Sandbox: testSandbox{}, Secrets: registry, Timeout: 200 * time.Millisecond, CaptureLimit: 512})
	next, err := old.WithWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	result := execute(t, next, "printf '%s\\n' \"$WORKTREE_TEST_TOKEN\"; pwd; printf fresh > bound.txt")
	if result.Failed() || strings.Contains(result.Stdout, "worktree-credential") || !strings.Contains(result.Stdout, secrets.Mask) || strings.Count(result.Stdout, next.Workspace()) != 2 {
		t.Fatal("cwd, sandbox or credentials remained stale", result)
	}
	if raw, err := os.ReadFile(filepath.Join(next.Workspace(), "bound.txt")); err != nil || string(raw) != "fresh" {
		t.Fatal("rebound write failed", err)
	}
	if _, err := os.Stat(filepath.Join(old.Workspace(), "bound.txt")); !os.IsNotExist(err) {
		t.Fatal("rebind changed original executor")
	}
	if result := execute(t, old, "pwd"); strings.Count(result.Stdout, old.Workspace()) != 2 {
		t.Fatal("original sandbox scope changed", result)
	}
	if result := execute(t, next, "awk 'BEGIN{for(i=0;i<2000;i++)printf \"x\"}'"); !result.Overflowed {
		t.Fatal("capture policy was lost", result)
	}
	if result := execute(t, next, "sleep 5"); !result.TimedOut {
		t.Fatal("deadline policy was lost", result)
	}
}

func TestWorkspaceRebindKeepsPrivateSpillAndInterruptOwnership(t *testing.T) {
	store, err := spill.NewLocalStore(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatal(err)
	}
	old := makeExecutor(t, Config{Spill: store})
	next, err := old.WithWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	output, err := next.ExecuteBash(context.Background(), protocol.BashInput{Command: "awk 'BEGIN{for(i=0;i<60000;i++)printf \"x\"}'"})
	if err != nil || !strings.Contains(output, "full output preserved:") {
		t.Fatal("spill store was lost", err)
	}
	artifacts, _ := filepath.Glob(filepath.Join(store.Root(), "session-*", "*.txt"))
	if len(artifacts) != 1 {
		t.Fatal("rebind used another artifact store")
	}
	marker := filepath.Join(next.Workspace(), "started")
	done := make(chan Result, 1)
	go func() {
		result, _ := next.ExecuteBashResult(context.Background(), protocol.BashInput{Command: "touch started; sleep 30"})
		done <- result
	}()
	awaitFile(t, marker)
	if old.Interrupt() != 1 {
		t.Fatal("rebind lost foreground interrupt ownership")
	}
	select {
	case result := <-done:
		if !result.Failed() {
			t.Fatal("interrupt was ignored")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("interrupt failed to join rebound process")
	}
}
