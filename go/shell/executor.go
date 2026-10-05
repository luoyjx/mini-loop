package shell

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/luoyjx/mini-loop/go/internal/pytext"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/secrets"
	"github.com/luoyjx/mini-loop/go/spill"
	"github.com/luoyjx/mini-loop/go/workspace"
)

// SecretSource supplies both narrow environment injection and whole-value masks.
type SecretSource interface {
	ScrubEnvironment(secrets.Environment) secrets.Environment
	EnvForCommand(string) secrets.Environment
	MaskText(string) string
}

// Sandbox owns argv and must return a workspace-bound instance. No sandbox is
// configured by default. Seatbelt/container implementations remain separate work.
type Sandbox interface {
	ForWorkspace(string) (Sandbox, error)
	Argv(string) ([]string, error)
}
type Config struct {
	Spill        spill.Store
	Workspace    string
	Timeout      time.Duration
	CaptureLimit int
	Secrets      SecretSource
	Sandbox      Sandbox
}
type Executor struct {
	spill        spill.Store
	root         string
	timeout      time.Duration
	captureLimit int
	secrets      SecretSource
	sandbox      Sandbox
	processes    *processTracker
}

type processTracker struct {
	mu   sync.Mutex
	live map[*os.Process]chan struct{}
}

func New(config Config) (*Executor, error) {
	if !groupsSupported {
		return nil, errors.New("foreground process groups require Linux or macOS")
	}
	files, err := workspace.NewFiles(config.Workspace)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(files.Root())
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, errors.New("shell workspace must be a directory")
	}
	if config.Timeout < 0 || config.CaptureLimit < 0 {
		return nil, errors.New("shell timeout and capture limit cannot be negative")
	}
	if config.Timeout == 0 {
		config.Timeout = 120 * time.Second
	}
	if config.CaptureLimit == 0 {
		config.CaptureLimit = CaptureLimit
	}
	if config.Secrets == nil {
		config.Secrets = secrets.Null{}
	}
	if config.Sandbox != nil {
		config.Sandbox, err = config.Sandbox.ForWorkspace(files.Root())
		if err != nil {
			return nil, err
		}
		if config.Sandbox == nil {
			return nil, errors.New("sandbox binding returned nil")
		}
	}
	return &Executor{spill: config.Spill, root: files.Root(), timeout: config.Timeout, captureLimit: config.CaptureLimit, secrets: config.Secrets, sandbox: config.Sandbox, processes: &processTracker{live: make(map[*os.Process]chan struct{})}}, nil
}

// WithSecrets returns an independent executor using the same bound process
// configuration. A session never changes a shared executor's credential scope.
func (executor *Executor) WithSecrets(source SecretSource) (*Executor, error) {
	clone, err := New(Config{Workspace: executor.root, Timeout: executor.timeout, CaptureLimit: executor.captureLimit, Secrets: source, Sandbox: executor.sandbox, Spill: executor.spill})
	if err == nil {
		clone.processes = executor.processes
	}
	return clone, err
}

// WithWorkspace rebinds cwd and sandbox together while retaining credentials,
// capture/deadline policy, spill and interrupt ownership. Existing copies stay
// bound to their old workspace; a child can safely keep its pinned executor.
func (executor *Executor) WithWorkspace(root string) (*Executor, error) {
	clone, err := New(Config{Workspace: root, Timeout: executor.timeout, CaptureLimit: executor.captureLimit, Secrets: executor.secrets, Sandbox: executor.sandbox, Spill: executor.spill})
	if err == nil {
		clone.processes = executor.processes
	}
	return clone, err
}

// WithSpill returns an independent executor bound to the same workspace,
// credentials and process tracker. Nil explicitly disables its string spill policy.
func (executor *Executor) WithSpill(store spill.Store) (*Executor, error) {
	clone, err := New(Config{Workspace: executor.root, Timeout: executor.timeout, CaptureLimit: executor.captureLimit, Secrets: executor.secrets, Sandbox: executor.sandbox, Spill: store})
	if err == nil {
		clone.processes = executor.processes
	}
	return clone, err
}

type TextMasker interface{ MaskText(string) string }
type maskingSecrets struct {
	SecretSource
	masker TextMasker
}

func (source maskingSecrets) MaskText(text string) string { return source.masker.MaskText(text) }
func (executor *Executor) WithMasker(masker TextMasker) (*Executor, error) {
	if masker == nil {
		return executor.WithSecrets(executor.secrets)
	}
	return executor.WithSecrets(maskingSecrets{executor.secrets, masker})
}

func (executor *Executor) Workspace() string { return executor.root }

