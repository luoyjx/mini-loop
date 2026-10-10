//go:build darwin || linux

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/httpapi"
	"github.com/luoyjx/mini-loop/go/reload"
)

// Copy the real command and its embed assets into an isolated development module.
// Edits exercise native rebuilding without changing the shared checkout.
func copyReloadModule(t *testing.T) string {
	t.Helper()
	source, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	err = filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == "testdata" || strings.HasPrefix(entry.Name(), ".") {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(target, relative), 0700)
		}
		if strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(target, relative), data, 0600)
	})
	if err != nil {
		t.Fatal(err)
	}
	return target
}

func TestForcedPollingCLIRebuildsSourceRetainsPortAndReloadsDotenv(t *testing.T) {
	testActualCLIRebuildsSource(t, reload.PollingBackend)
}
func testActualCLIRebuildsSource(t *testing.T, backend reload.ObserverBackend) {
	module := copyReloadModule(t)
	probe := filepath.Join(module, "cmd", "miniloop", "reload_version.go")
	generation := 0
	writeSource := func(model string, broken bool) {
		t.Helper()
		generation++
		source := fmt.Sprintf("package main\nimport \"os\"\nfunc init(){os.Setenv(\"MODEL_ID\",%q)}\n", model)
		if broken {
			source = "package main\nfunc broken( {"
		}
		pending := filepath.Join(filepath.Dir(probe), ".reload-source")
		if err := os.WriteFile(pending, []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
		stamp := time.Unix(int64(100+generation), 0)
		if err := os.Chtimes(pending, stamp, stamp); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(pending, probe); err != nil {
			t.Fatal(err)
		}
	}
	writeSource("reload-first", false)
	builder, err := reload.NewBuilder(reload.BuildConfig{ModuleDirectory: module, TemporaryParent: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	buildCtx, buildCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer buildCancel()
	artifact, err := builder.Build(buildCtx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer artifact.Close()
	binary, err := artifact.Executable(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	reservation, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := reservation.Addr().(*net.TCPAddr).Port
	reservation.Close()
	dotenv := filepath.Join(module, ".env")
	writeEnv := func(value string, childReload bool) {
		t.Helper()
		reloadValue := "false"
		configuredPort := port
		if !childReload {
			reloadValue = ""
			configuredPort = 0
		}
		text := fmt.Sprintf("MINILOOP_RELOAD=%s\nMINILOOP_FAKE_LLM=1\nHOST=127.0.0.1\nPORT=%d\nCLI_VALUE=%s\nMINILOOP_RELOAD_LISTENER_FD=dotenv-cannot-select-role\n", reloadValue, configuredPort, value)
		if err := os.WriteFile(dotenv, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	writeEnv("first", true)
	builds := filepath.Join(module, "compiler-temporaries")
	if err := os.Mkdir(builds, 0700); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(binary, "--reload-module", module)
	command.Dir = module
	command.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME"), "GOMAXPROCS=2", "TMPDIR=" + builds, "MINILOOP_WORKSPACE_ROOT=" + filepath.Join(module, "workspaces")}
	if backend == reload.PollingBackend {
		command.Env = append(command.Env, "WATCHFILES_FORCE_POLLING=0", "WATCHFILES_POLL_DELAY_MS=10")
	}
	log, err := os.Create(filepath.Join(module, "runtime.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	command.Stdout, command.Stderr = log, log
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	joined := false
	childPIDs := []int{}
	defer func() {
		if !joined {
			command.Process.Signal(syscall.SIGTERM)
			select {
			case <-done:
			case <-time.After(15 * time.Second):
				command.Process.Kill()
				t.Error("CLI cleanup did not join")
			}
		}
		if !joined {
			for _, pid := range childPIDs {
				_ = syscall.Kill(-pid, syscall.SIGKILL)
			}
		}
	}()
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	client := &http.Client{Timeout: time.Second}
	defer client.CloseIdleConnections()
	health := func(model string) httpapi.HealthResponse {
		t.Helper()
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) {
			response, err := client.Get(base + "/healthz")
			if err == nil {
				var report httpapi.HealthResponse
				err = json.NewDecoder(response.Body).Decode(&report)
				response.Body.Close()
				if err == nil && report.Model == model && report.Status == "ok" {
					if report.PID == command.Process.Pid {
						t.Fatal("supervisor owns runtime")
					}
					childPIDs = append(childPIDs, report.PID)
					return report
				}
			}
			select {
			case err := <-done:
				joined = true
				data, _ := os.ReadFile(log.Name())
				t.Fatalf("CLI exited: %v\n%s", err, data)
			default:
			}
			time.Sleep(30 * time.Millisecond)
		}
		data, _ := os.ReadFile(log.Name())
		t.Fatalf("model %q did not become ready\n%s", model, data)
		return httpapi.HealthResponse{}
	}
	post := func(path, body string) []byte {
		t.Helper()
		response, err := client.Post(base+path, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode != 200 {
			t.Fatal("HTTP status", response.StatusCode)
		}
		data, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	checkEnvironment := func(want string) httpapi.SessionInfo {
		t.Helper()
		var info httpapi.SessionInfo
		if err := json.Unmarshal(post("/sessions", "{}"), &info); err != nil {
			t.Fatal(err)
		}
		path := "/sessions/" + string(info.ID)
		post(path+"/mode", `{"mode":"auto"}`)
		var answer httpapi.MessageResponse
		if err := json.Unmarshal(post(path+"/messages", `{"message":";env>env.txt"}`), &answer); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(answer.Info.Workspace, "env.txt"))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(data, []byte("CLI_VALUE="+want+"\n")) || bytes.Contains(data, []byte(reload.ListenerEnvironment+"=")) {
			t.Fatalf("invalid child environment: %s", data)
		}
		return info
	}
	first := health("reload-first")
	data, err := os.ReadFile(log.Name())
	if err != nil || !bytes.Contains(data, []byte("observer="+string(backend))) {
		t.Fatal("CLI observer selection", string(data), err)
	}
	session := checkEnvironment("first")
	writeEnv("second", true)
	writeSource("reload-second", false)
	second := health("reload-second")
	if second.PID == first.PID {
		t.Fatal("source edit did not replace process")
	}
	checkEnvironment("second")
	response, err := client.Get(base + "/sessions/" + string(session.ID))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 404 {
		t.Fatal("old process-local session survived", response.StatusCode)
	}
	writeSource("", true)
	failed := false
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		data, _ := os.ReadFile(log.Name())
		if bytes.Contains(data, []byte("reload build_failed")) {
			failed = true
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if !failed {
		t.Fatal("invalid source did not fail compilation")
	}
	if duplicate, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port)); err == nil {
		duplicate.Close()
		t.Fatal("compile failure released port")
	}
	// Child role and retained listener remain authoritative even if dotenv disables
	// reload or changes PORT after the parent captured its configuration.
	writeEnv("recovered", false)
	writeSource("reload-recovered", false)
	third := health("reload-recovered")
	if third.PID == second.PID {
		t.Fatal("repair did not replace process")
	}
	checkEnvironment("recovered")
	if err := command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		joined = true
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("supervisor shutdown timeout")
	}
	for _, pid := range childPIDs {
		if syscall.Kill(pid, 0) == nil {
			t.Fatal("server child survived", pid)
		}
	}
	remaining, err := os.ReadDir(builds)
	if err != nil || len(remaining) != 0 {
		t.Fatal("private builds survived", remaining, err)
	}
	rebound, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatal("listener survived", err)
	}
	rebound.Close()
}

func TestActualInheritedRoleUsesPublicAddressBeforeAllocation(t *testing.T) {
	listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4zero})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	file, err := listener.File()
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("HOST=127.0.0.1\nPORT=0\nMINILOOP_RELOAD=\nMINILOOP_RELOAD_LISTENER_FD=dotenv-overwrite\n"), 0600); err != nil {
		t.Fatal(err)
	}
	template := cliProcess(t, root)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, template.Path, template.Args[1:]...)
	command.Dir = template.Dir
	command.Env = append(template.Env, reload.ListenerEnvironment+"=3")
	command.ExtraFiles = []*os.File{file}
	output, err := command.CombinedOutput()
	if ctx.Err() != nil || err == nil || !bytes.Contains(output, []byte("refusing to bind")) {
		t.Fatal(err, ctx.Err(), string(output))
	}
	if _, err := os.Stat(filepath.Join(root, "workspaces")); !os.IsNotExist(err) {
		t.Fatal("inherited bind refusal allocated runtime", err)
	}
}
