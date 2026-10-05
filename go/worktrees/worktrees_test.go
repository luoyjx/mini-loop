package worktrees

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/tasks"
)

func git(t *testing.T, root string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = root
	data, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v / %s", args, err, data)
	}
	return strings.TrimSpace(string(data))
}

func repo(t *testing.T, unborn bool) string {
	t.Helper()
	root := t.TempDir()
	git(t, root, "init", "-b", "main")
	for key, value := range map[string]string{"user.name": "Go parity", "user.email": "parity@example.invalid", "commit.gpgsign": "false", "core.hooksPath": "/dev/null", "core.autocrlf": "false"} {
		git(t, root, "config", key, value)
	}
	for path, text := range map[string]string{".gitignore": ".worktrees/\ntrees/\n.tasks/\n", "tracked.txt": "base\n"} {
		if err := os.WriteFile(filepath.Join(root, path), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if !unborn {
		git(t, root, "add", ".gitignore", "tracked.txt")
		git(t, root, "commit", "-m", "base")
	}
	return root
}

func events(t *testing.T, manager *Manager) []Event {
	t.Helper()
	result := []Event{}
	data, err := os.ReadFile(filepath.Join(manager.root, "events.jsonl"))
	if errors.Is(err, os.ErrNotExist) {
		return result
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var event Event
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatal(err)
		}
		if event.Timestamp <= 0 {
			t.Fatal("missing audit timestamp")
		}
		event.Timestamp = 0
		result = append(result, event)
	}
	return result
}

func TestActualPythonLifecycleFactoryAndTaskBinding(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-worktrees.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name         string
			Base, Prefix *string
			Git, Unborn  bool
			Steps        []struct {
				Input struct {
					Op, Name, File, Text string
					TaskID               tasks.ID `json:"task_id"`
					Discard              bool
				}
				Value        json.RawMessage
				Events       []Event
				PathExists   bool `json:"path_exists"`
				BranchExists bool `json:"branch_exists"`
				Binding      *string
			}
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 9 {
		t.Fatal("incomplete source worktree fixture")
	}
	ctx := context.Background()
	head := regexp.MustCompile(`(?m)(\s)[0-9a-f]{7,40}(\s)`)
	for _, scenario := range fixture.Cases {
		t.Run(scenario.Name, func(t *testing.T) {
			root := t.TempDir()
			if scenario.Git {
				root = repo(t, scenario.Unborn)
			}
			manager, err := New(Config{Repository: root, Base: scenario.Base, BranchPrefix: scenario.Prefix})
			if err != nil {
				t.Fatal(err)
			}
			board, err := tasks.New(tasks.Config{Workspace: root})
			if err != nil {
				t.Fatal(err)
			}
			if err := board.Save(tasks.Task{ID: "task_link", Subject: "linked task", Status: tasks.Pending, BlockedBy: []tasks.ID{}}); err != nil {
				t.Fatal(err)
			}
			normalize := func(text string) string {
				return head.ReplaceAllString(strings.ReplaceAll(text, root, "<REPO>"), "${1}<HEAD>${2}")
			}
			for index, step := range scenario.Steps {
				input := step.Input
				name := Name(input.Name)
				// Preserve lexical invalid-name probes: Python Path does not clean
				// away a nonexistent intermediate directory before exists().
				path := manager.root + string(os.PathSeparator) + input.Name
				var value interface{} // Source fixture serialization only.
				var err error
				switch input.Op {
				case "create":
					value, err = manager.Create(ctx, name, input.TaskID, board)
				case "remove":
					value, err = manager.Remove(ctx, name, input.Discard)
				case "keep":
					value, err = manager.Keep(ctx, name)
				case "list":
					lines := []string{}
					for _, line := range strings.Split(manager.List(ctx), "\n") {
						lines = append(lines, strings.Join(strings.Fields(normalize(line)), " "))
					}
					value = lines
				case "changes":
					changes := manager.Changes(ctx, name)
					value = []int{changes.Files, changes.Commits}
				case "factory":
					value, err = manager.WorkspaceFor(ctx, input.Name)
				case "mkdir":
					err = os.MkdirAll(path, 0755)
				case "write":
					err = os.WriteFile(filepath.Join(path, input.File), []byte(input.Text), 0600)
				case "commit":
					git(t, path, "add", ".")
					git(t, path, "commit", "-m", "work")
				case "branch":
					git(t, root, "branch", manager.branchPrefix+input.Name)
				default:
					t.Fatalf("unknown fixture operation: %s", input.Op)
				}
				if err != nil {
					t.Fatalf("step %d %s: %v", index, input.Op, err)
				}
				if text, ok := value.(string); ok {
					value = normalize(text)
				}
				actual, err := json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				var got, want interface{}
				if err := json.Unmarshal(actual, &got); err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(step.Value, &want); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("step %d %s: %s / %s", index, input.Op, actual, step.Value)
				}
				if actual := events(t, manager); !reflect.DeepEqual(actual, step.Events) {
					t.Fatalf("step %d audit: %+v / %+v", index, actual, step.Events)
				}
				if exists(path) != step.PathExists {
					t.Fatalf("step %d path existence differs", index)
				}
				branch, _ := runGit(ctx, root, "rev-parse", "--verify", "refs/heads/"+manager.branchPrefix+input.Name)
				if branch != step.BranchExists {
					t.Fatalf("step %d branch existence differs", index)
				}
				linked, err := board.Load("task_link")
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(linked.Worktree, step.Binding) {
					t.Fatalf("step %d task binding differs", index)
				}
			}
		})
	}
}

