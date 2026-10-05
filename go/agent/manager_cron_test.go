package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/cron"
	"github.com/luoyjx/mini-loop/go/protocol"
)

type managedCronFixture struct {
	Authority struct {
		Contexts         []RunContextSnapshot
		DistinctIDs      bool `json:"distinct_ids"`
		RunCount         int  `json:"run_count"`
		Status           SessionStatus
		HistoryCount     int                 `json:"history_count"`
		ScheduledPrompt  string              `json:"scheduled_prompt"`
		DefaultTools     []protocol.ToolName `json:"default_tools"`
		DefaultScheduler bool                `json:"default_scheduler"`
	}
	Ownership []struct {
		Name, Action    string
		Bound           bool
		Removed         *bool
		WorkspaceExists bool   `json:"workspace_exists"`
		AliceJobs       int    `json:"alice_jobs"`
		BobJobs         int    `json:"bob_jobs"`
		StoredJobs      int    `json:"stored_jobs"`
		ForeignCancel   string `json:"foreign_cancel"`
		ForeignArm      string `json:"foreign_arm"`
		ChildJobs       int    `json:"child_jobs"`
		SharedService   bool   `json:"shared_service"`
		Problems        []string
	}
	StopLive struct {
		Cancelled, Busy bool
		Status          SessionStatus
		RunCount        int  `json:"run_count"`
		RunningTasks    int  `json:"running_tasks"`
		WorkspaceExists bool `json:"workspace_exists"`
	} `json:"stop_live"`
}

func managedCronContracts(t *testing.T) managedCronFixture {
	t.Helper()
	b, e := os.ReadFile("../testdata/python-managed-cron.json")
	if e != nil {
		t.Fatal(e)
	}
	var f managedCronFixture
	if e = json.Unmarshal(b, &f); e != nil {
		t.Fatal(e)
	}
	return f
}

type cronDoneProvider struct{}

