package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/background"
	"github.com/luoyjx/mini-loop/go/protocol"
)

type managedBackgroundFixture struct {
	Cases []struct {
		Name, Action, Output string
		Status               background.Status
		Bound                bool
		Removed              *bool
		WorkspaceExists      bool `json:"workspace_exists"`
		LedgerExists         bool `json:"ledger_exists"`
		FreshFork            bool `json:"fresh_fork"`
		DifferentForkRoot    bool `json:"different_fork_root"`
		Tools                []protocol.ToolName
		CleanupErrors        []CleanupError `json:"cleanup_errors"`
	}
}

type managedBackgroundProvider struct{ stage int }

func (p *managedBackgroundProvider) Complete(context.Context, protocol.ModelRequest) (protocol.ModelReply, error) {
	p.stage++
	if p.stage == 1 {
		return fakeReply([]protocol.Block{protocol.NewToolUse("managed-bg", protocol.BackgroundRunToolInput(protocol.BackgroundRunInput{Command: "touch started; sleep 30 & wait"}))}, protocol.StopToolUse), nil
	}
	return fakeReply([]protocol.Block{protocol.NewTextBlock("started")}, protocol.StopEndTurn), nil
}

func waitManagedBackgroundStarted(t *testing.T, root string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(root, "started")); err == nil {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("native managed background did not start")
}

