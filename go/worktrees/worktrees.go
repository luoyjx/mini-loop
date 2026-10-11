// Package worktrees provides the operator-selected Git worktree lifecycle.
// It does not change an agent's bound workspace or activate model-facing tools.
package worktrees

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/luoyjx/mini-loop/go/tasks"
	"github.com/luoyjx/mini-loop/go/workspace"
)

type Name string
type EventType string

const (
	Created      EventType = "create"
	Removed      EventType = "remove"
	Kept         EventType = "keep"
	MaxGitOutput           = 5 * 1024 * 1024
)

var validName = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)
var ErrName = errors.New("worktree name must match [A-Za-z0-9._-]{1,64}")

type Config struct {
	Repository   string
	Base         *string
	BranchPrefix *string
}

// TaskBoard is a concrete task-binding seam, not an open service payload.
type TaskBoard interface {
	Load(tasks.ID) (*tasks.Task, error)
	BindWorktree(tasks.ID, string) (string, error)
}

type Event struct {
	Type      EventType `json:"type"`
	Worktree  Name      `json:"worktree"`
	TaskID    tasks.ID  `json:"task_id"`
	Timestamp float64   `json:"ts"`
}

type Changes struct{ Files, Commits int }

type Manager struct {
	repository, root, branchPrefix string
	mu                             sync.Mutex
	// Private seam for exercising Git's independent dirty-state guard.
	run func(context.Context, string, ...string) (bool, string)
	now func() time.Time
}

func New(config Config) (*Manager, error) {
	if config.Repository == "" {
		return nil, errors.New("worktree repository must be explicit")
	}
	repo, err := workspace.ResolvePath(config.Repository)
	if err != nil {
		return nil, err
	}
	base := ".worktrees"
	if config.Base != nil {
		base = *config.Base
	}
	root := filepath.Join(repo, base)
	if filepath.IsAbs(base) {
		root = base
	}
	prefix := "wt/"
	if config.BranchPrefix != nil {
		prefix = *config.BranchPrefix
	}
	return &Manager{repository: repo, root: root, branchPrefix: prefix, run: runGit, now: time.Now}, nil
}

func ValidateName(name Name) error {
	if name == "." || name == ".." || !validName.MatchString(string(name)) {
		return ErrName
	}
	return nil
}
func (m *Manager) Repository() string { return m.repository }
func (m *Manager) Root() string       { return m.root }
func (m *Manager) PathFor(name Name) (string, error) {
	if err := ValidateName(name); err != nil {
		return "", err
	}
	return filepath.Join(m.root, string(name)), nil
}
func (m *Manager) IsRepository(ctx context.Context) bool {
	ok, _ := m.run(ctx, m.repository, "rev-parse", "--is-inside-work-tree")
	return ok
}

func exists(path string) bool { _, err := os.Stat(path); return err == nil }

func (m *Manager) log(kind EventType, name Name, taskID tasks.ID) error {
	if err := os.MkdirAll(m.root, 0755); err != nil {
		return err
	}
	data, err := json.Marshal(Event{kind, name, taskID, float64(m.now().UnixNano()) / 1e9})
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(m.root, "events.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		return err
	}
	_, err = f.Write(append(data, '\n'))
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func (m *Manager) Create(ctx context.Context, name Name, taskID tasks.ID, board TaskBoard) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	path, err := m.PathFor(name)
	if err != nil {
		return "Error: " + err.Error(), nil
	}
	if !m.IsRepository(ctx) {
		return fmt.Sprintf("Error: %s is not a git repository", m.repository), ctx.Err()
	}
	if taskID != "" {
		if board == nil {
			return fmt.Sprintf("Error: task %s not found", taskID), nil
		}
		task, err := board.Load(taskID)
		if err != nil {
			return "", err
		}
		if task == nil {
			return fmt.Sprintf("Error: task %s not found", taskID), nil
		}
	}
	if exists(path) {
		return fmt.Sprintf("Error: worktree %s already exists", name), nil
	}
	if err := os.MkdirAll(m.root, 0755); err != nil {
		return "", err
	}
	ok, output := m.run(ctx, m.repository, "worktree", "add", path, "-b", m.branchPrefix+string(name), "HEAD")
	if !ok {
		return "Error: " + output, ctx.Err()
	}
	if taskID != "" {
		// Like source, a failure after Git creation is not rolled back: it may
		// already contain work. The caller must inspect the worktree.
		if _, err := board.BindWorktree(taskID, string(name)); err != nil {
			return "", err
		}
	}
	if err := m.log(Created, name, taskID); err != nil {
		return "", err
	}
	return fmt.Sprintf("Worktree '%s' created at %s", name, path), nil
}

