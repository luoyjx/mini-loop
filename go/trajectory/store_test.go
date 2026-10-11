package trajectory

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/luoyjx/mini-loop/go/agent"
)

func equivalentJSON(t *testing.T, actual, source []byte) {
	t.Helper()
	var a, b interface{}
	if json.Unmarshal(actual, &a) != nil || json.Unmarshal(source, &b) != nil {
		t.Fatal("invalid JSON")
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("actual %s\nsource %s", actual, source)
	}
}
func TestActualPythonJSONLReaders(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-trajectory.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Stores []struct {
			Name     string
			Raw      string
			Capture  bool `json:"capture_content"`
			Summary  json.RawMessage
			Document json.RawMessage
			Count    int
			RootMode uint32 `json:"root_mode"`
			FileMode uint32 `json:"file_mode"`
		}
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Stores) != 8 {
		t.Fatal("source inventory changed")
	}
	for _, row := range fixture.Stores {
		t.Run(row.Name, func(t *testing.T) {
			s, err := New(Config{Root: filepath.Join(t.TempDir(), "traces"), CaptureContent: row.Capture})
			if err != nil {
				t.Fatal(err)
			}
			id := agent.TrajectoryID("traj_" + strings.Repeat("a", 24))
			path, _ := s.path(id)
			if err = os.WriteFile(path, []byte(row.Raw), 0600); err != nil {
				t.Fatal(err)
			}
			if row.Name == "open" {
				s.active[id] = true
			}
			summary, err := s.Summary(id)
			if err != nil {
				t.Fatal(err)
			}
			encoded, _ := json.Marshal(summary)
			equivalentJSON(t, encoded, row.Summary)
			document, err := s.JSON(id, 8*1024*1024)
			if err != nil {
				t.Fatal(err)
			}
			equivalentJSON(t, document, row.Document)
			count, err := s.Count("session-a")
			if err != nil || count != row.Count {
				t.Fatal(count, err)
			}
			var stream bytes.Buffer
			if err = s.Stream(context.Background(), id, &stream); err != nil || stream.String() != row.Raw {
				t.Fatal("stream differs", err)
			}
			for _, test := range []struct {
				Path string
				Mode uint32
			}{{s.root, row.RootMode}, {path, row.FileMode}} {
				info, err := os.Stat(test.Path)
				if err != nil || uint32(info.Mode().Perm()) != test.Mode {
					t.Fatal("mode differs", err)
				}
			}
		})
	}
}
func TestTypedWriterPrivacyAndConcurrentRecords(t *testing.T) {
	for _, capture := range []bool{true, false} {
		t.Run(map[bool]string{true: "capture", false: "redact"}[capture], func(t *testing.T) {
			s, err := New(Config{Root: t.TempDir(), CaptureContent: capture})
			if err != nil {
				t.Fatal(err)
			}
			model, system := "fixture-model", "secret system"
			input := "secret 你好🙂"
			id, err := s.Start(agent.TrajectoryStart{Session: "s", Owner: "alice", RunIndex: 1, Input: input, Metadata: agent.TrajectoryMetadata{Model: &model, System: &system}})
			if err != nil {
				t.Fatal(err)
			}
			record := []byte(`{"record_type":"event","type":"tool_result","input":{"token":"secret"},"model_input":{"messages":[{"role":"user","content":"secret"}]},"output":"secret output","error":null}`)
			var workers sync.WaitGroup
			faults := make(chan error, 32)
			for i := 0; i < 32; i++ {
				workers.Add(1)
				go func() { defer workers.Done(); faults <- s.write(id, record) }()
			}
			workers.Wait()
			close(faults)
			for err := range faults {
				if err != nil {
					t.Fatal(err)
				}
			}
			output := "secret final"
			duration := 25.12355
			if err = s.Finish(id, agent.TrajectoryFinish{Status: agent.TrajectoryCompleted, Output: &output, DurationMS: &duration}); err != nil {
				t.Fatal(err)
			}
			summary, err := s.Summary(id)
			if err != nil || summary.Metrics.EventCount != 32 || summary.Status != agent.TrajectoryCompleted || summary.DurationMS == nil || *summary.DurationMS != 25.124 {
				t.Fatal(summary, err)
			}
			document, err := s.JSON(id, 8*1024*1024)
			if err != nil {
				t.Fatal(err)
			}
			if capture && !bytes.Contains(document, []byte("secret final")) {
				t.Fatal("lost content")
			}
			if !capture && bytes.Contains(document, []byte("secret")) {
				t.Fatal("privacy leaked")
			}
			if !capture {
				var value struct {
					Input  string
					Events []struct {
						Input      string `json:"input"`
						ModelInput string `json:"model_input"`
					}
				}
				if json.Unmarshal(document, &value) != nil || value.Input != "[redacted: 10 chars]" || value.Events[0].Input != "[redacted: dict, 1 item(s)]" || value.Events[0].ModelInput != "[redacted: dict, 1 item(s)]" {
					t.Fatal("redaction shape differs", string(document))
				}
			}
		})
	}
}
func TestBoundsCorruptionAndExplicitPurge(t *testing.T) {
	s, err := New(Config{Root: t.TempDir(), CaptureContent: true})
	if err != nil {
		t.Fatal(err)
	}
	id, err := s.Start(agent.TrajectoryStart{Session: "owned", Owner: "alice", Input: "x"})
	if err != nil {
		t.Fatal(err)
	}
	path, _ := s.path(id)
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	record := `{"record_type":"event","type":"model_start","model_input":"` + strings.Repeat("x", 20000) + `"}` + "\n"
	for i := 0; i < 500; i++ {
		if _, err = io.WriteString(file, record); err != nil {
			t.Fatal(err)
		}
	}
	file.Close()
	summary, err := s.Summary(id)
	if err != nil || summary.Metrics.ModelCalls != 500 {
		t.Fatal(summary, err)
	}
	if _, err = s.JSON(id, 8*1024*1024); !errors.Is(err, ErrTooLarge) {
		t.Fatal("accepted oversized JSON", err)
	}
	if err = s.Stream(context.Background(), id, io.Discard); err != nil {
		t.Fatal(err)
	}
	corrupt := filepath.Join(s.root, "traj_"+strings.Repeat("b", 24)+".jsonl")
	os.WriteFile(corrupt, []byte("not a header\n"), 0600)
	foreign, err := s.Start(agent.TrajectoryStart{Session: "foreign", Owner: "bob", Input: "x"})
	if err != nil {
		t.Fatal(err)
	}
	removed, err := s.DeleteForSession("owned")
	if err != nil || removed != 1 {
		t.Fatal(removed, err)
	}
	if _, err = s.Summary(foreign); err != nil {
		t.Fatal("deleted foreign recording", err)
	}
	if _, err = os.Stat(corrupt); err != nil {
		t.Fatal("guessed corrupt ownership", err)
	}
	problems, count := s.Problems()
	if count == 0 || len(problems) == 0 {
		t.Fatal("corrupt recording disappeared silently")
	}
	for _, bad := range []agent.TrajectoryID{"../../escape", "traj_bad", agent.TrajectoryID("traj_" + strings.Repeat("a", 25))} {
		if _, err = s.Summary(bad); !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid path accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err = s.Stream(ctx, foreign, io.Discard); !errors.Is(err, context.Canceled) {
		t.Fatal("stream ignored cancellation", err)
	}
}

func TestSourceDurationRoundingAndBoundedProblemLog(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-trajectory.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Rounding []struct{ Input, Output float64 }
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Rounding) == 0 {
		t.Fatal("missing source rounding evidence")
	}
	store, err := New(Config{Root: t.TempDir(), CaptureContent: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range fixture.Rounding {
		id, err := store.Start(agent.TrajectoryStart{Session: "round", Owner: "alice"})
		if err != nil {
			t.Fatal(err)
		}
		if err := store.Finish(id, agent.TrajectoryFinish{Status: agent.TrajectoryCompleted, DurationMS: &row.Input}); err != nil {
			t.Fatal(err)
		}
		summary, err := store.Summary(id)
		if err != nil || summary.DurationMS == nil || *summary.DurationMS != row.Output {
			t.Fatalf("round(%v): %v, want %v: %v", row.Input, summary.DurationMS, row.Output, err)
		}
	}
	store.report("repeated")
	store.report("repeated")
	for i := 0; i < 50; i++ {
		store.report(fmt.Sprint(i))
	}
	entries, total, dropped := store.ProblemDiagnostics()
	if len(entries) != 50 || total != 52 || dropped != 1 || entries[0].Message != "0" {
		t.Fatal(entries, total, dropped)
	}
	store.report("0")
	entries, total, dropped = store.ProblemDiagnostics()
	if entries[0].Count != 2 || total != 53 || dropped != 1 {
		t.Fatal(entries, total, dropped)
	}
	entries[0].Message = "mutated"
	again, _, _ := store.ProblemDiagnostics()
	if again[0].Message != "0" {
		t.Fatal("diagnostic aliases store")
	}
}

func TestBoundedScanCountsMalformedBytes(t *testing.T) {
	store, err := New(Config{Root: t.TempDir(), CaptureContent: true})
	if err != nil {
		t.Fatal(err)
	}
	id, err := store.Start(agent.TrajectoryStart{Session: "s", Owner: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.JSON(id, 1<<63-1); err != nil {
		t.Fatal("maximum int64 read cap overflowed", err)
	}
	path, _ := store.path(id)
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	file.WriteString(strings.Repeat("bad JSON\n", 1000))
	file.Close()
	_, _, _, _, err = store.scanBounded(id, nil, 1024)
	if !errors.Is(err, ErrTooLarge) {
		t.Fatal("malformed records escaped byte bound", err)
	}
}