func (cronDoneProvider) Complete(context.Context, protocol.ModelRequest) (protocol.ModelReply, error) {
	return fakeReply([]protocol.Block{protocol.NewTextBlock("done")}, protocol.StopEndTurn), nil
}
func cronTime() time.Time { return time.Date(2026, 10, 5, 12, 30, 0, 0, time.Local) }
func waitCron(t *testing.T, m *SessionManager) {
	t.Helper()
	ctx, c := context.WithTimeout(context.Background(), 5*time.Second)
	defer c()
	if e := m.cron.Wait(ctx); e != nil {
		t.Fatal(e)
	}
}
func cronJobCount(m *SessionManager, id SessionID) int {
	n := 0
	for _, j := range m.cron.Jobs() {
		if SessionID(j.Session) == id {
			n++
		}
	}
	return n
}
func TestManagedCronCreatesFreshUntrustedTurnMatchesSource(t *testing.T) {
	want := managedCronContracts(t).Authority
	var mu sync.Mutex
	contexts := []RunContextSnapshot{}
	cfg := managerTestConfig(t.TempDir(), cronDoneProvider{})
	cfg.Services.UserPromptHooks = []UserPromptHook{promptHookFunc(func(_ context.Context, v TurnContext, _ string) (*string, error) {
		mu.Lock()
		contexts = append(contexts, v.Authority.RunContext.Snapshot())
		mu.Unlock()
		return nil, nil
	})}
	m := makeManager(t, cfg)
	s := createManaged(t, m, CreateSessionRequest{Owner: "alice"})
	job, e := m.cron.Schedule(cron.Request{Session: cron.SessionID(s.ID()), Cron: "30 12 5 10 *", Prompt: "scheduled"})
	if e != nil {
		t.Fatal(e)
	}
	actor := ActorID("human")
	human, e := ExplicitHumanRunContext(HumanRunConfig{ActorID: &actor, ApprovedCapabilities: []RunCapability{CapabilityPersonalSkillCaptureSource}})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.RunWithContext(context.Background(), "human", human); e != nil {
		t.Fatal(e)
	}
	m.cron.Tick(cronTime())
	waitCron(t, m)
	ids := map[MessageID]bool{}
	for n, c := range contexts {
		if c.MessageID == "" || ids[c.MessageID] {
			t.Fatal("reused or absent run identity")
		}
		ids[c.MessageID] = true
		c.MessageID = "<message>"
		contexts[n] = c
	}
	if !reflect.DeepEqual(contexts, want.Contexts) || want.DistinctIDs != (len(ids) == len(contexts)) {
		t.Fatal(contexts, want.Contexts)
	}
	info := s.Info()
	messages := s.Messages()
	last, ok := messages[len(messages)-2].Content.Plain()
	if !ok {
		t.Fatal("scheduled input missing")
	}
	last = strings.ReplaceAll(last, string(job.Job.ID), "<job>")
	names := s.core.gate.CatalogNames()
	slices.Sort(names)
	slices.Sort(want.DefaultTools)
	if info.RunCount != want.RunCount || info.Status != want.Status || len(messages) != want.HistoryCount || last != want.ScheduledPrompt || !slices.Equal(names, want.DefaultTools) || (m.CronScheduler() != nil) != want.DefaultScheduler {
		t.Fatal(info, last, names)
	}
}
func TestManagedCronOwnershipDeletePreserveForkAndStopMatchSource(t *testing.T) {
	f := managedCronContracts(t)
	for _, want := range f.Ownership {
		t.Run(want.Name, func(t *testing.T) {
			root := t.TempDir()
			checkout := filepath.Join(root, "checkout")
			if e := os.Mkdir(checkout, 0700); e != nil {
				t.Fatal(e)
			}
			cfg := managerTestConfig(filepath.Join(root, "ws"), cronDoneProvider{})
			cfg.BindableRoots = []string{checkout}
			m := makeManager(t, cfg)
			req := CreateSessionRequest{Owner: "alice"}
			if want.Bound {
				req.Workspace = &checkout
			}
			a := createManaged(t, m, req)
			b := createManaged(t, m, CreateSessionRequest{Owner: "bob"})
			aj, e := m.ScheduleCron("alice", a.ID(), ScheduleCronRequest{Cron: "0 0 31 2 *", Prompt: "alice"})
			if e != nil {
				t.Fatal(e)
			}
			if _, e = m.ScheduleCron("bob", b.ID(), ScheduleCronRequest{Cron: "0 0 31 2 *", Prompt: "bob"}); e != nil {
				t.Fatal(e)
			}
			if got, e := m.CancelCron("bob", b.ID(), aj.Job.ID); e != nil || strings.ReplaceAll(got, string(aj.Job.ID), "<job>") != want.ForeignCancel {
				t.Fatal(got, e)
			}
			if got, e := m.ArmCron("bob", b.ID(), aj.Job.ID); e != nil || strings.ReplaceAll(got, string(aj.Job.ID), "<job>") != want.ForeignArm {
				t.Fatal(got, e)
			}
			var child *ManagedSession
			if want.Action == "fork" {
				child, e = m.Fork(context.Background(), "alice", a.ID())
				if e != nil {
					t.Fatal(e)
				}
			}
			if want.Action == "stop" {
				if e = m.Stop(context.Background()); e != nil {
					t.Fatal(e)
				}
			} else {
				removed, e := m.Delete("alice", a.ID(), DeleteSessionOptions{PreserveWorkspace: want.Action == "preserve"})
				if e != nil || want.Removed == nil || removed != *want.Removed {
					t.Fatal(removed, e)
				}
				if e = m.WaitCleanup(context.Background()); e != nil {
					t.Fatal(e)
				}
			}
			_, statErr := os.Stat(a.core.workspace)
			if (statErr == nil) != want.WorkspaceExists || cronJobCount(m, a.ID()) != want.AliceJobs || cronJobCount(m, b.ID()) != want.BobJobs {
				t.Fatal(statErr, m.cron.Jobs())
			}
			data, e := os.ReadFile(filepath.Join(cfg.WorkspaceRoot, ".cron.json"))
			if e != nil {
				t.Fatal(e)
			}
			var stored []cron.Job
			if e = json.Unmarshal(data, &stored); e != nil || len(stored) != want.StoredJobs {
				t.Fatal(len(stored), e)
			}
			if child != nil && cronJobCount(m, child.ID()) != want.ChildJobs {
				t.Fatal("fork borrowed jobs")
			}
			if !want.SharedService || !reflect.DeepEqual(m.cron.Problems().Summary(), want.Problems) {
				t.Fatal(m.cron.Problems())
			}
		})
	}
}
func TestManagerCronScopeAdmissionClosesWithDeletionAndStop(t *testing.T) {
	m := makeManager(t, managerTestConfig(t.TempDir(), cronDoneProvider{}))
	s := createManaged(t, m, CreateSessionRequest{Owner: "alice"})
	request := ScheduleCronRequest{Cron: "0 0 31 2 *", Prompt: "owned"}
	if _, e := m.ScheduleCron("bob", s.ID(), request); !errors.Is(e, ErrSessionNotFound) {
		t.Fatal(e)
	}
	if _, e := m.CronJobs("bob", s.ID()); !errors.Is(e, ErrSessionNotFound) {
		t.Fatal(e)
	}
	job, e := m.ScheduleCron("alice", s.ID(), request)
	if e != nil {
		t.Fatal(e)
	}
	jobs, e := m.CronJobs("alice", s.ID())
	if e != nil || len(jobs) != 1 || !jobs[0].Armed {
		t.Fatal(jobs, e)
	}
	jobs[0].Prompt = "tampered"
	again, _ := m.CronJobs("alice", s.ID())
	if again[0].Prompt == "tampered" {
		t.Fatal("borrowed state")
	}
	if _, e := m.Delete("bob", s.ID(), DeleteSessionOptions{}); !errors.Is(e, ErrSessionNotFound) || len(m.cron.Jobs()) != 1 {
		t.Fatal(e)
	}
	if _, e = m.Delete("alice", s.ID(), DeleteSessionOptions{}); e != nil {
		t.Fatal(e)
	}
	if _, e = m.ScheduleCron("alice", s.ID(), request); !errors.Is(e, ErrSessionNotFound) {
		t.Fatal("deleted session readmitted schedule", e)
	}
	other := createManaged(t, m, CreateSessionRequest{Owner: "alice"})
	if e = m.Stop(context.Background()); e != nil {
		t.Fatal(e)
	}
	if _, e = m.ScheduleCron("alice", other.ID(), request); !errors.Is(e, ErrManagerStopped) {
		t.Fatal(e)
	}
	if _, e = m.ArmCron("alice", other.ID(), job.Job.ID); !errors.Is(e, ErrManagerStopped) {
		t.Fatal(e)
	}
	if e = m.Start(); !errors.Is(e, ErrManagerStopped) {
		t.Fatal(e)
	}
}
func TestManagerStopsAndJoinsLiveCronTurnMatchesSource(t *testing.T) {
	want := managedCronContracts(t).StopLive
	p := newDrainingProvider()
	m := makeManager(t, managerTestConfig(t.TempDir(), p))
	s := createManaged(t, m, CreateSessionRequest{Owner: "alice"})
	if _, e := m.cron.Schedule(cron.Request{Session: cron.SessionID(s.ID()), Cron: "* * * * *", Prompt: "wait", Durable: boolPointer(false)}); e != nil {
		t.Fatal(e)
	}
	m.cron.Tick(cronTime())
	<-p.entered
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if e := m.Stop(ctx); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	select {
	case <-p.cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("cron run never cancelled")
	}
	if !s.Info().Busy {
		t.Fatal("stop did not retain pending holder")
	}
	close(p.release)
	if e := m.Stop(context.Background()); e != nil {
		t.Fatal(e)
	}
	waitCron(t, m)
	info := s.Info()
	_, statErr := os.Stat(s.core.workspace)
	if info.Busy != want.Busy || info.Status != want.Status || info.RunCount != want.RunCount || (statErr == nil) != want.WorkspaceExists || !want.Cancelled || want.RunningTasks != 0 {
		t.Fatal(info, statErr)
	}
}
func boolPointer(v bool) *bool { return &v }
func TestCronQueuedBehindAnotherTurnCannotReenterAfterStop(t *testing.T) {
	p := newDrainingProvider()
	m := makeManager(t, managerTestConfig(t.TempDir(), p))
	s := createManaged(t, m, CreateSessionRequest{Owner: "alice"})
	done := make(chan error, 1)
	go func() { _, e := s.Run(context.Background(), "holder"); done <- e }()
	<-p.entered
	if _, e := m.cron.Schedule(cron.Request{Session: cron.SessionID(s.ID()), Cron: "* * * * *", Prompt: "queued", Durable: boolPointer(false)}); e != nil {
		t.Fatal(e)
	}
	m.cron.Tick(cronTime())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_ = m.Stop(ctx)
	<-p.cancelled
	close(p.release)
	if e := m.Stop(context.Background()); e != nil {
		t.Fatal(e)
	}
	if e := <-done; !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	if s.Info().RunCount != 1 {
		t.Fatal("queued cron entered closed admission")
	}
	waitCron(t, m)
}
func TestCronDeleteSaveFailureStillDrainsAndReportsCleanup(t *testing.T) {
	root := t.TempDir()
	m := makeManager(t, managerTestConfig(root, cronDoneProvider{}))
	a := createManaged(t, m, CreateSessionRequest{Owner: "alice"})
	_, e := m.ScheduleCron("alice", a.ID(), ScheduleCronRequest{Cron: "0 0 31 2 *", Prompt: "lost"})
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(m.WorkspaceRoot(), ".cron.json")
	if e = os.Remove(path); e != nil {
		t.Fatal(e)
	}
	if e = os.Mkdir(path, 0700); e != nil {
		t.Fatal(e)
	}
	if removed, e := m.Delete("alice", a.ID(), DeleteSessionOptions{}); !removed || e != nil {
		t.Fatal(removed, e)
	}
	if e = m.WaitCleanup(context.Background()); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(a.core.workspace); !os.IsNotExist(e) {
		t.Fatal(e)
	}
	if len(m.cron.Jobs()) != 0 || len(m.CleanupErrors()) != 1 || m.CleanupErrors()[0].Workspace != path {
		t.Fatal(m.cron.Jobs(), m.CleanupErrors())
	}
}