func TestGitRetainsIndependentDirtyGuard(t *testing.T) {
	root := repo(t, false)
	manager, _ := New(Config{Repository: root})
	ctx := context.Background()
	if text, err := manager.Create(ctx, "race", "", nil); err != nil || !strings.HasPrefix(text, "Worktree") {
		t.Fatalf("create: %s / %v", text, err)
	}
	path, _ := manager.PathFor("race")
	// The service observed clean; a writer lands before the Git removal. This
	// specifically proves Git's second guard, rather than retesting our first.
	manager.run = func(ctx context.Context, dir string, args ...string) (bool, string) {
		if len(args) > 1 && args[0] == "worktree" && args[1] == "remove" {
			if err := os.WriteFile(filepath.Join(path, "arrived.txt"), []byte("preserve"), 0600); err != nil {
				t.Fatal(err)
			}
			for _, arg := range args {
				if arg == "--force" {
					t.Fatal("unrequested force")
				}
			}
		}
		return runGit(ctx, dir, args...)
	}
	text, err := manager.Remove(ctx, "race", false)
	if err != nil || !strings.HasPrefix(text, "Error:") || !exists(filepath.Join(path, "arrived.txt")) {
		t.Fatalf("dirty work lost: %s / %v", text, err)
	}
	if len(events(t, manager)) != 1 {
		t.Fatal("failed remove was audited as success")
	}
	manager.run = runGit
	if text, err := manager.Remove(ctx, "race", true); err != nil || text != "Removed worktree race" {
		t.Fatalf("explicit discard: %s / %v", text, err)
	}
}

func TestFailureAfterGitCreationIsNotRolledBack(t *testing.T) {
	root := repo(t, false)
	manager, _ := New(Config{Repository: root})
	board := failedBoard{}
	text, err := manager.Create(context.Background(), "bound", "task", board)
	path, _ := manager.PathFor("bound")
	if !errors.Is(err, errBinding) || text != "" || !exists(path) || len(events(t, manager)) != 0 {
		t.Fatalf("partial creation: %s / %v", text, err)
	}
	// A log failure likewise cannot authorize discarding successfully created work.
	if err := os.Mkdir(filepath.Join(manager.root, "events.jsonl"), 0700); err != nil {
		t.Fatal(err)
	}
	_, err = manager.Create(context.Background(), "audit", "", nil)
	path, _ = manager.PathFor("audit")
	if err == nil || !exists(path) {
		t.Fatal("audit failure erased created worktree")
	}
}

