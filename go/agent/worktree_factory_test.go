package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/worktrees"
)

type managedWorktreeState struct {
	Directory, Linked, Registered, Branch bool
	Proof                                 *string
}
type managedWorktreeCase struct {
	Name, Kind, Action, Workspace string
	Dirty, Bound                  bool
	Owner                         OwnerID
	Initial, Before, After        managedWorktreeState
	Output                        *string
	Removed                       *bool
	CleanupErrors                 []CleanupError `json:"cleanup_errors"`
}

func worktreeGit(t *testing.T, repo string, args ...string) (bool, string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = repo
	output, err := cmd.CombinedOutput()
	return err == nil, string(output)
}

func managedWorktreeProbe(t *testing.T, repo string, session *ManagedSession) managedWorktreeState {
	t.Helper()
	path := session.Info().Workspace
	dir, err := os.Stat(path)
	state := managedWorktreeState{Directory: err == nil && dir.IsDir()}
	gitFile, err := os.Stat(filepath.Join(path, ".git"))
	state.Linked = err == nil && !gitFile.IsDir()
	ok, listing := worktreeGit(t, repo, "worktree", "list", "--porcelain")
	state.Registered = ok && strings.Contains(listing, "worktree "+path+"\n")
	state.Branch, _ = worktreeGit(t, repo, "show-ref", "--verify", "--quiet", "refs/heads/wt/"+string(session.ID()))
	if data, err := os.ReadFile(filepath.Join(path, "proof.txt")); err == nil {
		text := string(data)
		state.Proof = &text
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return state
}

func TestManagedWorktreeFactoryMatchesActualSourceCleanupAndFallbacks(t *testing.T) {
	raw, err := os.ReadFile("../testdata/python-managed-worktrees.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct{ Cases []managedWorktreeCase }
	if err = json.Unmarshal(raw, &fixture); err != nil || len(fixture.Cases) != 7 {
		t.Fatal("source factory corpus missing", err)
	}
	for _, c := range fixture.Cases {
		t.Run(c.Name, func(t *testing.T) {
			base := t.TempDir()
			repo := filepath.Join(base, "repo")
			if err := os.Mkdir(repo, 0700); err != nil {
				t.Fatal(err)
			}
			git := func(args ...string) {
				t.Helper()
				if ok, output := worktreeGit(t, repo, args...); !ok {
					t.Fatalf("git %v: %s", args, output)
				}
			}
			if c.Kind != "nonrepo" {
				git("init", "-b", "main")
				for key, value := range map[string]string{"user.name": "Go parity", "user.email": "parity@example.invalid", "commit.gpgsign": "false", "core.hooksPath": "/dev/null", "core.autocrlf": "false"} {
					git("config", key, value)
				}
				if c.Kind != "unborn" {
					if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte(".worktrees/\n"), 0600); err != nil {
						t.Fatal(err)
					}
					git("add", ".gitignore")
					git("commit", "-m", "base")
				}
			}
			service, err := worktrees.New(worktrees.Config{Repository: repo})
			if err != nil {
				t.Fatal(err)
			}
			factory, err := NewWorktreeWorkspaceFactory(service)
			if err != nil {
				t.Fatal(err)
			}
			config := managerTestConfig(filepath.Join(base, "ws"), resourceProvider{[]protocol.Block{protocol.NewBashUse("write", "printf proof > proof.txt")}})
			config.WorkspaceFactory = factory
			if c.Kind == "conflict" {
				config.WorkspaceFactory = workspaceFactoryFunc(func(ctx context.Context, id SessionID) (string, error) {
					git("branch", "wt/"+string(id))
					return factory.WorkspaceFor(ctx, id)
				})
			}
			manager := makeManager(t, config)
			session := createManaged(t, manager, CreateSessionRequest{Owner: c.Owner})
			info := session.Info()
			path := strings.ReplaceAll(strings.ReplaceAll(info.Workspace, service.Repository(), "<REPO>"), string(session.ID()), "<SESSION>")
			if path != c.Workspace || info.WorkspaceBound != c.Bound || session.Owner() != c.Owner || len(session.core.gate.catalog.Names()) != 10 {
				t.Fatal("factory changed admission, binding or default tools", info)
			}
			if state := managedWorktreeProbe(t, repo, session); !reflect.DeepEqual(state, c.Initial) {
				t.Fatal("allocation differs", state, c.Initial)
			}
			if c.Dirty {
				output, err := session.Run(context.Background(), "write")
				if err != nil || c.Output == nil || output != *c.Output {
					t.Fatal("real tool write differs", output, err)
				}
			}
			if state := managedWorktreeProbe(t, repo, session); !reflect.DeepEqual(state, c.Before) {
				t.Fatal("before cleanup differs", state, c.Before)
			}
			if _, err := manager.Get("foreign", session.ID()); err != ErrSessionNotFound {
				t.Fatal("factory bypassed owner scope")
			}
			if c.Action == "stop" {
				if err := manager.Stop(context.Background()); err != nil {
					t.Fatal(err)
				}
			} else {
				removed, err := manager.Delete(c.Owner, session.ID(), DeleteSessionOptions{PreserveWorkspace: c.Action == "preserve"})
				if err != nil || c.Removed == nil || removed != *c.Removed {
					t.Fatal("delete differs", err)
				}
				if err := manager.WaitCleanup(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			if state := managedWorktreeProbe(t, repo, session); !reflect.DeepEqual(state, c.After) {
				t.Fatal("source cleanup differs", state, c.After)
			}
			if len(manager.CleanupErrors()) != len(c.CleanupErrors) {
				t.Fatal("cleanup error projection differs")
			}
		})
	}
}

func TestWorktreeFactoryRequiresServiceAndPropagatesCancellation(t *testing.T) {
	if _, err := NewWorktreeWorkspaceFactory(nil); err == nil {
		t.Fatal("implicit service accepted")
	}
	var zero WorktreeWorkspaceFactory
	if _, err := zero.WorkspaceFor(context.Background(), "session"); err == nil {
		t.Fatal("uninitialized factory accepted")
	}
	repo := worktreeRepo(t)
	service, _ := worktrees.New(worktrees.Config{Repository: repo})
	factory, _ := NewWorktreeWorkspaceFactory(service)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := factory.WorkspaceFor(ctx, "cancelled"); err != context.Canceled {
		t.Fatal("cancelled allocation proceeded", err)
	}
	path, _ := service.PathFor("cancelled")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("cancelled factory created work")
	}
}

func TestWorktreeFactorySharedScratchRetainsWorkUntilLastDelete(t *testing.T) {
	repo := worktreeRepo(t)
	service, err := worktrees.New(worktrees.Config{Repository: repo})
	if err != nil {
		t.Fatal(err)
	}
	factory, err := NewWorktreeWorkspaceFactory(service)
	if err != nil {
		t.Fatal(err)
	}
	config := managerTestConfig(t.TempDir(), resourceProvider{[]protocol.Block{protocol.NewBashUse("write", "printf proof > proof.txt")}})
	var sharedID SessionID
	config.WorkspaceFactory = workspaceFactoryFunc(func(ctx context.Context, id SessionID) (string, error) {
		if sharedID == "" {
			sharedID = id
		}
		return factory.WorkspaceFor(ctx, sharedID)
	})
	manager := makeManager(t, config)
	first := createManaged(t, manager, CreateSessionRequest{Owner: "alice"})
	second := createManaged(t, manager, CreateSessionRequest{Owner: "alice"})
	if first.Info().Workspace != second.Info().Workspace {
		t.Fatal("trusted shared factory did not reuse existing worktree")
	}
	if _, err := first.Run(context.Background(), "write"); err != nil {
		t.Fatal(err)
	}
	before := managedWorktreeProbe(t, repo, first)
	if !before.Linked || before.Proof == nil {
		t.Fatal("real shared work was not created", before)
	}
	for i, session := range []*ManagedSession{first, second} {
		if removed, err := manager.Delete("alice", session.ID(), DeleteSessionOptions{}); err != nil || !removed {
			t.Fatal("delete failed", err)
		}
		if err := manager.WaitCleanup(context.Background()); err != nil {
			t.Fatal(err)
		}
		after := managedWorktreeProbe(t, repo, first)
		if i == 0 && !reflect.DeepEqual(before, after) {
			t.Fatal("surviving holder lost work", after)
		}
		if i == 1 && (after.Directory || !after.Registered || !after.Branch) {
			t.Fatal("last scratch cleanup must remove only directory", after)
		}
	}
}

func TestWorktreeFactoryFailedAdmissionUsesExistingScratchCleanup(t *testing.T) {
	repo := worktreeRepo(t)
	service, err := worktrees.New(worktrees.Config{Repository: repo})
	if err != nil {
		t.Fatal(err)
	}
	factory, err := NewWorktreeWorkspaceFactory(service)
	if err != nil {
		t.Fatal(err)
	}
	config := managerTestConfig(t.TempDir(), resourceProvider{})
	config.WorkspaceFactory = factory
	fault := errors.New("executor construction failed")
	var binding SessionBinding
	config.Services.BashFactory = bashFactoryFunc(func(_ context.Context, b SessionBinding) (BashExecutor, error) {
		binding = b
		return nil, fault
	})
	manager := makeManager(t, config)
	if session, err := manager.Create(context.Background(), CreateSessionRequest{Owner: "alice"}); session != nil || !errors.Is(err, fault) {
		t.Fatal("failed construction published a session", session, err)
	}
	if binding.ID == "" || binding.Workspace == "" {
		t.Fatal("factory was not exercised")
	}
	if _, err := os.Stat(binding.Workspace); !os.IsNotExist(err) {
		t.Fatal("failed admission retained allocated scratch", err)
	}
	if ok, listing := worktreeGit(t, repo, "worktree", "list", "--porcelain"); !ok || !strings.Contains(listing, "worktree "+binding.Workspace+"\n") {
		t.Fatal("scratch cleanup unexpectedly removed Git registration", listing)
	}
	if ok, output := worktreeGit(t, repo, "show-ref", "--verify", "--quiet", "refs/heads/wt/"+string(binding.ID)); !ok {
		t.Fatal("scratch cleanup unexpectedly removed branch", output)
	}
}