// Configured sandbox argv is not evidence of OS confinement.
func (executor *Executor) SandboxConfigured() bool { return executor.sandbox != nil }
func (executor *Executor) ExecuteBash(ctx context.Context, input protocol.BashInput) (string, error) {
	result, err := executor.ExecuteBashResult(ctx, input)
	return executor.projectBash(ctx, result), err
}

// Interrupt kills all active foreground groups, including pipe-holding children
// whose shell has exited. The command goroutine owns reaping and pipe cleanup.
func (executor *Executor) Interrupt() int {
	executor.processes.mu.Lock()
	defer executor.processes.mu.Unlock()
	for process, interrupted := range executor.processes.live {
		killGroup(process)
		select {
		case interrupted <- struct{}{}:
		default:
		}
	}
	return len(executor.processes.live)
}
func (executor *Executor) ExecuteBashResult(ctx context.Context, input protocol.BashInput) (Result, error) {
	return executor.execute(ctx, input.Command, executionPolicy{timeout: executor.timeout})
}

func (executor *Executor) execute(ctx context.Context, command string, policy executionPolicy) (result Result, returnedErr error) {
	started := time.Now()
	result.CaptureLimit = executor.captureLimit
	defer func() { result.DurationMS = time.Since(started).Milliseconds() }()
	failure := func(err error) (Result, error) {
		text := executor.secrets.MaskText("Error: " + err.Error())
		result.Error = &text
		return result, nil
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if LooksDangerous(command) {
		return failure(errors.New("Dangerous command blocked"))
	}
	argv := []string{"/bin/sh", "-c", command}
	if executor.sandbox != nil {
		var err error
		argv, err = executor.sandbox.Argv(command)
		if err != nil {
			return failure(err)
		}
	}
	if len(argv) == 0 || argv[0] == "" {
		return failure(errors.New("sandbox returned empty argv"))
	}
	scrubbed := executor.secrets.ScrubEnvironment(nil)
	env := make(secrets.Environment, len(scrubbed))
	for key, value := range scrubbed {
		env[key] = value
	}
	for key, value := range executor.secrets.EnvForCommand(command) {
		env[key] = value
	}
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, string(key))
	}
	sort.Strings(keys)
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = executor.root
	// Explicit non-nil Env prevents an empty scrubbed environment inheriting host credentials.
	cmd.Env = make([]string, 0, len(keys))
	for _, key := range keys {
		cmd.Env = append(cmd.Env, key+"="+env[secrets.Name(key)])
	}
	configureGroup(cmd)
	stdout, stdoutWrite, err := os.Pipe()
	if err != nil {
		return failure(err)
	}
	defer stdout.Close()
	defer stdoutWrite.Close()
	stderr, stderrWrite, err := os.Pipe()
	if err != nil {
		return failure(err)
	}
	defer stderr.Close()
	defer stderrWrite.Close()
	cmd.Stdout, cmd.Stderr = stdoutWrite, stderrWrite
	if policy.background {
		cmd.Stderr = stdoutWrite
	}
	if err := cmd.Start(); err != nil {
		return failure(err)
	}
	_ = stdoutWrite.Close()
	_ = stderrWrite.Close()
	interrupted := make(chan struct{}, 1)
	if !policy.background {
		executor.processes.mu.Lock()
		executor.processes.live[cmd.Process] = interrupted
		executor.processes.mu.Unlock()
		defer func() {
			executor.processes.mu.Lock()
			delete(executor.processes.live, cmd.Process)
			executor.processes.mu.Unlock()
		}()
	}
	var startFault error
	if policy.started != nil {
		startFault = notifyStarted(policy.started, ProcessID(cmd.Process.Pid))
	}
	capture := &boundedCapture{limit: executor.captureLimit, overflow: make(chan struct{}, 1)}
	drained := make(chan error, 2)
	var bytes *byteCapture
	if policy.background {
		bytes = &byteCapture{limit: executor.captureLimit, overflow: capture.overflow}
		go func() { drained <- bytes.drain(stdout) }()
		go func() { drained <- nil }()
	} else {
		go func() { drained <- capture.drain(stdout, false) }()
		go func() { drained <- capture.drain(stderr, true) }()
	}
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	timer := time.NewTimer(policy.timeout)
	defer timer.Stop()
	deadline := timer.C
	cancelled := ctx.Done()
	overflow := capture.overflow
	var drainError error
	var waitError error
	var cleanup *time.Timer
	var killRetry *time.Ticker
	var retryKill <-chan time.Time
	defer func() {
		if cleanup != nil {
			cleanup.Stop()
		}
		if killRetry != nil {
			killRetry.Stop()
		}
	}()
	drains := 0
	reaped := false
	abandonedWait := false
	killed := false
	endGroup := func() {
		if killed {
			return
		}
		killed = true
		killGroup(cmd.Process)
		cleanup = time.NewTimer(5 * time.Second)
		deadline = cleanup.C
		// A concurrent shell fork can join the group after the first signal
		// broadcast. Retry while pipes remain open, within the cleanup deadline.
		killRetry = time.NewTicker(20 * time.Millisecond)
		retryKill = killRetry.C
	}
	if startFault != nil {
		endGroup()
	}
	for drains < 2 || !reaped && !abandonedWait {
		select {
		case err := <-drained:
			drains++
			if drains == 2 && killRetry != nil {
				killRetry.Stop()
				retryKill = nil
			}
			if err != nil && !errors.Is(err, os.ErrClosed) && drainError == nil {
				drainError = err
			}
		case err := <-waited:
			waitError = err
			reaped = true
			waited = nil
		case <-overflow:
			overflow = nil
			endGroup()
		case <-cancelled:
			returnedErr = ctx.Err()
			cancelled = nil
			endGroup()
		case <-interrupted:
			interrupted = nil
			endGroup()
		case <-retryKill:
			if drains < 2 {
				killGroup(cmd.Process)
			}
		case <-deadline:
			if !killed {
				result.TimedOut = true
				endGroup()
			} else {
				// A descendant may detach itself from the group and keep a pipe open.
				// Closing our read ends releases readers without an unbounded cleanup wait.
				_ = stdout.Close()
				_ = stderr.Close()
				deadline = nil
				killRetry.Stop()
				retryKill = nil
				if !reaped {
					abandonedWait = true
					waited = nil
				}
			}
		}
	}
	if bytes != nil {
		result.Stdout, result.Overflowed = bytes.finish()
	} else {
		result.Stdout, result.Stderr, result.Overflowed = capture.finish()
	}
	// Drainers and the parent can all finish before select consumes overflow.
	// The already-exited parent can still have a live producer in its group.
	if result.Overflowed && !killed {
		killGroup(cmd.Process)
	}
	projection := executor.secrets.MaskText(result.Stdout + result.Stderr)
	result.Stdout = executor.secrets.MaskText(result.Stdout)
	result.Stderr = executor.secrets.MaskText(result.Stderr)
	if projection != result.Stdout+result.Stderr {
		result.Stdout, result.Stderr = projection, ""
	}
	result.Projection = &projection
	if reaped && cmd.ProcessState != nil {
		code := processExitCode(cmd.ProcessState)
		result.ExitCode = &code
	}
	if startFault != nil {
		text := executor.secrets.MaskText("Error: " + startFault.Error())
		result.Error = &text
	} else if result.TimedOut {
		text := fmt.Sprintf("Error: Timeout (%gs)", policy.timeout.Seconds())
		result.Error = &text
	} else if drainError != nil {
		text := executor.secrets.MaskText("Error: " + drainError.Error())
		result.Error = &text
	} else if waitError != nil {
		var exit *exec.ExitError
		if !errors.As(waitError, &exit) {
			text := executor.secrets.MaskText("Error: " + waitError.Error())
			result.Error = &text
		}
	}
	return result, returnedErr
}

