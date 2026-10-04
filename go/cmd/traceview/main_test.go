package main

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/trajectory"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIExportAndStoredSessionProducePrivateSelfContainedHTML(t *testing.T) {
	data, err := os.ReadFile("../../testdata/python-trace-view.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct{ File struct{ Raw string } }
	json.Unmarshal(data, &fixture)
	root := t.TempDir()
	source := filepath.Join(root, "export.jsonl")
	os.WriteFile(source, []byte(fixture.File.Raw), 0600)
	output := filepath.Join(root, "page.html")
	var stdout, stderr bytes.Buffer
	if code := execute(context.Background(), []string{source, "-o", output}, map[string]string{"MINILOOP_MAX_TURNS": "invalid"}, &stdout, &stderr); code != 0 {
		t.Fatal(code, stderr.String())
	}
	page, _ := os.ReadFile(output)
	info, _ := os.Stat(output)
	if info.Mode().Perm() != 0600 || strings.TrimSpace(stdout.String()) != output || !bytes.Contains(page, []byte("last-end")) || !bytes.Contains(page, []byte("filter records")) || bytes.Contains(page, []byte("<script>alert(")) {
		t.Fatal("bad private HTML export")
	}
	store, err := trajectory.New(trajectory.Config{Root: filepath.Join(root, "traces"), CaptureContent: true})
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 2; i++ {
		id, err := store.Start(agent.TrajectoryStart{Session: "stored-session", Owner: "alice", RunIndex: i, Input: "prompt"})
		if err != nil {
			t.Fatal(err)
		}
		if err := store.Finish(id, agent.TrajectoryFinish{Status: agent.TrajectoryCompleted}); err != nil {
			t.Fatal(err)
		}
	}
	output = filepath.Join(root, "session.html")
	stdout.Reset()
	stderr.Reset()
	if code := execute(context.Background(), []string{"stored-session", "--root", store.Root(), "--output", output}, nil, &stdout, &stderr); code != 0 {
		t.Fatal(code, stderr.String())
	}
	page, _ = os.ReadFile(output)
	if bytes.Count(page, []byte(`<section class="turn">`)) != 2 {
		t.Fatal("session turns not assembled")
	}
	if code := execute(context.Background(), []string{"missing", "--root", store.Root()}, nil, &stdout, &stderr); code != 1 {
		t.Fatal("missing target accepted")
	}
	if code := execute(context.Background(), []string{"--unknown"}, nil, &stdout, &stderr); code != 2 {
		t.Fatal("unknown flag accepted")
	}
}