type nativeCronProvider struct{ stage int }

func (p *nativeCronProvider) Complete(context.Context, protocol.ModelRequest) (protocol.ModelReply, error) {
	p.stage++
	if p.stage == 1 {
		return fakeReply([]protocol.Block{protocol.NewToolUse("cron-bash", protocol.BashToolInput(protocol.BashInput{Command: "printf '%s' $$ > cron.pid; touch cron-started; sleep 30"}))}, protocol.StopToolUse), nil
	}
	return fakeReply([]protocol.Block{protocol.NewTextBlock("done")}, protocol.StopEndTurn), nil
}
func TestStoppingCronManagedTurnReapsNativeForegroundShell(t *testing.T) {
	m := makeManager(t, managerTestConfig(t.TempDir(), &nativeCronProvider{}))
	s := createManaged(t, m, CreateSessionRequest{Owner: "alice"})
	if _, e := m.cron.Schedule(cron.Request{Session: cron.SessionID(s.ID()), Cron: "* * * * *", Prompt: "native", Durable: boolPointer(false)}); e != nil {
		t.Fatal(e)
	}
	m.cron.Tick(cronTime())
	deadline := time.Now().Add(5 * time.Second)
	marker := filepath.Join(s.core.workspace, "cron-started")
	for {
		if _, e := os.Stat(marker); e == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("native cron command never ran")
		}
		time.Sleep(time.Millisecond)
	}
	b, e := os.ReadFile(filepath.Join(s.core.workspace, "cron.pid"))
	if e != nil {
		t.Fatal(e)
	}
	pid, e := strconv.Atoi(string(b))
	if e != nil {
		t.Fatal(e)
	}
	if e = syscall.Kill(pid, 0); e != nil {
		t.Fatal("native PID was never live", e)
	}
	if e = m.Stop(context.Background()); e != nil {
		t.Fatal(e)
	}
	if e = syscall.Kill(pid, 0); e == nil {
		t.Fatal("cron shell survived manager stop")
	}
}
