package agent

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/worktrees"
)

func TestOperatorWorktreeBindsRealToolsAndSurvivesSessionDeletion(t *testing.T) {
	root := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		command := exec.Command("git", args...)
		command.Dir = root
		data, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v / %s", args, err, data)
		}
		return string(data)
	}
	git("init", "-b", "main")
	for key, value := range map[string]string{"user.name": "Go parity", "user.email": "parity@example.invalid", "commit.gpgsign": "false", "core.hooksPath": "/dev/null"} {
		git("config", key, value)
	}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(".worktrees/\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", ".gitignore")
	git("commit", "-m", "base")
	worktree, err := worktrees.New(worktrees.Config{Repository: root})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if text, err := worktree.Create(ctx, "bound", "", nil); err != nil || !strings.HasPrefix(text, "Worktree '") {
		t.Fatalf("create: %s / %v", text, err)
	}
	path, _ := worktree.PathFor("bound")
	provider := &fakeSequenceProvider{replies: []protocol.ModelReply{
		fakeReply([]protocol.Block{protocol.NewBashUse("write", "printf 'bound\\n' > proof.txt")}, protocol.StopToolUse),
		fakeReply([]protocol.Block{protocol.NewTextBlock("done")}, protocol.StopEndTurn),
	}}
	config := managerTestConfig(t.TempDir(), provider)
	config.BindableRoots = []string{worktree.Root()}
	manager := makeManager(t, config)
	session := createManaged(t, manager, CreateSessionRequest{Owner: "alice", Workspace: &path})
	if _, err := manager.Get("bob", session.ID()); err != ErrSessionNotFound {
		t.Fatalf("foreign owner: %v", err)
	}
	if text, err := session.Run(ctx, "write in bound worktree"); err != nil || text != "done" {
		t.Fatalf("run: %s / %v", text, err)
	}
	if data, err := os.ReadFile(filepath.Join(path, "proof.txt")); err != nil || string(data) != "bound\n" {
		t.Fatalf("worktree write: %s / %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(root, "proof.txt")); !os.IsNotExist(err) {
		t.Fatal("write landed in main checkout")
	}
	if ok, err := manager.Delete("alice", session.ID(), DeleteSessionOptions{}); !ok || err != nil {
		t.Fatalf("delete: %t / %v", ok, err)
	}
	if err := manager.WaitCleanup(ctx); err != nil {
		t.Fatal(err)
	}
	if err := manager.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(path, "proof.txt")); err != nil || string(data) != "bound\n" {
		t.Fatal("bound work erased by manager cleanup")
	}
	if listing := worktree.List(ctx); !strings.Contains(listing, "[wt/bound]") {
		t.Fatal("worktree registration lost")
	}
	if text, err := worktree.Remove(ctx, "bound", false); err != nil || !strings.HasPrefix(text, "Refusing:") {
		t.Fatalf("dirty bound work: %s / %v", text, err)
	}
	if text, err := worktree.Remove(ctx, "bound", true); err != nil || text != "Removed worktree bound" {
		t.Fatalf("fixture discard: %s / %v", text, err)
	}
}
