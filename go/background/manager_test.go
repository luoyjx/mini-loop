package background

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/secrets"
	"github.com/luoyjx/mini-loop/go/shell"
)

type sourceCheck struct {
	ID     ID
	Output string
}
type sourceState struct {
	Listing     string
	Live        int
	Checks      []sourceCheck
	LedgerAfter []string `json:"ledger_after"`
	Done        []Notification
}
type sourceCommand struct {
	sourceState
	Name, Command, Started string
	Limit                  int
	Timeout                *int
}
type sourceBackground struct {
	CounterDigits struct {
		UnicodeVersion string `json:"unicode_version"`
		Cases          []struct {
			Digits string
			Value  *string
		}
	} `json:"counter_digits"`
	Commands   []sourceCommand
	Unrecorded struct {
		sourceState
		Started string
	}
	Retained struct {
		sourceState
		Starts []string
	}
	Cancelled struct {
		sourceState
		Started string
	}
	OrphanSeeds []struct {
		ID  ID
		Raw string
	} `json:"orphan_seeds"`
	Adopted     sourceState
	NextStarted string `json:"next_started"`
	Batch       struct {
		Listing  string
		Messages []struct{ Role, Content string }
		Events   []struct {
			Type, Agent           string
			Count, Dropped, Depth int
		}
		Drained []Notification
	}
	Heuristics []struct {
		Command          string
		Explicit, Result bool
	}
}

func TestActualSourceDecimalCounters(t *testing.T) {
	f := sourceFixture(t)
	if f.CounterDigits.UnicodeVersion != "14.0.0" || len(f.CounterDigits.Cases) != 74 {
		t.Fatal("source Unicode decimal contract changed")
	}
	for _, row := range f.CounterDigits.Cases {
		value, ok := decimalCounter(row.Digits)
		if row.Value == nil {
			if ok {
				t.Fatalf("accepted nondecimal source digits %q", row.Digits)
			}
		} else if !ok || value.String() != *row.Value {
			t.Fatalf("decimal counter differs for %q: %v", row.Digits, value)
		}
	}
}