func (m *Manager) changes(ctx context.Context, name Name) Changes {
	unknown := Changes{-1, -1}
	path, err := m.PathFor(name)
	if err != nil {
		return unknown
	}
	ok, status := m.run(ctx, path, "status", "--porcelain")
	if !ok {
		return unknown
	}
	files := 0
	if status != "" {
		files = len(strings.Split(status, "\n"))
	}
	ok, head := m.run(ctx, m.repository, "rev-parse", "HEAD")
	if !ok {
		return unknown
	}
	ok, ahead := m.run(ctx, path, "rev-list", "--count", head+"..HEAD")
	if !ok || ahead == "" {
		return unknown
	}
	for _, char := range ahead {
		if char < '0' || char > '9' {
			return unknown
		}
	}
	commits, err := strconv.Atoi(ahead)
	if err != nil {
		return unknown
	}
	return Changes{files, commits}
}

func (m *Manager) Changes(ctx context.Context, name Name) Changes {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.changes(ctx, name)
}

func (m *Manager) Remove(ctx context.Context, name Name, discardChanges bool) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	path, err := m.PathFor(name)
	if err != nil {
		return "Error: " + err.Error(), nil
	}
	if !exists(path) {
		return fmt.Sprintf("No worktree %s", name), nil
	}
	changes := m.changes(ctx, name)
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !discardChanges && changes.Files < 0 {
		return fmt.Sprintf("Refusing: could not verify worktree %s status; keep it or pass discard_changes=True", name), nil
	}
	if !discardChanges && (changes.Files != 0 || changes.Commits != 0) {
		return fmt.Sprintf("Refusing: worktree %s has %d changed file(s) and %d commit(s); keep it or pass discard_changes=True", name, changes.Files, changes.Commits), nil
	}
	args := []string{"worktree", "remove", path}
	branchFlag := "-d"
	if discardChanges {
		args = append(args, "--force")
		branchFlag = "-D"
	}
	ok, output := m.run(ctx, m.repository, args...)
	if !ok {
		return "Error: " + output, ctx.Err()
	}
	// Source ignores branch cleanup failure; never silently promote -d to -D.
	m.run(ctx, m.repository, "branch", branchFlag, m.branchPrefix+string(name))
	if err := m.log(Removed, name, ""); err != nil {
		return "", err
	}
	return fmt.Sprintf("Removed worktree %s", name), nil
}

func (m *Manager) Keep(ctx context.Context, name Name) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	path, err := m.PathFor(name)
	if err != nil {
		return "Error: " + err.Error(), nil
	}
	if !exists(path) {
		return fmt.Sprintf("No worktree %s", name), nil
	}
	if err := m.log(Kept, name, ""); err != nil {
		return "", err
	}
	return fmt.Sprintf("Worktree '%s' kept for review (branch: %s%s)", name, m.branchPrefix, name), nil
}

func (m *Manager) List(ctx context.Context) string {
	ok, output := m.run(ctx, m.repository, "worktree", "list")
	if !ok {
		return "(not a git repo)"
	}
	return output
}

// WorkspaceFor provisions source-compatible workspaces for an embedding caller.
// Git failures degrade to a plain directory, which is NOT an isolated branch.
// Callers select retention/removal. SessionManager's source-compatible factory
// adapter treats this path as scratch; its directory cleanup does not perform
// Git's dirty checks, registration cleanup or branch deletion. This helper alone
// does not rebind a running agent or install a manager hook.
func (m *Manager) WorkspaceFor(ctx context.Context, session string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	name := session
	if ValidateName(Name(name)) != nil {
		var chars []rune
		for _, ch := range name {
			if ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '.' || ch == '_' || ch == '-' {
				chars = append(chars, ch)
			} else {
				chars = append(chars, '_')
			}
			if len(chars) == 64 {
				break
			}
		}
		name = strings.Trim(string(chars), ".")
		if name == "" {
			name = "session"
		}
	}
	path := filepath.Join(m.root, name)
	if exists(path) {
		return path, nil
	}
	if m.IsRepository(ctx) {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return "", err
		}
		ok, _ := m.run(ctx, m.repository, "worktree", "add", path, "-b", m.branchPrefix+name, "HEAD")
		if ok {
			return path, nil
		}
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return path, os.MkdirAll(path, 0755)
}

type boundedOutput struct {
	data []byte
	over bool
}

func (b *boundedOutput) Write(data []byte) (int, error) {
	room := MaxGitOutput - len(b.data)
	if len(data) > room {
		b.over = true
		b.data = append(b.data, data[:room]...)
		return len(data), io.ErrShortBuffer
	}
	b.data = append(b.data, data...)
	return len(data), nil
}

func runGit(ctx context.Context, root string, args ...string) (bool, string) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "git", args...)
	command.Dir = root
	// Separate captures preserve source stdout + stderr ordering, not arrival order.
	var stdout, stderr boundedOutput
	command.Stdout, command.Stderr = &stdout, &stderr
	// Bound cleanup if a hook leaves inherited pipes open.
	command.WaitDelay = time.Second
	err := command.Run()
	if stdout.over || stderr.over {
		return false, "git output exceeded capture limit"
	}
	output := strings.TrimSpace(strings.ToValidUTF8(string(stdout.data)+string(stderr.data), "�"))
	if err != nil && output == "" {
		output = err.Error()
	}
	return err == nil, output
}
