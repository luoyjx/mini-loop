// Package background owns shell commands that outlive their admitting turn.
package background

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/luoyjx/mini-loop/go/internal/pytext"
	"github.com/luoyjx/mini-loop/go/secrets"
	"github.com/luoyjx/mini-loop/go/shell"
)

type ID string
type Status string

const (
	Running                Status = "running"
	Completed              Status = "completed"
	Error                  Status = "error"
	Cancelled              Status = "cancelled"
	Orphaned               Status = "orphaned"
	DefaultResultsRetained        = 100
	MaxTaskListing                = 50
	MaxNotifications              = 50
	ShedResult                    = "[result released to stay within the retention bound; it was delivered as a task_notification when the task completed]"
)

type Request struct {
	Command string
	Timeout *time.Duration
}
type Started struct {
	ID       ID
	Command  string
	Recorded bool
}

func (start Started) Render() string {
	text := fmt.Sprintf("Started background task %s: %s", start.ID, prefix(start.Command, 80))
	if !start.Recorded {
		text += " (unrecorded: a restart will lose track of it)"
	}
	return text
}

type Record struct {
	ID      ID
	Status  Status
	Command string
	Result  *string
}

func (record Record) Clone() Record {
	if record.Result != nil {
		value := *record.Result
		record.Result = &value
	}
	return record
}

type Notification struct {
	ID     ID     `json:"bg_id"`
	Status Status `json:"status"`
	Result string `json:"result"`
}

// Shell credentials and confinement bind together. Zero timeout uses the source
// 300s default; nil retention uses 100 while an explicit zero releases all text.
type Config struct {
	Shell              shell.Config
	DefaultTimeout     time.Duration
	MaxResultsRetained *int
}
type Handle struct {
	id     ID
	cancel context.CancelFunc
	done   <-chan struct{}
}

