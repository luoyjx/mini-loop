package main

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestMain lets the daemon under test spawn this test binary as its mock MCP
// child: "<test binary> mock-mcp -data DIR" serves stdio instead of testing.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "mock-mcp" {
		os.Exit(run(context.Background(), os.Args[1:], osEnvironment(), streams{os.Stdin, os.Stdout, os.Stderr}))
	}
	os.Exit(m.Run())
}

func testEnvironment(t *testing.T) environment {
	config, home := t.TempDir(), t.TempDir()
	return environment{
		getenv:     func(string) string { return "" },
		executable: os.Executable,
		configDir:  func() (string, error) { return config, nil },
		homeDir:    func() (string, error) { return home, nil },
	}
}

func TestRequireLoopback(t *testing.T) {
	for addr, ok := range map[string]bool{"127.0.0.1:8790": true, "localhost:0": true, "[::1]:8790": true, "0.0.0.0:8790": false, "192.168.1.2:8790": false, ":8790": false} {
		if err := requireLoopback(addr); (err == nil) != ok {
			t.Errorf("%s: err=%v, want ok=%v", addr, err, ok)
		}
	}
}

func TestDefaultDataDirUsesUserConfigDir(t *testing.T) {
	env := testEnvironment(t)
	config, _ := env.configDir()
	if got := defaultDataDir(env); got != filepath.Join(config, "mini-loop", "assistant") {
		t.Fatalf("default data dir %q", got)
	}
	env.configDir = func() (string, error) { return "", errors.New("no config dir") }
	if got := defaultDataDir(env); got != filepath.Join("var", "assistant") {
		t.Fatalf("fallback data dir %q", got)
	}
}

func TestRunRefusesUnsafeStartup(t *testing.T) {
	cases := map[string][]string{
		"non-loopback bind":        {"-addr", "0.0.0.0:8790"},
		"anthropic without secret": {"-addr", "127.0.0.1:0", "-model", "anthropic", "-data", t.TempDir()},
		"unknown model":            {"-addr", "127.0.0.1:0", "-model", "gpt", "-data", t.TempDir()},
		"mock-mcp without data":    {"mock-mcp"},
	}
	for name, args := range cases {
		var stderr bytes.Buffer
		if code := run(context.Background(), args, testEnvironment(t), streams{strings.NewReader(""), io.Discard, &stderr}); code != 1 || !strings.Contains(stderr.String(), "miniloop-assistant:") {
			t.Errorf("%s: code=%d stderr=%q", name, code, stderr.String())
		}
	}
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// The daemon spawns the mock MCP server as a real child process, verifies its
// tool contract, serves, and shuts both down on cancellation.
func TestRunServesUntilCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stderr := &lockedBuffer{}
	data := t.TempDir()
	done := make(chan int, 1)
	go func() {
		done <- run(ctx, []string{"-addr", "127.0.0.1:0", "-data", data}, testEnvironment(t), streams{strings.NewReader(""), io.Discard, stderr})
	}()
	deadline := time.Now().Add(20 * time.Second)
	for !strings.Contains(stderr.String(), "demo on http://127.0.0.1:") {
		if time.Now().After(deadline) {
			t.Fatalf("daemon did not start: %q", stderr.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	if code := <-done; code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	if out := stderr.String(); !strings.Contains(out, "scripted") || !strings.Contains(out, "mcp=mock-mailcal over stdio, 10 tools") {
		t.Fatalf("startup line should name the offline model and the MCP child: %q", out)
	}
	if _, err := os.Stat(filepath.Join(data, worldDirName, "world.db")); err != nil {
		t.Fatalf("the mock server keeps its own database: %v", err)
	}
}

func TestLaunchdPlistIsWellFormed(t *testing.T) {
	env := testEnvironment(t)
	data := "/Users/me/Library/Application Support/mini-loop/assistant"
	var out, stderr bytes.Buffer
	code := run(context.Background(), []string{"launchd-plist", "-binary", "/Users/me/.local/bin/miniloop-assistant", "-data", data, "-log", "/tmp/a&b.log"}, env, streams{strings.NewReader(""), &out, &stderr})
	if code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	plist := out.String()
	for _, want := range []string{"<string>dev.mini-loop.assistant</string>", "<string>/Users/me/.local/bin/miniloop-assistant</string>", "<string>" + data + "</string>", "<string>127.0.0.1:8790</string>", "<string>/tmp/a&amp;b.log</string>", "<key>SuccessfulExit</key>"} {
		if !strings.Contains(plist, want) {
			t.Errorf("plist lacks %s:\n%s", want, plist)
		}
	}
	decoder := xml.NewDecoder(strings.NewReader(plist))
	decoder.Strict = true
	for {
		if _, err := decoder.Token(); err == io.EOF {
			break
		} else if err != nil {
			t.Fatalf("plist is not well-formed XML: %v", err)
		}
	}
	if lint, err := exec.LookPath("plutil"); err == nil {
		path := filepath.Join(t.TempDir(), "agent.plist")
		if err := os.WriteFile(path, out.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		if result, err := exec.Command(lint, "-lint", path).CombinedOutput(); err != nil {
			t.Fatalf("plutil -lint: %v %s", err, result)
		}
	}
}

func TestLaunchdPlistRefusesUnsafeInstalls(t *testing.T) {
	cases := map[string][]string{
		"go run binary":    {"launchd-plist", "-binary", "/var/folders/x/go-build123/b001/exe/miniloop-assistant"},
		"model credential": {"launchd-plist", "-binary", "/usr/local/bin/miniloop-assistant", "-model", "anthropic"},
		"non-loopback":     {"launchd-plist", "-binary", "/usr/local/bin/miniloop-assistant", "-addr", "0.0.0.0:8790"},
		"unknown embed":    {"launchd-plist", "-binary", "/usr/local/bin/miniloop-assistant", "-embed", "cloud"},
		"bad embed url":    {"launchd-plist", "-binary", "/usr/local/bin/miniloop-assistant", "-embed", "ollama", "-embed-url", "localhost:11434"},
	}
	for name, args := range cases {
		var out, stderr bytes.Buffer
		if code := run(context.Background(), args, testEnvironment(t), streams{strings.NewReader(""), &out, &stderr}); code != 1 || out.Len() != 0 {
			t.Errorf("%s: code=%d out=%q stderr=%q", name, code, out.String(), stderr.String())
		}
	}
}

// The plist passes the embedding flags through, and leaves them out when
// embeddings are off.
func TestLaunchdPlistCarriesTheEmbeddingFlags(t *testing.T) {
	plist := func(extra ...string) string {
		t.Helper()
		var out, stderr bytes.Buffer
		args := append([]string{"launchd-plist", "-binary", "/usr/local/bin/miniloop-assistant"}, extra...)
		if code := run(context.Background(), args, testEnvironment(t), streams{strings.NewReader(""), &out, &stderr}); code != 0 {
			t.Fatalf("code=%d stderr=%q", code, stderr.String())
		}
		return out.String()
	}
	if off := plist(); strings.Contains(off, "-embed") {
		t.Fatalf("embeddings are off by default:\n%s", off)
	}
	on := plist("-embed", "ollama", "-embed-model", "bge-m3")
	for _, want := range []string{"<string>-embed</string>\n\t\t<string>ollama</string>", "<string>http://127.0.0.1:11434</string>", "<string>bge-m3</string>"} {
		if !strings.Contains(on, want) {
			t.Errorf("plist lacks %q:\n%s", want, on)
		}
	}
}