func TestManagedBackgroundMatchesSourceDeleteStopAndFreshFork(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-managed-background.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture managedBackgroundFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 5 {
		t.Fatal("source ownership cases missing")
	}
	for _, c := range fixture.Cases {
		t.Run(c.Name, func(t *testing.T) {
			root := t.TempDir()
			checkout := filepath.Join(root, "checkout")
			if err := os.Mkdir(checkout, 0700); err != nil {
				t.Fatal(err)
			}
			config := managerTestConfig(filepath.Join(root, "ws"), &managedBackgroundProvider{})
			config.BindableRoots = []string{checkout}
			config.Services.BackgroundTools = true
			manager := makeManager(t, config)
			request := CreateSessionRequest{Owner: "alice"}
			if c.Bound {
				request.Workspace = &checkout
			}
			s := createManaged(t, manager, request)
			if output, err := s.Run(context.Background(), "start"); err != nil || output != c.Output {
				t.Fatal(output, err, c.Output)
			}
			waitManagedBackgroundStarted(t, s.core.workspace)
			service := backgroundManager(t, s.core)
			if service.LiveCount() != 1 {
				t.Fatal("task not live before cleanup")
			}
			names, want := s.core.gate.catalog.Names(), slices.Clone(c.Tools)
			slices.Sort(names)
			slices.Sort(want)
			if !slices.Equal(names, want) {
				t.Fatal(names, want)
			}
			if c.Action == "fork" {
				child, err := manager.Fork(context.Background(), "alice", s.ID())
				if err != nil {
					t.Fatal(err)
				}
				if fresh := !child.core.backgroundInitialized(); fresh != c.FreshFork {
					t.Fatal("fork borrowed background state")
				}
				if different := child.core.workspace != s.core.workspace; different != c.DifferentForkRoot {
					t.Fatal("fork reused parent ledger")
				}
				backgroundDispatch(t, child.core, protocol.CheckBackgroundToolInput(protocol.CheckBackgroundInput{}))
				if child.core.background.manager == service || child.core.background.manager.LiveCount() != 0 {
					t.Fatal("fork borrowed parent tasks")
				}
			}
			if c.Action == "stop" {
				if err := manager.Stop(context.Background()); err != nil {
					t.Fatal(err)
				}
			} else {
				removed, err := manager.Delete("alice", s.ID(), DeleteSessionOptions{PreserveWorkspace: c.Action == "preserve"})
				if err != nil || c.Removed == nil || removed != *c.Removed {
					t.Fatal(removed, err)
				}
				if err := manager.WaitCleanup(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			if got := service.Check("bg_0001"); got != "["+string(c.Status)+"] Cancelled" || service.LiveCount() != 0 {
				t.Fatal(got, service.LiveCount())
			}
			_, err = os.Stat(s.core.workspace)
			if exists := err == nil; exists != c.WorkspaceExists {
				t.Fatal("workspace retention differs", exists, c.WorkspaceExists)
			}
			_, err = os.Stat(filepath.Join(s.core.workspace, ".background/bg_0001.json"))
			if exists := err == nil; exists != c.LedgerExists {
				t.Fatal("terminal task ledger survived", exists)
			}
			if len(manager.CleanupErrors()) != len(c.CleanupErrors) {
				t.Fatal(manager.CleanupErrors())
			}
			if _, err = s.Run(context.Background(), "late"); err == nil {
				t.Fatal("retired session admitted more work")
			}
		})
	}
}

func TestManagedDeleteDrainsTurnBeforeJoiningBackgroundAndReclaiming(t *testing.T) {
	p := newDrainingProvider()
	config := managerTestConfig(t.TempDir(), p)
	config.Services.BackgroundTools = true
	m := makeManager(t, config)
	s := createManaged(t, m, CreateSessionRequest{Owner: "owner"})
	backgroundDispatch(t, s.core, protocol.BackgroundRunToolInput(protocol.BackgroundRunInput{Command: "touch started; sleep 30 & wait"}))
	waitManagedBackgroundStarted(t, s.core.workspace)
	service := backgroundManager(t, s.core)
	done := make(chan error, 1)
	go func() { _, err := s.Run(context.Background(), "blocked"); done <- err }()
	receiveSignal(t, p.entered)
	if removed, err := m.Delete("owner", s.ID(), DeleteSessionOptions{}); err != nil || !removed {
		t.Fatal(removed, err)
	}
	receiveSignal(t, p.cancelled)
	if service.LiveCount() != 1 {
		t.Fatal("background closed while turn still owned services")
	}
	if _, err := os.Stat(s.core.workspace); err != nil {
		t.Fatal("workspace removed with live turn", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if err := m.Stop(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("stop ignored retiring turn", err)
	}
	close(p.release)
	if err := receiveRun(t, done); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := m.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if service.LiveCount() != 0 || !strings.HasPrefix(service.Check("bg_0001"), "[cancelled]") {
		t.Fatal(service.Check("bg_0001"))
	}
	if _, err := os.Stat(s.core.workspace); !os.IsNotExist(err) {
		t.Fatal("joined workspace retained", err)
	}
}

func TestManagerBackgroundActivationIsLazyAndRejectsCustomShell(t *testing.T) {
	config := managerTestConfig(t.TempDir(), &recordingProvider{})
	config.Services.BackgroundTools = true
	m := makeManager(t, config)
	s := createManaged(t, m, CreateSessionRequest{Owner: "owner"})
	if s.core.background == nil || s.core.backgroundInitialized() {
		t.Fatal("activation borrowed/eagerly built service")
	}
	if err := m.Stop(context.Background()); err != nil || s.core.backgroundInitialized() {
		t.Fatal("stop created a service", err)
	}
	config = managerTestConfig(t.TempDir(), &recordingProvider{})
	config.Services.BackgroundTools = true
	config.Services.BashFactory = bashFactoryFunc(func(context.Context, SessionBinding) (BashExecutor, error) { return echoExecutor{}, nil })
	m = makeManager(t, config)
	if _, err := m.Create(context.Background(), CreateSessionRequest{Owner: "owner"}); err == nil || !strings.Contains(err.Error(), "native shell") {
		t.Fatal(err)
	}
}

type lateBackgroundProvider struct {
	stage            int
	entered, release chan struct{}
}

func (p *lateBackgroundProvider) Complete(ctx context.Context, _ protocol.ModelRequest) (protocol.ModelReply, error) {
	p.stage++
	if p.stage == 1 {
		close(p.entered)
		select {
		case <-p.release:
		case <-ctx.Done():
			return protocol.ModelReply{}, ctx.Err()
		}
		return fakeReply([]protocol.Block{protocol.NewToolUse("late-bg", protocol.BackgroundRunToolInput(protocol.BackgroundRunInput{Command: "sleep 30"}))}, protocol.StopToolUse), nil
	}
	return fakeReply([]protocol.Block{protocol.NewTextBlock("done")}, protocol.StopEndTurn), nil
}

func TestDeleteJoinsBackgroundCreatedByDrainingTurn(t *testing.T) {
	p := &lateBackgroundProvider{entered: make(chan struct{}), release: make(chan struct{})}
	config := managerTestConfig(t.TempDir(), p)
	config.Services.BackgroundTools = true
	config.DeleteGrace = 5 * time.Second
	m := makeManager(t, config)
	s := createManaged(t, m, CreateSessionRequest{Owner: "owner"})
	done := make(chan error, 1)
	go func() { _, err := s.Run(context.Background(), "late background"); done <- err }()
	receiveSignal(t, p.entered)
	if s.core.backgroundInitialized() {
		t.Fatal("service was not lazy")
	}
	if removed, err := m.Delete("owner", s.ID(), DeleteSessionOptions{}); err != nil || !removed {
		t.Fatal(removed, err)
	}
	close(p.release)
	if err := receiveRun(t, done); err != nil {
		t.Fatal(err)
	}
	if err := m.WaitCleanup(context.Background()); err != nil {
		t.Fatal(err)
	}
	service := backgroundManager(t, s.core)
	if service.LiveCount() != 0 || service.Check("bg_0001") != "[cancelled] Cancelled" {
		t.Fatal("late service escaped cleanup", service.Check("bg_0001"))
	}
	if _, err := os.Stat(s.core.workspace); !os.IsNotExist(err) {
		t.Fatal("late service prevented reclamation", err)
	}
}
