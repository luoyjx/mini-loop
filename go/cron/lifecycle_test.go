package cron

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestLiveClaimLoserConsumesMinuteAndCanWinNext(t *testing.T) {
	f := contracts(t)
	path := filepath.Join(t.TempDir(), "jobs.json")
	seed(t, path, Job{ID: "shared", Cron: "* * * * *", Prompt: "claim", Session: "session-a", Recurring: true, Durable: true})
	counts := [2]int{}
	services := [2]*Scheduler{}
	for n := range services {
		i := n
		services[n] = makeScheduler(t, path, ResolverFunc(func(SessionID) (Runner, error) {
			counts[i]++
			return RunnerFunc(func(context.Context, Invocation) error { return nil }), nil
		}))
		services[n].ArmAll()
	}
	a, b := services[0], services[1]
	a.Tick(now())
	b.Tick(now())
	b.Tick(now().Add(time.Minute))
	a.Tick(now().Add(time.Minute))
	waitRuns(t, a)
	waitRuns(t, b)
	if counts[0] != f.Pair.LeftFires || counts[1] != f.Pair.RightFires || a.Jobs()[0].LastFired != f.Pair.LeftMarker || b.Jobs()[0].LastFired != f.Pair.RightMarker || claims(t, a) != f.Pair.Claims || len(a.Problems().Entries)+len(b.Problems().Entries) != len(f.Pair.Problems) {
		t.Fatal(counts, a.Jobs(), b.Jobs(), a.Problems(), b.Problems())
	}
	if count, e := b.CancelForSession("session-a"); e != nil || count != 1 || claims(t, b) != 0 {
		t.Fatal(count, e)
	}
}
func TestClaimProcessHelper(t *testing.T) {
	path := os.Getenv("MINILOOP_CRON_TEST_STORE")
	if path == "" {
		t.Skip("subprocess helper")
	}
	ready := os.Getenv("MINILOOP_CRON_TEST_READY")
	gate := os.Getenv("MINILOOP_CRON_TEST_GATE")
	result := os.Getenv("MINILOOP_CRON_TEST_RESULT")
	count := 0
	s := makeScheduler(t, path, ResolverFunc(func(SessionID) (Runner, error) {
		count++
		return RunnerFunc(func(context.Context, Invocation) error { return nil }), nil
	}))
	s.ArmAll()
	if e := os.WriteFile(ready, []byte("ready"), 0600); e != nil {
		t.Fatal(e)
	}
	awaitFile(t, gate)
	s.Tick(now())
	waitRuns(t, s)
	if e := os.WriteFile(result, []byte(fmt.Sprint(count)), 0600); e != nil {
		t.Fatal(e)
	}
}
func awaitFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, e := os.Stat(path); e == nil {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("marker never appeared: %s", path)
}
func TestSeparateProcessesExclusivelyClaimSharedOccurrence(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "jobs.json")
	seed(t, path, Job{ID: "shared", Cron: "* * * * *", Prompt: "claim", Session: "s", Recurring: true, Durable: true})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	commands := [2]*exec.Cmd{}
	gate := filepath.Join(root, "gate")
	for n := range commands {
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestClaimProcessHelper$")
		cmd.Env = append(os.Environ(), "MINILOOP_CRON_TEST_STORE="+path, "MINILOOP_CRON_TEST_READY="+filepath.Join(root, fmt.Sprintf("ready-%d", n)), "MINILOOP_CRON_TEST_RESULT="+filepath.Join(root, fmt.Sprintf("result-%d", n)), "MINILOOP_CRON_TEST_GATE="+gate)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		commands[n] = cmd
		if e := cmd.Start(); e != nil {
			t.Fatal(e)
		}
	}
	for n := range commands {
		awaitFile(t, filepath.Join(root, fmt.Sprintf("ready-%d", n)))
	}
	if e := os.WriteFile(gate, []byte("go"), 0600); e != nil {
		t.Fatal(e)
	}
	results := []string{}
	for n, cmd := range commands {
		if e := cmd.Wait(); e != nil {
			t.Fatal(e)
		}
		b, e := os.ReadFile(filepath.Join(root, fmt.Sprintf("result-%d", n)))
		if e != nil {
			t.Fatal(e)
		}
		results = append(results, string(b))
	}
	if strings.Join(results, "") != "10" && strings.Join(results, "") != "01" {
		t.Fatal("both stale processes dispatched or neither won", results)
	}
}
func TestStopCancelsRunsAndExpiredObserverCanResume(t *testing.T) {
	started := make(chan struct{})
	cancelled := make(chan struct{})
	release := make(chan struct{})
	s := makeScheduler(t, "", ResolverFunc(func(SessionID) (Runner, error) {
		return RunnerFunc(func(ctx context.Context, _ Invocation) error {
			close(started)
			<-ctx.Done()
			close(cancelled)
			<-release
			return ctx.Err()
		}), nil
	}))
	schedule(t, s, Request{Cron: "* * * * *", Prompt: "blocked", Durable: flag(false)})
	if e := s.Start(); e != nil {
		t.Fatal(e)
	}
	s.mu.Lock()
	loop := s.loop
	s.mu.Unlock()
	if e := s.Start(); e != nil {
		t.Fatal(e)
	}
	s.mu.Lock()
	if s.loop != loop {
		t.Fatal("duplicate ticker")
	}
	s.mu.Unlock()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("ticker never dispatched")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if e := s.Stop(ctx); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("run was not cancelled")
	}
	if e := s.Start(); e == nil {
		t.Fatal("admission reopened during drain")
	}
	close(release)
	if e := s.Stop(context.Background()); e != nil {
		t.Fatal(e)
	}
	if count, e := s.CancelForSession(""); e != nil || count != 1 {
		t.Fatal(count, e)
	}
	if e := s.Start(); e != nil {
		t.Fatal(e)
	}
	if e := s.Stop(context.Background()); e != nil {
		t.Fatal(e)
	}
}
func TestStopFencesResolutionAlreadyInFlight(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	dispatch := make(chan struct{}, 1)
	s := makeScheduler(t, "", ResolverFunc(func(SessionID) (Runner, error) {
		close(entered)
		<-release
		return RunnerFunc(func(context.Context, Invocation) error { dispatch <- struct{}{}; return nil }), nil
	}))
	schedule(t, s, Request{Cron: "* * * * *", Durable: flag(false)})
	done := make(chan struct{})
	go func() { s.Tick(now()); close(done) }()
	<-entered
	if e := s.Stop(context.Background()); e != nil {
		t.Fatal(e)
	}
	close(release)
	<-done
	waitRuns(t, s)
	if len(dispatch) != 0 || s.Problems().Total != 1 {
		t.Fatal("late resolution admitted a stopped generation", s.Problems())
	}
}
func TestLoadKeepsValidRowsAndRejectsDynamicFieldTypes(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "jobs.json")
	if e := os.WriteFile(path, []byte(`[{"id":"ok","cron":"* * * * *","prompt":"ok","session_id":"s"},{"id":"bad","cron":"invalid","prompt":"x","session_id":"s"},{"id":4,"cron":"* * * * *","prompt":"x","session_id":"s"},{"id":"unknown","cron":"* * * * *","prompt":"x","session_id":"s","armed":true}]`), 0600); e != nil {
		t.Fatal(e)
	}
	s := makeScheduler(t, path, nil)
	if jobs := s.Jobs(); len(jobs) != 1 || jobs[0].ID != "ok" || !jobs[0].Recurring || jobs[0].Durable || s.Armed("ok") || s.Problems().Total != 3 {
		t.Fatal(jobs, s.Problems())
	}
	if count := s.ArmAll(); count != 1 || s.ArmAll() != 0 {
		t.Fatal(count)
	}
	for _, body := range []string{"{", `{"jobs":[]}`, "null"} {
		if e := os.WriteFile(path, []byte(body), 0600); e != nil {
			t.Fatal(e)
		}
		s = makeScheduler(t, path, nil)
		want := uint64(0)
		if body == "{" {
			want = 1
		}
		if len(s.Jobs()) != 0 || s.Problems().Total != want {
			t.Fatal(body, s.Problems())
		}
	}
	if _, e := New(Config{}); e == nil {
		t.Fatal("unbound scheduler accepted")
	}
}
func TestProblemsCountChurnAndAreDetached(t *testing.T) {
	s := makeScheduler(t, "", nil)
	for n := 0; n < 120; n++ {
		s.problem(fmt.Sprintf("problem %d", n%60))
	}
	p := s.Problems()
	if len(p.Entries) != 50 || p.Total != 120 || p.Dropped != 70 {
		t.Fatal(p)
	}
	p.Entries[0].Text = "changed"
	if s.Problems().Entries[0].Text == "changed" {
		t.Fatal("borrowed problem state")
	}
	s.problem(s.Problems().Entries[0].Text)
	if !strings.HasSuffix(s.Problems().Summary()[0], "(x2)") {
		t.Fatal(s.Problems())
	}
	jobs := s.Jobs()
	if !reflect.DeepEqual(jobs, []Job{}) {
		t.Fatal(jobs)
	}
}

type panicMask struct{}

func (panicMask) Mask(string) string { panic("mask fault") }
func TestCallbackFaultsAreReportedAndOtherJobsContinue(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "jobs.json")
	seed(t, path, Job{ID: "mask", Cron: "* * * * *", Prompt: "secret", Session: "s", Recurring: true, Durable: true})
	s := makeScheduler(t, path, nil)
	s.secrets = panicMask{}
	s.ArmAll()
	s.Tick(now())
	if s.Problems().Total != 1 || s.Jobs()[0].LastFired == "" {
		t.Fatal(s.Problems(), s.Jobs())
	}
	s = makeScheduler(t, "", ResolverFunc(func(id SessionID) (Runner, error) {
		if id == "panic" {
			panic("resolver fault")
		}
		return RunnerFunc(func(context.Context, Invocation) error { return errors.New("runner fault") }), nil
	}))
	schedule(t, s, Request{Session: "panic", Cron: "* * * * *", Durable: flag(false)})
	schedule(t, s, Request{Session: "error", Cron: "* * * * *", Durable: flag(false)})
	s.Tick(now())
	waitRuns(t, s)
	if s.Problems().Total != 2 {
		t.Fatal(s.Problems())
	}
}
