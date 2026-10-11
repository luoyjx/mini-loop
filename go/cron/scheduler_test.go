package cron

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

type fireEvidence struct {
	Job  Job   `json:"job"`
	Disk []Job `json:"disk"`
}
type operation struct {
	Name, Text string
	Jobs       []Job
	Armed      []ID
	Problems   []string
	Fires      []fireEvidence
	Claims     int
}
type lossCase struct {
	Kind         string
	JobCount     int `json:"job_count"`
	Marker       *string
	FireCount    int    `json:"fire_count"`
	ProblemTotal uint64 `json:"problem_total"`
}
type fixture struct {
	Dates  []string
	Parser []struct {
		Expression string
		Error      *string
		Matches    []bool
	}
	Operations []operation
	OneShot    struct {
		Jobs  []Job
		Fires []fireEvidence
		Disk  []Job
	} `json:"one_shot"`
	Pair struct {
		LeftFires   int    `json:"left_fires"`
		RightFires  int    `json:"right_fires"`
		LeftMarker  string `json:"left_marker"`
		RightMarker string `json:"right_marker"`
		Claims      int
		Problems    []string
	}
	Losses  []lossCase
	Masking struct {
		Live, Stored, Restored string
		Problems               []string
	}
	Bounds struct {
		PromptError string `json:"prompt_error"`
		JobError    string `json:"job_error"`
	}
	Authority struct {
		Runs []struct {
			Prompt    string
			Authority Authority
		}
		Problems []string
	}
}

