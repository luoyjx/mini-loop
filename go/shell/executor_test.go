//go:build darwin || linux

package shell

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/secrets"
)

type fixtureResult struct {
	Stdout       string  `json:"stdout"`
	Stderr       string  `json:"stderr"`
	ExitCode     *int    `json:"exit_code"`
	TimedOut     bool    `json:"timed_out"`
	Overflowed   bool    `json:"overflowed"`
	Error        *string `json:"error"`
	Projection   *string `json:"projection"`
	CaptureLimit int     `json:"capture_limit"`
}

func fixtureOf(result Result) fixtureResult {
	return fixtureResult{result.Stdout, result.Stderr, result.ExitCode, result.TimedOut, result.Overflowed, result.Error, result.Projection, result.CaptureLimit}
}
func (value fixtureResult) result() Result {
	return Result{Stdout: value.Stdout, Stderr: value.Stderr, ExitCode: value.ExitCode, TimedOut: value.TimedOut, Overflowed: value.Overflowed, Error: value.Error, Projection: value.Projection, CaptureLimit: value.CaptureLimit}
}
func makeExecutor(t *testing.T, config Config) *Executor {
	t.Helper()
	if config.Workspace == "" {
		config.Workspace = t.TempDir()
	}
	executor, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { executor.Interrupt() })
	return executor
}
func execute(t *testing.T, executor *Executor, command string) Result {
	t.Helper()
	result, err := executor.ExecuteBashResult(context.Background(), protocol.BashInput{Command: command})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func TestPythonCommandContracts(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-commands.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Commands []struct {
			Name, Command string
			Cap           int
			Result        fixtureResult
			Rendered      string
		}
		Rendering []struct {
			Name   string
			Result fixtureResult
			Repeat int    `json:"stdout_repeat"`
			Suffix string `json:"stdout_suffix"`
			Hash   string `json:"render_sha256"`
			Chars  int    `json:"render_chars"`
		}
		Dangerous []struct {
			Command string
			Blocked bool
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, item := range fixture.Commands {
		t.Run(item.Name, func(t *testing.T) {
			executor := makeExecutor(t, Config{CaptureLimit: item.Cap})
			result := execute(t, executor, item.Command)
			if result.DurationMS < 0 {
				t.Fatal("negative duration")
			}
			if item.Name == "cwd" {
				result.Stdout = strings.ReplaceAll(result.Stdout, executor.Workspace(), "<workspace>")
				value := strings.ReplaceAll(*result.Projection, executor.Workspace(), "<workspace>")
				result.Projection = &value
			}
			if !reflect.DeepEqual(fixtureOf(result), item.Result) {
				t.Fatalf("result = %#v, want %#v", fixtureOf(result), item.Result)
			}
			if result.Render() != item.Rendered {
				t.Fatalf("render %q, want %q", result.Render(), item.Rendered)
			}
		})
	}
	for _, item := range fixture.Rendering {
		t.Run(item.Name, func(t *testing.T) {
			result := item.Result.result()
			if item.Repeat > 0 {
				result.Stdout = strings.Repeat(result.Stdout, item.Repeat) + item.Suffix
			}
			rendered := result.Render()
			if fmt.Sprintf("%x", sha256.Sum256([]byte(rendered))) != item.Hash || utf8.RuneCountInString(rendered) != item.Chars {
				t.Fatal("Python rendering mismatch")
			}
		})
	}
	for _, item := range fixture.Dangerous {
		if LooksDangerous(item.Command) != item.Blocked {
			t.Fatalf("blocklist %q", item.Command)
		}
	}
}
func TestAggregateCaptureAndExactLimit(t *testing.T) {
	executor := makeExecutor(t, Config{CaptureLimit: 8})
	result := execute(t, executor, "printf '1234'; printf '5678' >&2")
	if result.Overflowed || result.Stdout != "1234" || result.Stderr != "5678" {
		t.Fatalf("exact cap: %#v", result)
	}
	executor = makeExecutor(t, Config{CaptureLimit: 1024})
	result = execute(t, executor, "while :; do printf 'abcdefgh'; printf 'ijklmnop' >&2; done")
	if !result.Overflowed || utf8.RuneCountInString(result.Stdout+result.Stderr) != 1024 {
		t.Fatal("aggregate cap did not stop producer")
	}
	if !strings.Contains(result.Render(), "exceeded 1,024 bytes") || result.DurationMS > 2000 {
		t.Fatal("overflow did not promptly report capture stop")
	}
}
func TestWholeStreamsMaskedBeforeProjection(t *testing.T) {
	registry := secrets.New(secrets.Config{})
	registry.RegisterValue("SHELL_TEST_CREDENTIAL", "credential-long-value")
	t.Setenv("SHELL_TEST_CREDENTIAL", "ambient-old-value")
	executor := makeExecutor(t, Config{Secrets: registry})
	// An unrelated command must not inherit a registered ambient credential.
	env := execute(t, executor, "env")
	if strings.Contains(env.Stdout, "SHELL_TEST_CREDENTIAL=") || strings.Contains(env.Stdout, "ambient-old-value") {
		t.Fatal("registered environment was inherited")
	}
	result := execute(t, executor, "printf '%s' \"$SHELL_TEST_CREDENTIAL\"")
	if result.Stdout != secrets.Mask || result.Render() != secrets.Mask {
		t.Fatal("named credential not injected and masked")
	}
	result = execute(t, executor, "printf 'credential-'; printf 'long-value' >&2")
	if result.Stdout != secrets.Mask || result.Stderr != "" || result.Render() != secrets.Mask {
		t.Fatal("streams reassembled an unmasked credential")
	}
	// A value spanning the model projection's cut point is masked in full first.
	command := "i=0; while [ $i -lt 24995 ]; do printf x; i=$((i+1)); done; printf 'credential-long-value'; i=0; while [ $i -lt 30000 ]; do printf y; i=$((i+1)); done"
	result = execute(t, executor, command)
	if strings.Contains(result.Stdout, "credential-") || !strings.Contains(result.Stdout, secrets.Mask) || strings.Contains(result.Render(), "credential-") {
		t.Fatal("truncation preceded masking")
	}
}
func awaitFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("command did not become ready")
}
func TestTimeoutKillsDescendantAfterShellExit(t *testing.T) {
	root := t.TempDir()
	executor := makeExecutor(t, Config{Workspace: root, Timeout: 80 * time.Millisecond})
	result := execute(t, executor, "printf 'partial'; (sleep 0.3; printf leaked > canary) &")
	if !result.TimedOut || result.Stdout != "partial" || result.ExitCode == nil || *result.ExitCode != 0 || !strings.Contains(result.Render(), "partial\nError: Timeout (0.08s)") {
		t.Fatalf("pipe timeout: %#v", result)
	}
	time.Sleep(350 * time.Millisecond)
	if _, err := os.Stat(filepath.Join(root, "canary")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("pipe-holding descendant survived timeout")
	}
}
func TestCancellationAndInterruptKillWholeGroup(t *testing.T) {
	for _, interrupt := range []bool{false, true} {
		t.Run(fmt.Sprint(interrupt), func(t *testing.T) {
			root := t.TempDir()
			executor := makeExecutor(t, Config{Workspace: root})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan struct {
				result Result
				err    error
			}, 1)
			go func() {
				result, err := executor.ExecuteBashResult(ctx, protocol.BashInput{Command: "trap '' TERM; (sleep 0.3; printf leaked > canary) & printf ready > ready; wait"})
				done <- struct {
					result Result
					err    error
				}{result, err}
			}()
			awaitFile(t, filepath.Join(root, "ready"))
			if interrupt {
				if executor.Interrupt() != 1 {
					t.Fatal("live process not registered")
				}
			} else {
				cancel()
			}
			select {
			case value := <-done:
				if interrupt && value.err != nil {
					t.Fatal(value.err)
				}
				if !interrupt && !errors.Is(value.err, context.Canceled) {
					t.Fatalf("cancel error %v", value.err)
				}
				if value.result.ExitCode == nil || *value.result.ExitCode != -9 {
					t.Fatal("group not killed with SIGKILL")
				}
			case <-time.After(2 * time.Second):
				t.Fatal("command did not settle")
			}
			if executor.Interrupt() != 0 {
				t.Fatal("live process not removed")
			}
			time.Sleep(350 * time.Millisecond)
			if _, err := os.Stat(filepath.Join(root, "canary")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("descendant survived cancellation")
			}
		})
	}
}
func TestBlockedCommandAndInvalidConfiguration(t *testing.T) {
	executor := makeExecutor(t, Config{})
	result := execute(t, executor, "sudo echo must-not-run > canary")
	if result.ExitCode != nil || result.Error == nil || result.Render() != "Error: Dangerous command blocked" || !result.Failed() {
		t.Fatal("direct caller bypassed typo guard")
	}
	if _, err := New(Config{Workspace: executor.Workspace(), Timeout: -1}); err == nil {
		t.Fatal("negative timeout accepted")
	}
	if _, err := New(Config{Workspace: executor.Workspace(), CaptureLimit: -1}); err == nil {
		t.Fatal("negative cap accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := executor.ExecuteBashResult(ctx, protocol.BashInput{Command: "touch canary"}); !errors.Is(err, context.Canceled) {
		t.Fatal("pre-cancelled command started")
	}
	if _, err := os.Stat(filepath.Join(executor.Workspace(), "canary")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("forbidden effect landed")
	}
}

func TestDeadlineAlsoCoversProcessWithClosedPipes(t *testing.T) {
	executor := makeExecutor(t, Config{Timeout: 60 * time.Millisecond})
	result := execute(t, executor, "exec 1>&- 2>&-; sleep 10")
	if !result.TimedOut || result.ExitCode == nil || *result.ExitCode != -9 || result.DurationMS > 1500 {
		t.Fatal("process wait escaped command deadline")
	}
}

type testSandbox struct {
	root    string
	program string
}

func (sandbox testSandbox) ForWorkspace(root string) (Sandbox, error) {
	sandbox.root = root
	return sandbox, nil
}
func (sandbox testSandbox) Argv(command string) ([]string, error) {
	if sandbox.program != "" {
		return []string{sandbox.program}, nil
	}
	return []string{"/bin/sh", "-c", "printf '%s' \"$1\"; " + command, "sh", sandbox.root}, nil
}
func TestSandboxArgvRebindingAndMaskedStartFailure(t *testing.T) {
	root := t.TempDir()
	executor := makeExecutor(t, Config{Workspace: root, Sandbox: testSandbox{root: "stale"}})
	if execute(t, executor, "true").Stdout != executor.Workspace() {
		t.Fatal("sandbox not rebound")
	}
	registry := secrets.New(secrets.Config{})
	registry.RegisterValue("PATH_SECRET", "credential-path-part")
	executor = makeExecutor(t, Config{Workspace: root, Secrets: registry, Sandbox: testSandbox{program: "/nonexistent/credential-path-part"}})
	result := execute(t, executor, "true")
	if !result.Failed() || strings.Contains(result.Render(), "credential-path-part") || !strings.Contains(result.Render(), secrets.Mask) || result.ExitCode != nil {
		t.Fatal("start error leaked a secret or invented process status")
	}
}
func TestConcurrentExecutionsRemainIndependent(t *testing.T) {
	executor := makeExecutor(t, Config{})
	done := make(chan string, 8)
	for i := 0; i < 8; i++ {
		go func(i int) {
			result, err := executor.ExecuteBashResult(context.Background(), protocol.BashInput{Command: fmt.Sprintf("printf result-%d", i)})
			if err != nil {
				done <- "error"
				return
			}
			done <- result.Stdout
		}(i)
	}
	seen := make(map[string]bool)
	for i := 0; i < 8; i++ {
		select {
		case value := <-done:
			seen[value] = true
		case <-time.After(2 * time.Second):
			t.Fatal("concurrent commands blocked")
		}
	}
	for i := 0; i < 8; i++ {
		if !seen[fmt.Sprintf("result-%d", i)] {
			t.Fatal("concurrent capture state crossed commands")
		}
	}
	if executor.Interrupt() != 0 {
		t.Fatal("completed calls retained live process entries")
	}
}

type helperSandbox struct{ root string }

func (sandbox helperSandbox) ForWorkspace(root string) (Sandbox, error) {
	sandbox.root = root
	return sandbox, nil
}
func (sandbox helperSandbox) Argv(string) ([]string, error) {
	return []string{os.Args[0], "-test.run=^TestDetachedPipeHelper$", "--", "spawn", sandbox.root}, nil
}
func TestDetachedPipeHelper(t *testing.T) {
	args := os.Args
	if len(args) < 4 || args[len(args)-3] != "--" {
		return
	}
	mode, root := args[len(args)-2], args[len(args)-1]
	if mode == "hold" {
		time.Sleep(15 * time.Second)
		return
	}
	if mode != "spawn" {
		return
	}
	command := exec.Command(args[0], "-test.run=^TestDetachedPipeHelper$", "--", "hold", root)
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	command.Stdout, command.Stderr = os.Stdout, os.Stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "detached-pid"), []byte(fmt.Sprint(command.Process.Pid)), 0600); err != nil {
		t.Fatal(err)
	}
}
func TestDetachedPipeHolderCannotHangCleanup(t *testing.T) {
	root := t.TempDir()
	t.Cleanup(func() {
		data, err := os.ReadFile(filepath.Join(root, "detached-pid"))
		if err == nil {
			pid, err := strconv.Atoi(string(data))
			if err == nil {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
	})
	executor := makeExecutor(t, Config{Workspace: root, Timeout: 60 * time.Millisecond, Sandbox: helperSandbox{}})
	result := execute(t, executor, "ignored")
	if !result.TimedOut || result.DurationMS < 5000 || result.DurationMS > 8000 {
		t.Fatalf("detached drain deadline %d ms, timed out %v", result.DurationMS, result.TimedOut)
	}
}

type nilEnvironmentSource struct{ secrets.Null }

func (nilEnvironmentSource) ScrubEnvironment(secrets.Environment) secrets.Environment { return nil }
func (nilEnvironmentSource) EnvForCommand(string) secrets.Environment {
	return secrets.Environment{"EMPTY_ENV_VALUE": "selected"}
}
func TestEmptyCustomEnvironmentCanReceiveSelectedValues(t *testing.T) {
	executor := makeExecutor(t, Config{Secrets: nilEnvironmentSource{}})
	if execute(t, executor, "printf '%s' \"$EMPTY_ENV_VALUE\"").Stdout != "selected" {
		t.Fatal("empty environment selection lost")
	}
}

func TestOverflowKillsProducerEvenWhenParentHasExited(t *testing.T) {
	root := t.TempDir()
	executor := makeExecutor(t, Config{Workspace: root, CaptureLimit: 4})
	for i := 0; i < 8; i++ {
		command := fmt.Sprintf("(sleep 0.04; printf abcdefgh; sleep 0.3; printf leaked > canary-%d) 2>/dev/null &", i)
		result := execute(t, executor, command)
		if !result.Overflowed || result.DurationMS > 2000 {
			t.Fatal("background producer escaped overflow")
		}
	}
	time.Sleep(350 * time.Millisecond)
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(root, fmt.Sprintf("canary-%d", i))); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("overflow left a child alive after its parent exited")
		}
	}
}
