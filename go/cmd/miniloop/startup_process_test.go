package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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
	"github.com/luoyjx/mini-loop/go/launcher"
)

func TestMiniloopProcessEntry(t *testing.T) {
	if os.Getenv("MINILOOP_CLI_PROBE") != "1" {
		return
	}
	for index, arg := range os.Args {
		if arg == "--" {
			if marker := os.Getenv("MINILOOP_CLI_MARKER"); marker != "" {
				if err := os.WriteFile(marker, nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
			os.Args = append([]string{os.Args[0]}, os.Args[index+1:]...)
			main()
			return
		}
	}
	t.Fatal("missing CLI boundary")
}

func TestSignalTerminatesBlockedStartupFIFO(t *testing.T) {
	root := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(root, ".env"), 0600); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(root, "entered-main")
	command := cliProcess(t, root, "--dump-config")
	command.Env = append(command.Env, "MINILOOP_CLI_MARKER="+marker)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	waited := false
	defer func() {
		if !waited {
			_ = command.Process.Kill()
			<-done
		}
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("CLI helper did not reach main")
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)
	if err := command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		waited = true
		if err == nil {
			t.Fatal("blocked FIFO startup unexpectedly succeeded")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("runtime signal handler swallowed termination during bootstrap")
	}
}

func cliProcess(t *testing.T, root string, args ...string) *exec.Cmd {
	t.Helper()
	command := exec.Command(os.Args[0], append([]string{"-test.run=^TestMiniloopProcessEntry$", "--"}, args...)...)
	command.Dir = root
	command.Env = cliTestResourceEnvironment([]string{"MINILOOP_CLI_PROBE=1", "PATH=" + os.Getenv("PATH"), "MODEL_ID=incoming", "MINILOOP_WORKSPACE_ROOT=" + filepath.Join(root, "workspaces")})
	return command
}

func TestActualMainDumpAndBootstrapFailureOrder(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".env")
	if err := os.WriteFile(path, []byte("MODEL_ID=from-file\nANTHROPIC_API_KEY=file-key\nMINILOOP_FAKE_LLM=1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	command := cliProcess(t, root, "--dump-config")
	// The stdout protocol must remain independent of runtime stderr diagnostics.
	output, err := command.Output()
	if err != nil {
		t.Fatalf("%v: %s", err, output)
	}
	var report launcher.Report
	if err := json.Unmarshal(output, &report); err != nil {
		t.Fatal(err, string(output))
	}
	if !report.DotEnvDiscovery || report.Settings.Model != "from-file" || !report.Settings.FakeLLM || strings.Contains(string(output), "file-key") {
		t.Fatal(string(output))
	}
	if _, err := os.Stat(filepath.Join(root, "workspaces")); !os.IsNotExist(err) {
		t.Fatal("dump constructed runtime", err)
	}
	if err := os.WriteFile(path, []byte("BAD=\xff\n"), 0600); err != nil {
		t.Fatal(err)
	}
	command = cliProcess(t, root, "--help")
	output, err = command.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "invalid start byte") || strings.Contains(string(output), "Usage") {
		t.Fatal(err, string(output))
	}
	command = cliProcess(t, root, "--dump-config")
	command.Env = append(command.Env, "PYTHON_DOTENV_DISABLED=true")
	output, err = command.Output()
	if err != nil {
		t.Fatal(err, string(output))
	}
	if err := json.Unmarshal(output, &report); err != nil {
		t.Fatal(err, string(output))
	}
	if report.DotEnvDiscovery || report.Settings.Model != "incoming" {
		t.Fatal(string(output))
	}
}

func TestActualCLIHTTPBashInheritsPreparedEnvironment(t *testing.T) {
	root := t.TempDir()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	text := fmt.Sprintf("MODEL_ID=cli-model\nMINILOOP_FAKE_LLM=1\nPORT=%d\nCLI_VALUE=from-file\nANTHROPIC_BASE_URL=https://example.invalid\n", port)
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	command := cliProcess(t, root)
	command.Env = append(command.Env, "ANTHROPIC_AUTH_TOKEN=incoming-token")
	log, err := os.Create(filepath.Join(root, "cli.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	command.Stdout = log
	command.Stderr = log
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	waited := false
	defer func() {
		if !waited {
			_ = command.Process.Kill()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Error("CLI cleanup did not finish")
			}
		}
	}()
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	client := &http.Client{Timeout: 5 * time.Second}
	ready := false
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		response, err := client.Get(base + "/healthz")
		if err == nil {
			response.Body.Close()
			if response.StatusCode == 200 {
				ready = true
				break
			}
		}
		select {
		case err := <-done:
			waited = true
			t.Fatalf("CLI exited before ready: %v", err)
		default:
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !ready {
		data, _ := os.ReadFile(log.Name())
		t.Fatal("CLI did not become ready", string(data))
	}
	post := func(path, body string) []byte {
		t.Helper()
		response, err := client.Post(base+path, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, err := io.ReadAll(response.Body)
		if err != nil || response.StatusCode != 200 {
			t.Fatal(response.StatusCode, err, string(data))
		}
		return data
	}
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
		t.Fatal(err, answer.Final)
	}
	if !bytes.Contains(data, []byte("CLI_VALUE=from-file\n")) || !bytes.Contains(data, []byte("MODEL_ID=cli-model\n")) || bytes.Contains(data, []byte("ANTHROPIC_AUTH_TOKEN=")) {
		t.Fatal("Bash environment did not retain startup effects", string(data))
	}
	if err := command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		waited = true
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("CLI shutdown did not finish")
	}
}
