package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/luoyjx/mini-loop/go/config"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/launcher"
)

func member(t *testing.T, value jsonvalue.Value, key string) jsonvalue.Value {
	t.Helper()
	result, ok := value.Lookup(key)
	if !ok {
		t.Fatal("missing fixture field", key)
	}
	return result
}

func TestRemoteCommandMatchesSourceThroughActualHTTP(t *testing.T) {
	data, err := os.ReadFile("../../testdata/python-audit-remote-cli.json")
	if err != nil {
		t.Fatal(err)
	}
	fixture, err := jsonvalue.Decode(string(data))
	if err != nil {
		t.Fatal(err)
	}
	rows, ok := member(t, fixture, "cases").Array()
	if !ok || len(rows) != 22 {
		t.Fatal("unexpected Source CLI corpus")
	}
	for _, row := range rows {
		name, _ := member(t, row, "name").Text()
		t.Run(name, func(t *testing.T) {
			body, err := member(t, row, "report").MarshalJSON()
			if err != nil {
				t.Fatal(err)
			}
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != "GET" || r.URL.Path != "/healthz" || r.Header.Get("Authorization") != "" {
					t.Error("request diverged from public Source health fetch", r)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write(body)
			}))
			defer server.Close()
			var stdout, stderr bytes.Buffer
			code := execute(context.Background(), []string{"--url", server.URL + "///"}, &stdout, &stderr)
			wantCodeText, _ := member(t, row, "exit_code").Integer()
			wantCode, _ := strconv.Atoi(wantCodeText)
			wantText, _ := member(t, row, "stdout").Text()
			got := strings.ReplaceAll(stdout.String(), server.URL, "http://fixture.invalid")
			if code != wantCode || got != wantText || stderr.Len() != 0 || calls.Load() != 1 {
				t.Fatalf("code=%d calls=%d stdout=%q stderr=%q; want %d %q", code, calls.Load(), got, stderr.String(), wantCode, wantText)
			}
		})
	}
}

func TestRemoteCommandAuditsActualNativeLauncherWithoutCreatingSession(t *testing.T) {
	root := t.TempDir()
	settings, err := config.Load(map[string]string{"MINILOOP_FAKE_LLM": "1", "MINILOOP_WORKSPACE_ROOT": filepath.Join(root, "workspaces"), "MINILOOP_TRAJECTORY_ROOT": filepath.Join(root, "trajectories")}, config.LoadOptions{Directory: root})
	if err != nil {
		t.Fatal(err)
	}
	app, err := launcher.New(context.Background(), settings, config.ServerSettings{Host: "127.0.0.1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Stop(context.Background())
	server := httptest.NewServer(app.Handler())
	defer server.Close()
	var stdout, stderr bytes.Buffer
	if code := execute(context.Background(), []string{"--url", server.URL}, &stdout, &stderr); code != 1 || stderr.Len() != 0 {
		t.Fatal(code, stderr.String())
	}
	for _, check := range []string{"authentication", "shell-confinement", "secret-masking", "durable-state", "action-journal"} {
		if !strings.Contains(stdout.String(), check) {
			t.Fatal("missing actual default posture finding", check, stdout.String())
		}
	}
	response, err := http.Get(server.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var health bytes.Buffer
	_, _ = health.ReadFrom(response.Body)
	value, err := jsonvalue.Decode(health.String())
	if err != nil {
		t.Fatal(err)
	}
	sessions, _ := member(t, value, "sessions").Integer()
	if sessions != "0" {
		t.Fatal("audit allocated session", sessions)
	}
}

func TestRemoteCommandUsageFailuresAndAbsentHeaderFields(t *testing.T) {
	for _, args := range [][]string{nil, {"--url"}, {"--local"}} {
		var out, fault bytes.Buffer
		if code := execute(context.Background(), args, &out, &fault); code != 2 || out.Len() != 0 || !strings.Contains(fault.String(), "usage:") {
			t.Fatal(code, out.String(), fault.String())
		}
	}
	for _, body := range []string{`{}`, `{"build":null,"pid":null}`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, body) }))
		var out, fault bytes.Buffer
		code := execute(context.Background(), []string{"--url", server.URL}, &out, &fault)
		server.Close()
		if code != 0 || !strings.Contains(out.String(), "build=None pid=None") || fault.Len() != 0 {
			t.Fatal(code, out.String(), fault.String())
		}
	}
	var out, fault bytes.Buffer
	if code := execute(context.Background(), []string{"--url", "://invalid"}, &out, &fault); code != 1 || out.Len() != 0 || fault.Len() == 0 {
		t.Fatal(code, out.String(), fault.String())
	}
}
