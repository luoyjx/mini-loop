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

func TestScopedTasksKeepSeparateIDsQueuesAndRestartEvidence(t *testing.T) {
	root := t.TempDir()
	executor, err := shell.New(shell.Config{Workspace: root})
	if err != nil {
		t.Fatal(err)
	}
	parent, err := NewWithExecutor(executor)
	if err != nil {
		t.Fatal(err)
	}
	defer parent.Close(context.Background())
	p, err := parent.Run(context.Background(), Request{Command: "sleep 30"})
	if err != nil {
		t.Fatal(err)
	}
	awaitPID(t, parent, p.ID)
	child, err := NewScopedWithExecutor(executor, Scope(strings.Repeat("a", 64)))
	if err != nil {
		t.Fatal(err)
	}
	defer child.Close(context.Background())
	c, err := child.Run(context.Background(), Request{Command: "sleep 30"})
	if err != nil {
		t.Fatal(err)
	}
	awaitPID(t, child, c.ID)
	if c.ID == p.ID || c.ID != ID("bg_"+strings.Repeat("a", 64)+"_0001") {
		t.Fatal(p.ID, c.ID)
	}
	for _, id := range []ID{p.ID, c.ID} {
		if _, err := os.Stat(filepath.Join(root, ".background", string(id)+".json")); err != nil {
			t.Fatal("ledger collision", id, err)
		}
	}
	if _, ok := child.Get(p.ID); ok {
		t.Fatal("child adopted live parent")
	}
	if _, ok := parent.Get(c.ID); ok {
		t.Fatal("parent borrowed child state")
	}
	if len(parent.Drain()) != 0 || len(child.Drain()) != 0 {
		t.Fatal("live task reported as orphan")
	}
	// Preserve the exact native ledger bytes to model a crash leaving evidence.
	path := filepath.Join(root, ".background", string(c.ID)+".json")
	bytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := parent.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := child.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, bytes, 0600); err != nil {
		t.Fatal(err)
	}
	restarted, err := NewWithExecutor(executor)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close(context.Background())
	record, ok := restarted.Get(c.ID)
	if !ok || record.Status != Orphaned || record.Result == nil || !strings.Contains(*record.Result, "orphaned by a restart") {
		t.Fatal(record, ok)
	}
	batch := restarted.DrainBatch()
	if len(batch.Notifications) != 1 || batch.Notifications[0].ID != c.ID {
		t.Fatal(batch)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("adopted scoped evidence retained", err)
	}
	zero := time.Duration(-1)
	next := run(t, restarted, Request{Command: "sleep 30", Timeout: &zero})
	if next.ID != "bg_0001" {
		t.Fatal("qualified orphan corrupted root counter", next.ID)
	}
}

func TestScopedConstructorRefusesUnboundAndPathlikeIdentity(t *testing.T) {
	executor, err := shell.New(shell.Config{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	for _, scope := range []Scope{"", "../escape", Scope(strings.Repeat("A", 64)), Scope(strings.Repeat("a", 63)), Scope(strings.Repeat("a", 63) + "/")} {
		if _, err := NewScopedWithExecutor(executor, scope); err == nil {
			t.Fatal("invalid scope admitted", scope)
		}
	}
	if _, err := NewScopedWithExecutor(nil, Scope(strings.Repeat("a", 64))); err == nil {
		t.Fatal("nil executor admitted")
	}
}