func contracts(t *testing.T) fixture {
	t.Helper()
	b, e := os.ReadFile("../testdata/python-cron.json")
	if e != nil {
		t.Fatal(e)
	}
	var f fixture
	if e = json.Unmarshal(b, &f); e != nil {
		t.Fatal(e)
	}
	return f
}
func now() time.Time    { return time.Date(2026, 10, 5, 12, 30, 0, 0, time.Local) }
func flag(v bool) *bool { return &v }
func makeScheduler(t *testing.T, path string, resolver Resolver) *Scheduler {
	t.Helper()
	if resolver == nil {
		resolver = ResolverFunc(func(SessionID) (Runner, error) {
			return RunnerFunc(func(context.Context, Invocation) error { return nil }), nil
		})
	}
	s, e := New(Config{DurablePath: path, Resolver: resolver})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if e := s.Stop(context.Background()); e != nil {
			t.Error(e)
		}
	})
	return s
}
func waitRuns(t *testing.T, s *Scheduler) {
	t.Helper()
	ctx, c := context.WithTimeout(context.Background(), 5*time.Second)
	defer c()
	if e := s.Wait(ctx); e != nil {
		t.Fatal(e)
	}
}
func schedule(t *testing.T, s *Scheduler, r Request) Scheduled {
	t.Helper()
	j, e := s.Schedule(r)
	if e != nil {
		t.Fatal(e)
	}
	return j
}
func disk(t *testing.T, path string) []Job {
	t.Helper()
	b, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	var jobs []Job
	if e = json.Unmarshal(b, &jobs); e != nil {
		t.Fatal(e)
	}
	return jobs
}
func claims(t *testing.T, s *Scheduler) int {
	t.Helper()
	entries, e := os.ReadDir(s.claims)
	if errors.Is(e, os.ErrNotExist) {
		return 0
	}
	if e != nil {
		t.Fatal(e)
	}
	return len(entries)
}
func normalizeJob(j Job, id ID) Job {
	if j.ID == id {
		j.ID = "aabbccdd"
	}
	return j
}
func normalizeOperation(o operation, id ID) operation {
	o.Text = strings.ReplaceAll(o.Text, string(id), "aabbccdd")
	for n, j := range o.Jobs {
		o.Jobs[n] = normalizeJob(j, id)
	}
	for n, v := range o.Armed {
		if v == id {
			o.Armed[n] = "aabbccdd"
		}
	}
	for n, p := range o.Problems {
		o.Problems[n] = strings.ReplaceAll(p, string(id), "aabbccdd")
	}
	for n, f := range o.Fires {
		o.Fires[n].Job = normalizeJob(f.Job, id)
		for k, j := range f.Disk {
			o.Fires[n].Disk[k] = normalizeJob(j, id)
		}
	}
	return o
}
func TestExpressionSourceContracts(t *testing.T) {
	f := contracts(t)
	for _, row := range f.Parser {
		t.Run(row.Expression, func(t *testing.T) {
			e, err := Parse(row.Expression)
			if row.Error != nil {
				if err == nil || err.Error() != *row.Error {
					t.Fatalf("%v want %s", err, *row.Error)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			for n, date := range f.Dates {
				d, err := time.Parse("2006-01-02T15:04:05", date)
				if err != nil {
					t.Fatal(err)
				}
				if e.Matches(d) != row.Matches[n] {
					t.Fatalf("%s differs", date)
				}
			}
		})
	}
}
func TestOperatorLifecycleMatchesActualPython(t *testing.T) {
	f := contracts(t)
	path := filepath.Join(t.TempDir(), "jobs.json")
	var s *Scheduler
	fires := []fireEvidence{}
	resolver := ResolverFunc(func(SessionID) (Runner, error) {
		j := s.Jobs()[0]
		fires = append(fires, fireEvidence{j, disk(t, path)})
		return RunnerFunc(func(context.Context, Invocation) error { return nil }), nil
	})
	s = makeScheduler(t, path, resolver)
	start := schedule(t, s, Request{Session: "session-a", Cron: "* * * * *", Prompt: "中文 run"})
	id := start.Job.ID
	next := now().Add(time.Minute)
	texts := map[string]string{"schedule": start.Render()}
	for _, want := range f.Operations {
		switch want.Name {
		case "list", "empty", "restore":
			if want.Name == "restore" {
				s = makeScheduler(t, path, resolver)
				fires = []fireEvidence{}
			}
			texts[want.Name] = s.ListFor("session-a")
		case "tick", "same-minute":
			s.Tick(now())
			waitRuns(t, s)
		case "next-minute", "disarmed-tick", "restored-same-minute":
			s.Tick(next)
			waitRuns(t, s)
		case "restored-next-minute":
			s.Tick(next.Add(time.Minute))
			waitRuns(t, s)
		case "foreign-arm":
			foreign := SessionID("other")
			texts[want.Name] = s.Arm(id, &foreign)
		case "arm":
			own := SessionID("session-a")
			texts[want.Name] = s.Arm(id, &own)
		case "foreign-cancel", "cancel":
			own := SessionID("session-a")
			if want.Name == "foreign-cancel" {
				own = "other"
			}
			text, e := s.Cancel(id, &own)
			if e != nil {
				t.Fatal(e)
			}
			texts[want.Name] = text
		}
		armed := []ID{}
		for _, j := range s.Jobs() {
			if s.Armed(j.ID) {
				armed = append(armed, j.ID)
			}
		}
		copyFires := make([]fireEvidence, len(fires))
		for n, x := range fires {
			copyFires[n] = fireEvidence{x.Job, append([]Job{}, x.Disk...)}
		}
		got := normalizeOperation(operation{want.Name, texts[want.Name], s.Jobs(), armed, s.Problems().Summary(), copyFires, claims(t, s)}, id)
		if !reflect.DeepEqual(got, want) {
			a, _ := json.Marshal(got)
			b, _ := json.Marshal(want)
			t.Fatalf("%s\ngot %s\nwant %s", want.Name, a, b)
		}
	}
}
func TestOneShotPersistsRemovalBeforeResolving(t *testing.T) {
	f := contracts(t)
	path := filepath.Join(t.TempDir(), "jobs.json")
	var original Job
	fires := []fireEvidence{}
	var s *Scheduler
	s = makeScheduler(t, path, ResolverFunc(func(SessionID) (Runner, error) {
		j := original
		j.LastFired = now().Format("2006-01-02 15:04")
		fires = append(fires, fireEvidence{j, disk(t, path)})
		return RunnerFunc(func(context.Context, Invocation) error { return nil }), nil
	}))
	original = schedule(t, s, Request{Session: "session-a", Cron: "* * * * *", Prompt: "once", Recurring: flag(false)}).Job
	s.Tick(now())
	waitRuns(t, s)
	for n, x := range fires {
		fires[n].Job = normalizeJob(x.Job, original.ID)
	}
	if !reflect.DeepEqual(s.Jobs(), f.OneShot.Jobs) || !reflect.DeepEqual(fires, f.OneShot.Fires) || !reflect.DeepEqual(disk(t, path), f.OneShot.Disk) {
		t.Fatal(s.Jobs(), fires, disk(t, path))
	}
}
func seed(t *testing.T, path string, jobs ...Job) {
	t.Helper()
	b, e := json.Marshal(jobs)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(path, b, 0600); e != nil {
		t.Fatal(e)
	}
}
func TestFailedClaimOrSaveNeverDispatches(t *testing.T) {
	f := contracts(t)
	for _, want := range f.Losses {
		t.Run(want.Kind, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "jobs.json")
			seed(t, path, Job{ID: "lost", Cron: "* * * * *", Prompt: "lost", Session: "session-a", Recurring: want.Kind != "one-shot-save", Durable: true})
			fired := 0
			s := makeScheduler(t, path, ResolverFunc(func(SessionID) (Runner, error) { fired++; return nil, nil }))
			s.ArmAll()
			if want.Kind == "claim" {
				if e := os.WriteFile(s.claims, []byte("blocked"), 0600); e != nil {
					t.Fatal(e)
				}
			} else {
				if e := os.Remove(path); e != nil {
					t.Fatal(e)
				}
				if e := os.Mkdir(path, 0700); e != nil {
					t.Fatal(e)
				}
			}
			s.Tick(now())
			var marker *string
			jobs := s.Jobs()
			if len(jobs) > 0 {
				v := jobs[0].LastFired
				marker = &v
			}
			got := lossCase{want.Kind, len(jobs), marker, fired, s.Problems().Total}
			if !reflect.DeepEqual(got, want) {
				t.Fatal(got, want)
			}
		})
	}
}