func TestGitRetainsIndependentUnmergedBranchGuard(t *testing.T) {
	root := repo(t, false)
	manager, _ := New(Config{Repository: root})
	ctx := context.Background()
	if text, err := manager.Create(ctx, "ahead", "", nil); err != nil || !strings.HasPrefix(text, "Worktree '") {
		t.Fatalf("create: %s / %v", text, err)
	}
	path, _ := manager.PathFor("ahead")
	if err := os.WriteFile(filepath.Join(path, "new.txt"), []byte("unmerged"), 0600); err != nil {
		t.Fatal(err)
	}
	git(t, path, "add", "new.txt")
	git(t, path, "commit", "-m", "unmerged")
	manager.run = func(ctx context.Context, dir string, args ...string) (bool, string) {
		// Model an obsolete preflight result; Git still owns the final ref check.
		if len(args) > 0 && args[0] == "rev-list" {
			return true, "0"
		}
		if len(args) > 1 && args[0] == "branch" && args[1] != "-d" {
			t.Fatal("unrequested forced branch deletion")
		}
		return runGit(ctx, dir, args...)
	}
	text, err := manager.Remove(ctx, "ahead", false)
	if err != nil || text != "Removed worktree ahead" || exists(path) {
		t.Fatalf("clean directory removal: %s / %v", text, err)
	}
	if ok, _ := runGit(ctx, root, "rev-parse", "--verify", "refs/heads/wt/ahead"); !ok {
		t.Fatal("unmerged commit branch was erased")
	}
	if len(events(t, manager)) != 2 {
		t.Fatal("remove audit missing")
	}
}

var errBinding = errors.New("binding failed")

type failedBoard struct{}

func (failedBoard) Load(tasks.ID) (*tasks.Task, error)            { return &tasks.Task{ID: "task"}, nil }
func (failedBoard) BindWorktree(tasks.ID, string) (string, error) { return "", errBinding }

func TestSerializedDuplicateCreationAndCancellation(t *testing.T) {
	manager, _ := New(Config{Repository: repo(t, false)})
	var wg sync.WaitGroup
	results := make(chan string, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			text, err := manager.Create(context.Background(), "same", "", nil)
			if err != nil {
				text = err.Error()
			}
			results <- text
		}()
	}
	wg.Wait()
	close(results)
	created, refused := 0, 0
	for result := range results {
		if strings.HasPrefix(result, "Worktree '") {
			created++
		}
		if result == "Error: worktree same already exists" {
			refused++
		}
	}
	if created != 1 || refused != 1 || len(events(t, manager)) != 1 {
		t.Fatalf("duplicate: %d / %d", created, refused)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := manager.Create(ctx, "cancelled", "", nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := manager.WorkspaceFor(ctx, "cancelled"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if exists(filepath.Join(manager.root, "cancelled")) {
		t.Fatal("cancelled call created work")
	}
}

func TestBoundedGitOutputAndContextOwnedWait(t *testing.T) {
	var output boundedOutput
	_, err := output.Write(make([]byte, MaxGitOutput+1))
	if err == nil || !output.over || len(output.data) != MaxGitOutput {
		t.Fatal("unbounded Git output")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	ok, _ := runGit(ctx, t.TempDir(), "status")
	if ok {
		t.Fatal("cancelled git command succeeded")
	}
	manager, err := New(Config{})
	if manager != nil || err == nil {
		t.Fatal("implicit repository accepted")
	}
	for _, name := range []Name{".", "..", Name(strings.Repeat("a", 65)), "bad/name"} {
		if err := ValidateName(name); !errors.Is(err, ErrName) {
			t.Fatal(fmt.Sprintf("accepted %q", name))
		}
	}
}
