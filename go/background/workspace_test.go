package background

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/shell"
)

type scopedSandbox struct{ root string }

func (s scopedSandbox) ForWorkspace(root string) (shell.Sandbox, error) {
	return scopedSandbox{root}, nil
}
func (s scopedSandbox) Argv(command string) ([]string, error) {
	return []string{"/bin/sh", "-c", "export BOUND_ROOT='" + s.root + "'; " + command}, nil
}
func TestRebindKeepsInflightScopeAndOriginalLedger(t *testing.T) {
	old, next := t.TempDir(), t.TempDir()
	old, _ = filepath.EvalSymlinks(old)
	next, _ = filepath.EvalSymlinks(next)
	m := newManager(t, Config{Shell: shell.Config{Workspace: old, Sandbox: scopedSandbox{}}})
	first, err := m.Run(context.Background(), Request{Command: "while [ ! -f release ]; do sleep 0.01; done; printf '%s\n%s' \"$PWD\" \"$BOUND_ROOT\""})
	if err != nil {
		t.Fatal(err)
	}
	awaitPID(t, m, first.ID)
	if err = m.Rebind(context.Background(), next); err != nil {
		t.Fatal(err)
	}
	if m.Workspace() != next {
		t.Fatal("background cwd did not move")
	}
	second := run(t, m, Request{Command: "printf '%s\n%s' \"$PWD\" \"$BOUND_ROOT\""})
	if record, _ := m.Get(second.ID); record.Result == nil || *record.Result != next+"\n"+next {
		t.Fatal("cwd and sandbox diverged", record)
	}
	if m.ledgerDir != filepath.Join(old, ".background") {
		t.Fatal("source ledger root moved")
	}
	if err = os.WriteFile(filepath.Join(old, "release"), []byte("ok"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = m.Wait(context.Background(), first.ID); err != nil {
		t.Fatal(err)
	}
	if record, _ := m.Get(first.ID); record.Result == nil || *record.Result != old+"\n"+old {
		t.Fatal("inflight executor binding moved", record)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err = m.Rebind(ctx, old); err != context.Canceled || m.Workspace() != next {
		t.Fatal("cancelled preparation changed binding")
	}
	invalid := filepath.Join(next, "file")
	if err = os.WriteFile(invalid, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = m.Rebind(context.Background(), invalid); err == nil || m.Workspace() != next {
		t.Fatal("failed preparation changed binding")
	}
}
func TestCloseKillsPipeHoldingDescendantsAndCanResumeWaiting(t *testing.T) {
	root := t.TempDir()
	m := newManager(t, Config{Shell: shell.Config{Workspace: root}})
	s, err := m.Run(context.Background(), Request{Command: "(sleep 0.4; printf escaped > late) & wait"})
	if err != nil {
		t.Fatal(err)
	}
	awaitPID(t, m, s.ID)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_ = m.Close(ctx)
	if err = m.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	time.Sleep(450 * time.Millisecond)
	if _, err = os.Stat(filepath.Join(root, "late")); !os.IsNotExist(err) {
		t.Fatal("background descendant survived close", err)
	}
	if !strings.HasPrefix(m.Check(s.ID), "[cancelled]") {
		t.Fatal("cancelled task status lost")
	}
}