type mask struct{}

func (mask) Mask(v string) string { return strings.ReplaceAll(v, "secret", "[MASK]") }
func TestMaskedDiskCopyAndBoundsMatchSource(t *testing.T) {
	f := contracts(t)
	path := filepath.Join(t.TempDir(), "jobs.json")
	s := makeScheduler(t, path, nil)
	s.secrets = mask{}
	j := schedule(t, s, Request{Session: "session-a", Cron: "* * * * *", Prompt: "secret"})
	restored := makeScheduler(t, path, nil)
	if s.Jobs()[0].Prompt != f.Masking.Live || disk(t, path)[0].Prompt != f.Masking.Stored || restored.Jobs()[0].Prompt != f.Masking.Restored {
		t.Fatal("masked live state or unmasked disk")
	}
	summary := s.Problems().Summary()
	for n, p := range summary {
		summary[n] = strings.ReplaceAll(p, string(j.Job.ID), "aabbccdd")
	}
	if !reflect.DeepEqual(summary, f.Masking.Problems) {
		t.Fatal(summary)
	}
	bounded := makeScheduler(t, "", nil)
	_, err := bounded.Schedule(Request{Cron: "* * * * *", Prompt: strings.Repeat("你", 8001), Durable: flag(false)})
	if err == nil || err.Error() != f.Bounds.PromptError {
		t.Fatal(err)
	}
	for n := 0; n < 200; n++ {
		schedule(t, bounded, Request{Session: "s", Cron: "* * * * *", Prompt: "ok", Durable: flag(false)})
	}
	_, err = bounded.Schedule(Request{Cron: "* * * * *", Prompt: "ok", Durable: flag(false)})
	if err == nil || err.Error() != f.Bounds.JobError {
		t.Fatal(err)
	}
	count, e := bounded.CancelForSession("s")
	if e != nil || count != 200 || len(bounded.Jobs()) != 0 {
		t.Fatal(count, e)
	}
}
func TestDispatchAuthorityAndMissingSession(t *testing.T) {
	f := contracts(t)
	var mu sync.Mutex
	seen := []Invocation{}
	s := makeScheduler(t, "", ResolverFunc(func(id SessionID) (Runner, error) {
		if id != "session-a" {
			return nil, nil
		}
		return RunnerFunc(func(_ context.Context, i Invocation) error {
			mu.Lock()
			seen = append(seen, i)
			mu.Unlock()
			return nil
		}), nil
	}))
	s.jobs = []Job{{ID: "invoke", Cron: "* * * * *", Prompt: "go", Session: "session-a", Recurring: true}, {ID: "missing", Cron: "* * * * *", Prompt: "go", Session: "absent", Recurring: true}}
	s.ArmAll()
	s.Tick(now())
	waitRuns(t, s)
	if len(seen) != 1 || seen[0].Prompt() != f.Authority.Runs[0].Prompt || seen[0].Authority() != f.Authority.Runs[0].Authority || seen[0].Session() != "session-a" || !reflect.DeepEqual(s.Problems().Summary(), f.Authority.Problems) {
		t.Fatal(seen, s.Problems())
	}
}
