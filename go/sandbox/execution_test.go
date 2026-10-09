package sandbox

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/shell"
)

func requireSeatbelt(t *testing.T) {
	t.Helper()
	if !Available() {
		t.Skip(*UnavailableReason())
	}
}
func execute(t *testing.T, executor *shell.Executor, command string) shell.Result {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := executor.ExecuteBashResult(ctx, protocol.BashInput{Command: command})
	if err != nil {
		t.Fatal(err)
	}
	if result.TimedOut || result.Error != nil {
		t.Fatalf("command failed to execute: %#v", result)
	}
	return result
}
func succeeded(t *testing.T, result shell.Result) {
	t.Helper()
	if result.ExitCode == nil || *result.ExitCode != 0 {
		t.Fatalf("expected success: %#v", result)
	}
}
func denied(t *testing.T, result shell.Result) {
	t.Helper()
	if result.ExitCode == nil || *result.ExitCode == 0 {
		t.Fatalf("expected denied command: %#v", result)
	}
}
func newExecutor(t *testing.T, root string, sandbox shell.Sandbox) *shell.Executor {
	t.Helper()
	executor, err := shell.New(shell.Config{Workspace: root, Sandbox: sandbox, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	return executor
}
func TestActualDefaultSeatbeltReadWriteAndRebind(t *testing.T) {
	requireSeatbelt(t)
	root := testRoot(t)
	t.Setenv("HOME", root)
	ws := filepath.Join(root, "ws")
	next := filepath.Join(root, "next")
	for _, directory := range []string{ws, next, filepath.Join(root, ".ssh")} {
		if err := os.Mkdir(directory, 0700); err != nil {
			t.Fatal(err)
		}
	}
	credential := filepath.Join(root, ".ssh", "canary")
	if err := os.WriteFile(credential, []byte("PRIVATE-CANARY"), 0600); err != nil {
		t.Fatal(err)
	}
	backend, err := NewDefault(ws, DefaultOptions{Require: true})
	if err != nil {
		t.Fatal(err)
	}
	executor := newExecutor(t, ws, backend)
	if executor.SandboxPosture().Name != "SeatbeltSandbox" {
		t.Fatal("native backend not observable")
	}
	result := execute(t, executor, "printf shell-works; printf content > inside")
	succeeded(t, result)
	if result.Stdout != "shell-works" {
		t.Fatal(result)
	}
	denied(t, execute(t, executor, "cat "+quote(credential)))
	denied(t, execute(t, executor, "ls "+quote(filepath.Dir(credential))))
	denied(t, execute(t, executor, "cp "+quote(credential)+" stolen"))
	if _, err := os.Stat(filepath.Join(ws, "stolen")); !os.IsNotExist(err) {
		t.Fatal("credential copy exists", err)
	}
	out := filepath.Join(root, "outside")
	denied(t, execute(t, executor, "printf forbidden > "+quote(out)))
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatal("outside file exists", err)
	}
	rebound, err := executor.WithWorkspace(next)
	if err != nil {
		t.Fatal(err)
	}
	succeeded(t, execute(t, rebound, "printf rebound > new-file"))
	denied(t, execute(t, rebound, "printf stale > "+quote(filepath.Join(ws, "stale"))))
	succeeded(t, execute(t, executor, "printf original > still-bound"))
	if _, err := os.Stat(filepath.Join(next, "new-file")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(ws, "stale")); !os.IsNotExist(err) {
		t.Fatal("stale scope remained writable", err)
	}
	// Seatbelt permits children but defines no CPU, memory or PID resource caps.
	succeeded(t, execute(t, rebound, "for i in 1 2 3; do (true &); done; printf spawned"))
}
func TestActualSeatbeltNetworkDeniedAndExplicitlyAllowed(t *testing.T) {
	requireSeatbelt(t)
	if _, err := os.Stat("/usr/bin/curl"); err != nil {
		t.Skip("system curl unavailable")
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte("reachable-canary"))
	}))
	defer server.Close()
	root := testRoot(t)
	command := "/usr/bin/curl --noproxy '*' --max-time 3 --silent --show-error " + quote(server.URL)
	allowed, err := NewSeatbelt(Config{WritableRoots: []string{root}, AllowNetwork: true})
	if err != nil {
		t.Fatal(err)
	}
	positive := execute(t, newExecutor(t, root, allowed), command)
	succeeded(t, positive)
	if positive.Stdout != "reachable-canary" {
		t.Fatal("positive network control failed", positive)
	}
	closed, err := NewSeatbelt(Config{WritableRoots: []string{root}})
	if err != nil {
		t.Fatal(err)
	}
	negative := execute(t, newExecutor(t, root, closed), command)
	denied(t, negative)
	if strings.Contains(negative.Stdout, "reachable-canary") {
		t.Fatal("denied network returned response")
	}
}
func TestActualSeatbeltWorktreeCanCommitWithoutWritingSiblingFiles(t *testing.T) {
	requireSeatbelt(t)
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git unavailable")
	}
	root := testRoot(t)
	repo := filepath.Join(root, "repo")
	linked := filepath.Join(root, "linked")
	sibling := filepath.Join(root, "sibling")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	gitRun := func(arguments ...string) {
		t.Helper()
		if output, err := exec.CommandContext(ctx, git, arguments...).CombinedOutput(); err != nil {
			t.Fatalf("git failed: %v %s", err, output)
		}
	}
	gitRun("init", "-q", repo)
	gitRun("-C", repo, "-c", "user.name=fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-qm", "initial")
	gitRun("-C", repo, "worktree", "add", "-q", "-b", "fixture-linked", linked)
	gitRun("-C", repo, "worktree", "add", "-q", "-b", "fixture-sibling", sibling)
	backend, err := NewSeatbelt(Config{WritableRoots: []string{linked}})
	if err != nil {
		t.Fatal(err)
	}
	executor := newExecutor(t, linked, backend)
	succeeded(t, execute(t, executor, "printf committed > proof && "+quote(git)+" add proof && "+quote(git)+" -c user.name=fixture -c user.email=fixture@example.invalid -c commit.gpgsign=false commit -qm confined"))
	gitRun("-C", linked, "diff", "--exit-code", "HEAD")
	for _, other := range []string{repo, sibling} {
		path := filepath.Join(other, "forbidden")
		denied(t, execute(t, executor, "printf forbidden > "+quote(path)))
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("sibling file exists", err)
		}
	}
}
func TestConfigurationCopiesAndConcurrentRebinding(t *testing.T) {
	root := testRoot(t)
	config := Config{WritableRoots: []string{root}, UnreadableRoots: []string{filepath.Join(root, "deny")}, ExtraWritable: []string{filepath.Join(root, "extra")}}
	backend, err := NewSeatbelt(config)
	if err != nil {
		t.Fatal(err)
	}
	before := observe(t, backend, "true", root)
	config.WritableRoots[0] = "changed"
	config.UnreadableRoots[0] = "changed"
	config.ExtraWritable[0] = "changed"
	copy := backend.Configuration()
	copy.WritableRoots[0] = "changed"
	copy.UnreadableRoots[0] = "changed"
	copy.ExtraWritable[0] = "changed"
	equalSnapshot(t, observe(t, backend, "true", root), before)
	beforeArgv, err := backend.Argv("true")
	if err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	for index := 0; index < 8; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for attempt := 0; attempt < 20; attempt++ {
				_, err := backend.ForWorkspace(filepath.Join(root, "next"))
				if err != nil {
					t.Error(err)
					return
				}
				argv, err := backend.Argv("true")
				if err != nil {
					t.Error(err)
					return
				}
				if !reflect.DeepEqual(argv, beforeArgv) {
					t.Error("source binding changed")
					return
				}
			}
		}()
	}
	group.Wait()
	unavailable := NewUnavailable("reason")
	*unavailable.SandboxReason() = "changed"
	if *unavailable.SandboxReason() != "reason" {
		t.Fatal("reason aliases caller")
	}
}

func TestUnavailableExecutorRunsHostAndReportsDetachedReason(t *testing.T) {
	root := testRoot(t)
	backend, err := defaultForHost(root, DefaultOptions{}, hostFacts{system: "Linux", binaryExists: true})
	if err != nil {
		t.Fatal(err)
	}
	executor := newExecutor(t, root, backend)
	posture := executor.SandboxPosture()
	if posture.Name != "UnavailableSandbox" || posture.Reason == nil || !strings.Contains(*posture.Reason, "this host is Linux") {
		t.Fatal(posture)
	}
	*posture.Reason = "changed"
	if *executor.SandboxPosture().Reason == "changed" {
		t.Fatal("executor reason aliases observation")
	}
	result := execute(t, executor, "printf host-fallback")
	succeeded(t, result)
	if result.Stdout != "host-fallback" {
		t.Fatal(result)
	}
	host := newExecutor(t, root, nil)
	if host.SandboxConfigured() || host.SandboxPosture().Name != "None" {
		t.Fatal("host default changed")
	}
}