// Python's capture budget counts decoded characters, despite its bytes label.
// The fixed-size read buffers and shared rune budget bound allocation by the cap.
type boundedCapture struct {
	mu             sync.Mutex
	limit, size    int
	stdout, stderr strings.Builder
	overflowed     bool
	overflow       chan struct{}
}

func (capture *boundedCapture) drain(stream *os.File, stderr bool) error {
	reader := pytext.NewReader(stream)
	var chunk strings.Builder
	count := 0
	target := min(4096, capture.limit+1)
	flush := func() bool {
		if count == 0 {
			return false
		}
		capture.mu.Lock()
		defer capture.mu.Unlock()
		if capture.overflowed {
			return true
		}
		room := capture.limit - capture.size
		text := chunk.String()
		accepted := count
		if accepted > room {
			accepted = room
			text = string([]rune(text)[:room])
		}
		if stderr {
			capture.stderr.WriteString(text)
		} else {
			capture.stdout.WriteString(text)
		}
		capture.size += accepted
		if count > accepted {
			capture.overflowed = true
			capture.overflow <- struct{}{}
			return true
		}
		chunk.Reset()
		count = 0
		target = min(4096, capture.limit-capture.size+1)
		return false
	}
	for {
		r, err := reader.ReadTextRune()
		if err != nil {
			flush()
			if errors.Is(err, os.ErrClosed) || errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		chunk.WriteRune(r)
		count++
		if count >= target {
			if flush() {
				return nil
			}
		}
	}
}
func (capture *boundedCapture) finish() (string, string, bool) {
	capture.mu.Lock()
	defer capture.mu.Unlock()
	return capture.stdout.String(), capture.stderr.String(), capture.overflowed
}
