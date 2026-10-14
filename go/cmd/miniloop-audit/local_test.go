package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type localSourceCase struct {
	Name      string
	Overrides map[string]string
	Args      []string
	ExitCode  *int `json:"exit_code"`
	Stdout    string
	Fault     *struct{ Type, Message string }
}

func localSourceCases(t *testing.T) []localSourceCase {
	t.Helper()
	data, err := os.ReadFile("../../testdata/python-audit-local-cli.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct{ Cases []localSourceCase }
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 14 {
		t.Fatal("Source local CLI corpus changed")
	}
	return fixture.Cases
}

func TestLocalCommandMatchesActualSourceConstruction(t *testing.T) {
	matched := 0
	for _, row := range localSourceCases(t) {
		if row.Fault != nil {
			continue
		}
		t.Run(row.Name, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			root = filepath.Join(root, row.Name)
			env := map[string]string{"MINILOOP_FAKE_LLM": "1", "MINILOOP_WORKSPACE_ROOT": filepath.Join(root, "workspaces"), "MINILOOP_SKILLS_DIR": filepath.Join(root, "skills")}
			for key, value := range row.Overrides {
				env[key] = value
			}
			if row.Name == "world-writable" {
				if err := os.MkdirAll(env["MINILOOP_WORKSPACE_ROOT"], 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(env["MINILOOP_WORKSPACE_ROOT"], 0777); err != nil {
					t.Fatal(err)
				}
			}
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(500) }))
			defer server.Close()
			env["ANTHROPIC_BASE_URL"] = server.URL
			var out, fault bytes.Buffer
			code := executeWithEnvironment(context.Background(), row.Args, env, &out, &fault)
			got := strings.ReplaceAll(out.String(), filepath.Dir(root), "ROOT")
			if code != *row.ExitCode || got != row.Stdout || fault.Len() != 0 || calls.Load() != 0 {
				t.Fatalf("code=%d calls=%d out=%q fault=%q; want %q", code, calls.Load(), got, fault.String(), row.Stdout)
			}
			entries, err := os.ReadDir(env["MINILOOP_WORKSPACE_ROOT"])
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if !strings.HasPrefix(entry.Name(), ".") {
					t.Fatal("audit created session", entry.Name())
				}
			}
		})
		matched++
	}
	if matched != 12 {
		t.Fatal("unexpected matched profile count", matched)
	}
}

func TestLocalCommandReportsOperationalFailuresAndPendingBackends(t *testing.T) {
	for _, extra := range []map[string]string{
		{"MINILOOP_MAX_TURNS": "0"},
		{"MINILOOP_API_TOKENS": "alice:shared,bob:shared"},
		{"MINILOOP_FEATURES": "1"},
	} {
		env := map[string]string{"MINILOOP_FAKE_LLM": "1", "MINILOOP_WORKSPACE_ROOT": filepath.Join(t.TempDir(), "workspaces")}
		for key, value := range extra {
			env[key] = value
		}
		if _, full := extra["MINILOOP_FEATURES"]; full {
			t.Setenv("PATH", t.TempDir())
		}
		var out, fault bytes.Buffer
		if code := executeWithEnvironment(context.Background(), nil, env, &out, &fault); code != 1 || out.Len() != 0 || fault.Len() == 0 {
			t.Fatal(code, out.String(), fault.String())
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out, fault bytes.Buffer
	if code := executeWithEnvironment(ctx, nil, map[string]string{"MINILOOP_FAKE_LLM": "1", "MINILOOP_WORKSPACE_ROOT": filepath.Join(t.TempDir(), "cancelled")}, &out, &fault); code != 1 || out.Len() != 0 || !strings.Contains(fault.String(), "canceled") {
		t.Fatal(code, out.String(), fault.String())
	}
}

func TestLocalAuditNativeBinaryMatchesSource(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	binary := filepath.Join(t.TempDir(), "miniloop-audit")
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, ".")
	if data, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, data)
	}
	root := t.TempDir()
	cmd := exec.CommandContext(ctx, binary)
	cmd.Dir = root
	cmd.Env = []string{"MINILOOP_FAKE_LLM=1", "MINILOOP_WORKSPACE_ROOT=" + filepath.Join(root, "workspaces"), "MINILOOP_SKILLS_DIR=" + filepath.Join(root, "skills"), "PORT=not-a-port"}
	var out, fault bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &fault
	err := cmd.Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 || fault.Len() != 0 {
		t.Fatal(err, out.String(), fault.String())
	}
	for _, row := range localSourceCases(t) {
		if row.Name == "default" && out.String() != row.Stdout {
			t.Fatal("native binary output differs", out.String())
		}
	}
}