func (handle Handle) ID() ID { return handle.id }
func (handle Handle) Wait(ctx context.Context) error {
	select {
	case <-handle.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type slot struct {
	record Record
	handle *Handle
}
type Manager struct {
	mu             sync.Mutex
	executor       *shell.Executor
	secrets        shell.SecretSource
	defaultTimeout time.Duration
	retention      int
	ledgerDir      string
	counter        big.Int
	tasks          map[ID]*slot
	order          []ID
	finished       []ID
	completed      []Notification
}

func New(config Config) (*Manager, error) {
	if config.DefaultTimeout == 0 {
		config.DefaultTimeout = 300 * time.Second
	}
	retention := DefaultResultsRetained
	if config.MaxResultsRetained != nil {
		retention = *config.MaxResultsRetained
	}
	if retention < 0 {
		return nil, errors.New("background retention cannot be negative")
	}
	if config.Shell.Secrets == nil {
		config.Shell.Secrets = secrets.Null{}
	}
	executor, err := shell.New(config.Shell)
	if err != nil {
		return nil, err
	}
	m := &Manager{executor: executor, secrets: config.Shell.Secrets, defaultTimeout: config.DefaultTimeout, retention: retention, ledgerDir: filepath.Join(executor.Workspace(), ".background"), tasks: make(map[ID]*slot)}
	m.adoptOrphans()
	return m, nil
}

func (manager *Manager) Workspace() string {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	return manager.executor.Workspace()
}

// Rebind prepares cwd and sandbox together. Existing tasks retain their admitted
// executor; the in-flight ledger stays under the original root, matching source.
func (manager *Manager) Rebind(ctx context.Context, root string) error {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	next, err := manager.executor.WithWorkspace(root)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	manager.executor = next
	return nil
}

// Run checks the caller only at admission. Once admitted, the task has its own
// context and survives cancellation of the caller's turn. CancelAll/Close own it.
func (manager *Manager) Run(ctx context.Context, request Request) (Started, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Started{}, err
	}
	if shell.LooksDangerous(request.Command) {
		return Started{}, errors.New("Dangerous command blocked")
	}
	manager.counter.Add(&manager.counter, big.NewInt(1))
	number := manager.counter.String()
	if len(number) < 4 {
		number = strings.Repeat("0", 4-len(number)) + number
	}
	id := ID("bg_" + number)
	ctxTask, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	handle := &Handle{id, cancel, done}
	manager.tasks[id] = &slot{Record{ID: id, Status: Running, Command: request.Command}, handle}
	manager.order = append(manager.order, id)
	recorded := manager.writeLedger(id, request.Command, nil)
	timeout := manager.defaultTimeout
	if request.Timeout != nil && *request.Timeout != 0 {
		timeout = *request.Timeout
	}
	executor := manager.executor
	go manager.execute(ctxTask, executor, id, request.Command, timeout, done, cancel)
	return Started{id, request.Command, recorded}, nil
}

func (manager *Manager) execute(ctx context.Context, executor *shell.Executor, id ID, command string, timeout time.Duration, done chan struct{}, cancel context.CancelFunc) {
	defer close(done)
	defer cancel()
	defer func() {
		if fault := recover(); fault != nil {
			manager.mu.Lock()
			defer manager.mu.Unlock()
			text := fmt.Sprintf("Error: background execution panicked (%T)", fault)
			manager.tasks[id].record.Status = Error
			manager.tasks[id].record.Result = &text
			manager.completed = append(manager.completed, Notification{id, Error, text})
			manager.settle(id)
		}
	}()
	result, err := executor.ExecuteBackground(ctx, shell.BackgroundCommand{Command: command, Timeout: timeout, Started: func(pid shell.ProcessID) {
		manager.mu.Lock()
		defer manager.mu.Unlock()
		manager.writeLedger(id, command, &pid)
	}})
	manager.mu.Lock()
	defer manager.mu.Unlock()
	text := result.Render()
	status := Completed
	if err != nil || ctx.Err() != nil {
		text = "Cancelled"
		status = Cancelled
	} else if result.Error != nil {
		status = Error
	}
	manager.tasks[id].record.Status = status
	manager.tasks[id].record.Result = &text
	manager.unledger(id)
	if status != Cancelled {
		manager.completed = append(manager.completed, Notification{id, status, text})
		manager.settle(id)
	}
}

func (manager *Manager) settle(id ID) {
	manager.unledger(id)
	manager.finished = append(manager.finished, id)
	for len(manager.finished) > manager.retention {
		old := manager.finished[0]
		manager.finished = manager.finished[1:]
		if task := manager.tasks[old]; task != nil && task.record.Result != nil {
			text := ShedResult
			task.record.Result = &text
			task.handle = nil
		}
	}
}
func (manager *Manager) Get(id ID) (Record, bool) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	task, ok := manager.tasks[id]
	if !ok {
		return Record{}, false
	}
	return task.record.Clone(), true
}
func (manager *Manager) Check(id ID) string {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if id != "" {
		task := manager.tasks[id]
		if task == nil {
			return "Unknown: " + string(id)
		}
		text := "(running)"
		if task.record.Result != nil && *task.record.Result != "" {
			text = *task.record.Result
		}
		return fmt.Sprintf("[%s] %s", task.record.Status, text)
	}
	if len(manager.order) == 0 {
		return "No background tasks."
	}
	first := max(0, len(manager.order)-MaxTaskListing)
	var lines []string
	if first > 0 {
		lines = append(lines, fmt.Sprintf("... (%d older task(s) not shown; check a bg_id directly)", first))
	}
	for _, id := range manager.order[first:] {
		r := manager.tasks[id].record
		lines = append(lines, fmt.Sprintf("%s: [%s] %s", id, r.Status, prefix(r.Command, 60)))
	}
	return strings.Join(lines, "\n")
}
func (manager *Manager) LiveCount() int {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	n := 0
	for _, task := range manager.tasks {
		if task.record.Status == Running {
			n++
		}
	}
	return n
}
func (manager *Manager) Drain() []Notification {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	done := manager.completed
	manager.completed = nil
	return append([]Notification{}, done...)
}
func (manager *Manager) CancelAll() []Handle {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	var handles []Handle
	for _, id := range manager.order {
		task := manager.tasks[id]
		if task.handle != nil {
			select {
			case <-task.handle.done:
				continue
			default:
			}
			task.handle.cancel()
			handles = append(handles, *task.handle)
		}
	}
	return handles
}

// Close cancels and joins the current handles. As in source, the caller owns
// admission quiescence; a later Run is allowed. Orphan PIDs are never signalled.
func (manager *Manager) Close(ctx context.Context) error {
	for _, handle := range manager.CancelAll() {
		if err := handle.Wait(ctx); err != nil {
			return err
		}
	}
	return nil
}
func (manager *Manager) Wait(ctx context.Context, id ID) error {
	manager.mu.Lock()
	task := manager.tasks[id]
	var handle *Handle
	if task != nil {
		handle = task.handle
	}
	manager.mu.Unlock()
	if task == nil {
		return fmt.Errorf("unknown background task %s", id)
	}
	if handle == nil {
		return nil
	}
	return handle.Wait(ctx)
}
func prefix(text string, n int) string { r := []rune(text); return string(r[:min(n, len(r))]) }
func IsSlowOperation(command string) bool {
	text := " " + pytext.Lower(command)
	for _, word := range []string{" install", " build", " test", " deploy", " compile", "docker build", "pip install", "npm install", "pnpm install", "cargo build", "pytest", " make"} {
		if strings.Contains(text, word) {
			return true
		}
	}
	return false
}
func ShouldRunBackground(command string, explicit bool) bool {
	return explicit || IsSlowOperation(command)
}