func sourceFixture(t *testing.T) sourceBackground {
	t.Helper()
	data, err := os.ReadFile("../testdata/python-background.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture sourceBackground
	if err = json.Unmarshal(data, &fixture); err != nil || len(fixture.Commands) != 12 {
		t.Fatal("actual source corpus missing", err)
	}
	return fixture
}
func newManager(t *testing.T, config Config) *Manager {
	t.Helper()
	if config.Shell.Workspace == "" {
		config.Shell.Workspace = t.TempDir()
	}
	m, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
		defer cancel()
		if err := m.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	return m
}
func run(t *testing.T, m *Manager, request Request) Started {
	t.Helper()
	started, err := m.Run(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if err = m.Wait(ctx, started.ID); err != nil {
		t.Fatal(err)
	}
	return started
}
func checkState(t *testing.T, m *Manager, state sourceState, drain bool) {
	t.Helper()
	if m.Check("") != state.Listing || m.LiveCount() != state.Live {
		t.Fatal("source status differs", m.Check(""), state.Listing)
	}
	for _, check := range state.Checks {
		actual := strings.ReplaceAll(m.Check(check.ID), strconv.Itoa(os.Getpid()), "<PID>")
		if actual != check.Output {
			t.Fatal(check.ID, actual, check.Output)
		}
	}
	paths, _ := filepath.Glob(filepath.Join(m.ledgerDir, "bg_*.json"))
	actual := []string{}
	for _, path := range paths {
		actual = append(actual, filepath.Base(path))
	}
	if !reflect.DeepEqual(actual, state.LedgerAfter) {
		t.Fatal("source ledger differs", actual, state.LedgerAfter)
	}
	if drain {
		done := m.Drain()
		for i := range done {
			done[i].Result = strings.ReplaceAll(done[i].Result, strconv.Itoa(os.Getpid()), "<PID>")
		}
		if !reflect.DeepEqual(done, state.Done) {
			t.Fatal("source completion queue differs", done, state.Done)
		}
	}
}
func TestActualSourceCommandsAndRendering(t *testing.T) {
	fixture := sourceFixture(t)
	for _, row := range fixture.Commands {
		t.Run(row.Name, func(t *testing.T) {
			m := newManager(t, Config{Shell: shell.Config{CaptureLimit: row.Limit}, DefaultTimeout: 3 * time.Second})
			var timeout *time.Duration
			if row.Timeout != nil {
				value := time.Duration(*row.Timeout) * time.Second
				timeout = &value
			}
			if start := run(t, m, Request{row.Command, timeout}); start.Render() != row.Started {
				t.Fatal("source start differs", start.Render(), row.Started)
			}
			checkState(t, m, row.sourceState, true)
		})
	}
	for _, row := range fixture.Heuristics {
		if ShouldRunBackground(row.Command, row.Explicit) != row.Result {
			t.Fatal("source heuristic differs", row)
		}
	}
}
func TestActualSourceRetentionAndUnrecordedExecution(t *testing.T) {
	f := sourceFixture(t)
	zero := 2
	m := newManager(t, Config{MaxResultsRetained: &zero})
	for i, start := range f.Retained.Starts {
		if run(t, m, Request{Command: "printf result-" + strconv.Itoa(i)}).Render() != start {
			t.Fatal("source counter differs")
		}
	}
	checkState(t, m, f.Retained.sourceState, true)
	r, ok := m.Get("bg_0001")
	if !ok || r.Result == nil {
		t.Fatal("shed record missing")
	}
	*r.Result = "tampered"
	if strings.Contains(m.Check(r.ID), "tampered") {
		t.Fatal("record snapshot mutated manager")
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".background"), []byte("blocked"), 0600); err != nil {
		t.Fatal(err)
	}
	m = newManager(t, Config{Shell: shell.Config{Workspace: root}})
	if start := run(t, m, Request{Command: "printf ok"}); start.Render() != f.Unrecorded.Started || start.Recorded {
		t.Fatal("recording fault changed execution", start)
	}
	checkState(t, m, f.Unrecorded.sourceState, true)
}
func TestActualSourceOrphansAndNotificationBounds(t *testing.T) {
	f := sourceFixture(t)
	root := filepath.Join(t.TempDir(), "[source-scope]")
	ledger := filepath.Join(root, ".background")
	if err := os.MkdirAll(ledger, 0700); err != nil {
		t.Fatal(err)
	}
	for _, seed := range f.OrphanSeeds {
		data := strings.ReplaceAll(seed.Raw, "<PID>", strconv.Itoa(os.Getpid()))
		if err := os.WriteFile(filepath.Join(ledger, string(seed.ID)+".json"), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	m := newManager(t, Config{Shell: shell.Config{Workspace: root}})
	checkState(t, m, f.Adopted, true)
	if start := run(t, m, Request{Command: "printf fresh"}); start.Render() != f.NextStarted {
		t.Fatal("adopted counter was reused", start)
	}
	root = t.TempDir()
	ledger = filepath.Join(root, ".background")
	os.Mkdir(ledger, 0700)
	for i := 0; i < 53; i++ {
		data, _ := json.Marshal(ledgerRecord{Command: "cmd-" + strconv.Itoa(i)})
		if err := os.WriteFile(filepath.Join(ledger, fmt.Sprintf("bg_%04d.json", i+1)), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	m = newManager(t, Config{Shell: shell.Config{Workspace: root}})
	if m.Check("") != f.Batch.Listing {
		t.Fatal("source listing bound differs")
	}
	batch := m.DrainBatch()
	if len(f.Batch.Messages) != 1 || batch.Render() != f.Batch.Messages[0].Content || len(batch.Notifications) != f.Batch.Events[0].Count || batch.Dropped != f.Batch.Events[0].Dropped {
		t.Fatal("actual source injector projection differs", batch.Dropped)
	}
	if !reflect.DeepEqual(m.Drain(), f.Batch.Drained) {
		t.Fatal("notification queue was not consumed")
	}
}
func awaitPID(t *testing.T, m *Manager, id ID) shell.ProcessID {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if record, ok := readLedger(filepath.Join(m.ledgerDir, string(id)+".json")); ok && record.PID != nil {
			return *record.PID
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("background process never started")
	return 0
}
func TestSourceCancellationAndCallerLifetime(t *testing.T) {
	f := sourceFixture(t)
	m := newManager(t, Config{})
	ctx, cancel := context.WithCancel(context.Background())
	start, err := m.Run(ctx, Request{Command: "sleep 30"})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	pid := awaitPID(t, m, start.ID)
	if m.LiveCount() != 1 || !processAlive(pid) {
		t.Fatal("caller cancellation stopped admitted work")
	}
	if start.Render() != f.Cancelled.Started {
		t.Fatal("source cancellation start differs")
	}
	if err = m.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if processAlive(pid) {
		t.Fatal("close did not reap background shell")
	}
	checkState(t, m, f.Cancelled.sourceState, true)
	if _, err = m.Run(ctx, Request{Command: "printf denied"}); err != context.Canceled {
		t.Fatal("cancelled admission accepted")
	}
	// Source permits subsequent admission after Close when its caller owns scope.
	run(t, m, Request{Command: "printf fresh"})
}
func TestGuardsSecretsAndImmediateCancellation(t *testing.T) {
	registry := secrets.New(secrets.Config{})
	registry.RegisterValue("BACKGROUND_API_KEY", "credential-long-value")
	t.Setenv("BACKGROUND_API_KEY", "ambient-hidden")
	m := newManager(t, Config{Shell: shell.Config{Secrets: registry}})
	for _, command := range []string{"env", "printf '%s' \"$BACKGROUND_API_KEY\"", "printf credential-; printf long-value >&2"} {
		s := run(t, m, Request{Command: command})
		r, _ := m.Get(s.ID)
		if r.Result == nil || strings.Contains(*r.Result, "credential-long-value") || strings.Contains(*r.Result, "ambient-hidden") || strings.Contains(*r.Result, "BACKGROUND_API_KEY=") {
			t.Fatal("background credential escaped", r)
		}
		if strings.HasPrefix(command, "printf") && *r.Result != secrets.Mask {
			t.Fatal("selected/split secret was not masked", *r.Result)
		}
	}
	if _, err := m.Run(context.Background(), Request{Command: "rm  -rf  /"}); err == nil {
		t.Fatal("dangerous command accepted")
	}
	if m.LiveCount() != 0 {
		t.Fatal("refusal allocated work")
	}
	m = newManager(t, Config{})
	s, err := m.Run(context.Background(), Request{Command: "sleep 30"})
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if m.Check(s.ID) != "[cancelled] Cancelled" {
		t.Fatal("immediate close stranded running metadata")
	}
	paths, _ := filepath.Glob(filepath.Join(m.ledgerDir, "*.json"))
	if len(paths) != 0 {
		t.Fatal("immediate close left an orphan record")
	}
}

func TestConcurrentAdmissionKeepsUniqueIDsAndJoinedResults(t *testing.T) {
	m := newManager(t, Config{})
	starts := make(chan Started, 24)
	faults := make(chan error, 24)
	for i := 0; i < 24; i++ {
		go func(index int) {
			start, err := m.Run(context.Background(), Request{Command: fmt.Sprintf("printf result-%d", index)})
			if err != nil {
				faults <- err
				return
			}
			starts <- start
		}(i)
	}
	seen := make(map[ID]bool)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	for i := 0; i < 24; i++ {
		select {
		case err := <-faults:
			t.Fatal(err)
		case start := <-starts:
			if seen[start.ID] {
				t.Fatal("concurrent admission reused task id", start.ID)
			}
			seen[start.ID] = true
			if err := m.Wait(ctx, start.ID); err != nil {
				t.Fatal(err)
			}
			if record, ok := m.Get(start.ID); !ok || record.Status != Completed || record.Result == nil || !strings.HasPrefix(*record.Result, "result-") {
				t.Fatal("concurrent task lost result", record)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	if m.LiveCount() != 0 || len(m.Drain()) != 24 {
		t.Fatal("concurrent completions were not joined and queued")
	}
}
