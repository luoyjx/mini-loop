package httpapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/benchmark"
	"github.com/luoyjx/mini-loop/go/secrets"
)

func normalizeBenchmark(report benchmark.FakeReport) benchmark.FakeReport {
	for i := range report.Baseline {
		report.Baseline[i].DurationMilliseconds = nil
	}
	for i := range report.Candidate {
		report.Candidate[i].DurationMilliseconds = nil
	}
	for _, comparison := range []*benchmark.Comparison{&report.Comparison, &report.HeldoutComparison} {
		delete(comparison.Dimensions, benchmark.Duration)
		warnings := []string{}
		for _, warning := range comparison.DimensionWarnings {
			if !strings.HasPrefix(warning, "duration_ms ") {
				warnings = append(warnings, warning)
			}
		}
		comparison.DimensionWarnings = warnings
	}
	return report
}

func TestBenchmarkActualPythonHTTP(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-benchmark-http.json")
	if err != nil {
		t.Fatal(err)
	}
	fixture := decode[struct {
		Cases []struct {
			Name        string
			Raw         string
			Environment map[string]string
			MainFake    bool `json:"main_fake"`
			Status      int
			Response    benchmark.FakeReport
		}
	}](t, data)
	if len(fixture.Cases) != 5 {
		t.Fatal("source benchmark inventory drift")
	}
	for _, test := range fixture.Cases {
		t.Run(test.Name, func(t *testing.T) {
			t.Setenv("MINILOOP_MAX_TURNS", "50")
			t.Setenv("MINILOOP_FAKE_DELAY", "0")
			for key, value := range test.Environment {
				t.Setenv(key, value)
			}
			main := &countedProvider{}
			manager := testManager(t, main)
			s := testServer(t, Config{Manager: manager, Auth: tokenAuth(t), FakeLLM: test.MainFake, BenchmarkSkillsDir: t.TempDir() + "/missing"})
			if w := request(s, "POST", "/benchmark", "", ""); w.Code != 401 {
				t.Fatalf("auth %d", w.Code)
			}
			// Captured app skills win over process changes; model/root/body never select
			// the live main provider or its workspace.
			t.Setenv("MINILOOP_SKILLS_DIR", "ignored-environment-path")
			t.Setenv("MINILOOP_WORKSPACE_ROOT", "/dev/null/ignored-env-root")
			scratch := t.TempDir()
			t.Setenv("TMPDIR", scratch)
			w := request(s, "POST", "/benchmark?real=true", test.Raw, "token-a")
			if w.Code != test.Status {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
			report := decode[benchmark.FakeReport](t, w.Body.Bytes())
			for _, rows := range [][]benchmark.TaskResult{report.Baseline, report.Candidate} {
				for _, row := range rows {
					if row.DurationMilliseconds == nil || row.DurationMilliseconds.Float64() < 0 {
						t.Fatal("missing or negative measured duration")
					}
				}
			}
			got, _ := json.Marshal(normalizeBenchmark(report))
			want, _ := json.Marshal(test.Response)
			if string(got) != string(want) {
				t.Fatalf("actual source comparison mismatch\ngot %s\nwant %s", got, want)
			}
			if main.calls.Load() != 0 {
				t.Fatal("main provider called")
			}
			entries, err := os.ReadDir(scratch)
			if err != nil || len(entries) != 0 {
				t.Fatalf("temporary workspaces survived: %v %v", entries, err)
			}
		})
	}
}

func TestBenchmarkSharedRateAndMethods(t *testing.T) {
	s := testServer(t, Config{Manager: testManager(t, doneProvider{}), Auth: tokenAuth(t), RateLimitPerMinute: 1, Now: func() time.Time { return time.Unix(120, 0) }})
	if w := request(s, "GET", "/benchmark", "", "token-a"); w.Code != 405 || w.Header().Get("Allow") != "POST" {
		t.Fatal("method contract")
	}
	session := create(t, s, "token-a")
	if w := request(s, "POST", "/sessions/"+string(session.ID)+"/messages", `{"message":"go"}`, "token-a"); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	w := request(s, "POST", "/benchmark", "", "token-a")
	if w.Code != 429 || w.Header().Get("Retry-After") != "60" {
		t.Fatalf("shared rate: %d %s", w.Code, w.Body.String())
	}
	// A cancelled request consumes the admitted caller's budget, but leaves no
	// temporary root and never reads a paid provider.
	scratch := t.TempDir()
	t.Setenv("TMPDIR", scratch)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := httptest.NewRequest("POST", "/benchmark", nil).WithContext(ctx)
	r.Header.Set("Authorization", "Bearer token-b")
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 500 {
		t.Fatalf("cancelled %d", w.Code)
	}
	entries, _ := os.ReadDir(scratch)
	if len(entries) != 0 {
		t.Fatal("cancelled request allocated root")
	}
	if w := request(s, "POST", "/benchmark", "", "token-b"); w.Code != 429 {
		t.Fatal("independent owner's budget not accounted")
	}
}

func TestBenchmarkPrivateConfigurationFailure(t *testing.T) {
	s := testServer(t, Config{Manager: testManager(t, doneProvider{})})
	t.Setenv("MINILOOP_MAX_TURNS", "private-invalid-value")
	w := request(s, "POST", "/benchmark", "", "")
	if w.Code != 500 || strings.Contains(w.Body.String(), "private-invalid-value") {
		t.Fatal("configuration failure leaked")
	}
	t.Setenv("MINILOOP_MAX_TURNS", "50")
	t.Setenv("MINILOOP_FEATURES", "all")
	w = request(s, "POST", "/benchmark", "", "")
	if w.Code != 500 {
		t.Fatal("unsupported activation silently ignored")
	}
}

func TestBenchmarkRecordingProjection(t *testing.T) {
	registry := secrets.New(secrets.Config{})
	registry.RegisterValue("NOTE", "fake transport")
	manager, err := agent.NewSessionManager(agent.ManagerConfig{WorkspaceRoot: t.TempDir(), Services: agent.ManagerServices{Provider: doneProvider{}, Secrets: registry}})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Stop(context.Background())
	s := testServer(t, Config{Manager: manager})
	w := request(s, "POST", "/benchmark", "", "")
	if w.Code != 200 || strings.Contains(w.Body.String(), "fake transport") {
		t.Fatalf("projection %d %s", w.Code, w.Body.String())
	}
	report := decode[benchmark.FakeReport](t, w.Body.Bytes())
	if len(report.Baseline) != 5 || report.Baseline[0].ContextTokensEstimate.Float64() != 208 {
		t.Fatal("mask projection corrupted concrete numbers")
	}
}
