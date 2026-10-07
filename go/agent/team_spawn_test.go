package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/skills"
	"github.com/luoyjx/mini-loop/go/teams"
)

func spawnMember(t *testing.T, m *SessionManager, parent *ManagedSession, name teams.MemberName) *ManagedSession {
	t.Helper()
	spawn, err := m.SpawnTeammate(context.Background(), parent.Owner(), parent.ID(), SpawnTeammateRequest{Name: name, Role: "research", Prompt: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	child, err := m.Get(parent.Owner(), spawn.Session)
	if err != nil {
		t.Fatal(err)
	}
	return child
}

func TestSpawnTeammatePinsResourcesAndRunsWithPeerProvenance(t *testing.T) {
	cfg := managerTestConfig(t.TempDir(), cronDoneProvider{})
	cfg.Defaults.Model = "manager-model"
	cfg.Services.TeamTools, cfg.Services.TaskTools = true, true
	cfg.Services.SystemBuilder = FixedSystem("manager system")
	cfg.Services.UserResources = ownerResourceResolver(t, t.TempDir(), skills.EmptyCatalog())
	m := makeManager(t, cfg)
	model, system := "parent-model", "parent system"
	parent := createManaged(t, m, CreateSessionRequest{Owner: "alice", Model: &model, System: &system, PermissionMode: ModeReadonly})
	human, err := ExplicitHumanRunContext(HumanRunConfig{StampedBy: "test", Channel: "cli", ApprovedCapabilities: []RunCapability{CapabilityWorkflowLaunch, CapabilityPersonalSkillCaptureSource}})
	if err != nil {
		t.Fatal(err)
	}
	// Subsequent construction must use the parent's snapshot even when resolving
	// current resources would yield a different owner bundle. Restore before exposing the manager again.
	resolver := m.config.Services.UserResources
	m.config.Services.UserResources = ownerResourceResolver(t, t.TempDir(), skills.EmptyCatalog())
	spawn, err := m.SpawnTeammate(context.Background(), "alice", parent.ID(), SpawnTeammateRequest{Name: "researcher", Role: "research", Prompt: "hello", RunContext: human})
	m.config.Services.UserResources = resolver
	if err != nil {
		t.Fatal(err)
	}
	child, err := m.Get("alice", spawn.Session)
	if err != nil {
		t.Fatal(err)
	}
	receiveSignal(t, child.teamRun.initialDone)
	if child.core.workspace != parent.core.workspace || child.core.owner != parent.core.owner || child.workspaceBound != parent.workspaceBound || child.core.skills != parent.core.skills || child.core.memory != parent.core.memory {
		t.Fatal("parent binding/resource snapshot not inherited")
	}
	if child.core.ownerResources == nil || child.core.ownerResources.Memory() != parent.core.ownerResources.Memory() || child.core.model != "manager-model" || child.core.permissionMode() != ModeInteractive || child.core.explicitSystem != nil {
		t.Fatal("fresh child defaults differ")
	}
	if child.core.team.Team != parent.core.team.Team || child.core.team.Name != "researcher" || child.core.label != "researcher" || child.core.taskDiagnostics.Load() == nil {
		t.Fatal("child identity/shared tasks missing")
	}
	if _, ok := child.core.gate.catalog.Lookup(protocol.ToolSpawnTeammate); ok {
		t.Fatal("recursive spawn enabled")
	}
	text, err := child.core.systemBuilder.BuildSystem(SystemContext{Workspace: parent.core.workspace})
	if err != nil || !strings.HasPrefix(text, "You are teammate 'researcher' (role: research) working in "+parent.core.workspace+".\n") || !strings.HasSuffix(text, "\n\nmanager system") || !strings.Contains(text, teammateGuidance) {
		t.Fatal("teammate system", text, err)
	}
	found := false
	for _, event := range child.core.Events() {
		peer := event.Scope.RunContext.Snapshot()
		if peer.ParentMessageID == nil {
			continue
		}
		found = true
		if peer.Authority != AuthorityPeerAgent || peer.ActorID == nil || *peer.ActorID != "researcher" || *peer.ParentMessageID != human.MessageID() || peer.DelegatedBy == nil || *peer.DelegatedBy != parent.core.label || len(peer.ApprovedCapabilities) != 0 || peer.StampedBy != "test" {
			t.Fatal("peer provenance widened", peer)
		}
	}
	if !found {
		t.Fatal("missing peer run events")
	}
	view, err := m.PeekTeam(context.Background(), "alice", parent.ID())
	if err != nil || len(view.Inbox) != 1 || !strings.Contains(spawn.Render(), "running concurrently") {
		t.Fatal("initial result not delivered", view, err)
	}
}

func TestSpawnTeammateNameReservationAndFailureRollback(t *testing.T) {
	cfg := managerTestConfig(t.TempDir(), cronDoneProvider{})
	m := makeManager(t, cfg)
	parent := createManaged(t, m, CreateSessionRequest{Owner: "alice"})
	for _, name := range []teams.MemberName{"lead", "", "bad/name", "含中文"} {
		_, err := m.SpawnTeammate(context.Background(), "alice", parent.ID(), SpawnTeammateRequest{Name: name})
		var refusal *TeamSpawnRefusal
		if !errors.As(err, &refusal) {
			t.Fatal("invalid name admitted", name, err)
		}
	}
	if _, err := m.SpawnTeammate(context.Background(), "bob", parent.ID(), SpawnTeammateRequest{Name: "worker"}); err == nil {
		t.Fatal("foreign owner spawned")
	}
	var wg sync.WaitGroup
	outcomes := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := m.SpawnTeammate(context.Background(), "alice", parent.ID(), SpawnTeammateRequest{Name: "worker"})
			outcomes <- err
		}()
	}
	wg.Wait()
	close(outcomes)
	success := 0
	for err := range outcomes {
		if err == nil {
			success++
		} else if !strings.Contains(err.Error(), "already in use") {
			t.Fatal(err)
		}
	}
	if success != 1 || len(m.teamNames(parent.core.team.Team)) != 1 {
		t.Fatal("duplicate concurrent members", success)
	}
	factory := m.config.Services.BashFactory
	m.config.Services.BashFactory = bashFactoryFunc(func(context.Context, SessionBinding) (BashExecutor, error) { return nil, errors.New("factory failed") })
	_, err := m.SpawnTeammate(context.Background(), "alice", parent.ID(), SpawnTeammateRequest{Name: "retry"})
	m.config.Services.BashFactory = factory
	if err == nil {
		t.Fatal("factory failure ignored")
	}
	child := spawnMember(t, m, parent, "retry")
	receiveSignal(t, child.teamRun.initialDone)
	if len(m.teamReservations) != 0 || m.creating != 0 {
		t.Fatal("creation reservation leaked")
	}
}

func TestSpawnTeammateDeleteJoinsBeforeSharedScratchReclamation(t *testing.T) {
	provider := newDrainingProvider()
	m := makeManager(t, managerTestConfig(t.TempDir(), provider))
	parent := createManaged(t, m, CreateSessionRequest{Owner: "alice"})
	child := spawnMember(t, m, parent, "worker")
	receiveSignal(t, provider.entered)
	path := parent.core.workspace
	if _, err := m.Delete("alice", parent.ID(), DeleteSessionOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Get("alice", child.ID()); err != nil {
		t.Fatal("parent deletion removed child", err)
	}
	if _, err := m.Delete("alice", child.ID(), DeleteSessionOptions{}); err != nil {
		t.Fatal(err)
	}
	receiveSignal(t, provider.cancelled)
	if _, err := os.Stat(path); err != nil {
		t.Fatal("live child root reclaimed", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if err := m.WaitCleanup(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("cleanup finished with live worker", err)
	}
	close(provider.release)
	if err := m.WaitCleanup(context.Background()); err != nil {
		t.Fatal(err)
	}
	receiveSignal(t, child.teamRun.done)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("shared scratch retained after last reference", err)
	}
}

func TestSpawnTeammateGateReadonlyAndNoParentTurnDeadlock(t *testing.T) {
	cfg := managerTestConfig(t.TempDir(), cronDoneProvider{})
	cfg.Services.TeamTools = true
	m := makeManager(t, cfg)
	parent := createManaged(t, m, CreateSessionRequest{Owner: "alice", PermissionMode: ModeReadonly})
	run, _ := DefaultRunContext()
	input := protocol.SpawnTeammateToolInput(protocol.SpawnTeammateInput{Name: "worker", Role: "research", Prompt: "hello"})
	if out := teamDispatch(t, parent.core, run, "readonly", input); !out.Denied || len(m.teamNames(parent.core.team.Team)) != 0 {
		t.Fatal("read-only spawn admitted", out)
	}
	if _, err := parent.ChangePermissionMode(ModeAuto); err != nil {
		t.Fatal(err)
	}
	// The model executes its gate while holding the core turn mutex.
	parent.core.mu.Lock()
	completed := make(chan ToolOutcome, 1)
	go func() {
		out, _ := parent.core.gate.Dispatch(context.Background(), ToolAuthority{SessionID: parent.ID(), OwnerID: "alice", Workspace: parent.core.workspace, Mode: ModeAuto, RunContext: run}, ToolCall{ID: "spawn", Input: input})
		completed <- out
	}()
	select {
	case out := <-completed:
		parent.core.mu.Unlock()
		if out.Failed || out.Denied || !strings.HasPrefix(out.Output, "Spawned teammate 'worker'") {
			t.Fatal(out)
		}
	case <-time.After(3 * time.Second):
		parent.core.mu.Unlock()
		t.Fatal("spawn waited on parent turn mutex")
	}
}

func TestSpawnTeammateStopWaitsForConstructionAndRejectsPublication(t *testing.T) {
	m := makeManager(t, managerTestConfig(t.TempDir(), cronDoneProvider{}))
	parent := createManaged(t, m, CreateSessionRequest{Owner: "alice"})
	entered, release := make(chan struct{}), make(chan struct{})
	factory := m.config.Services.BashFactory
	m.config.Services.BashFactory = bashFactoryFunc(func(ctx context.Context, binding SessionBinding) (BashExecutor, error) {
		close(entered)
		<-release
		return factory.BashFor(ctx, binding)
	})
	created := make(chan error, 1)
	go func() {
		_, err := m.SpawnTeammate(context.Background(), "alice", parent.ID(), SpawnTeammateRequest{Name: "worker"})
		created <- err
	}()
	receiveSignal(t, entered)
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	err := m.Stop(ctx)
	close(release)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("stop skipped construction", err)
	}
	if err := receiveRun(t, created); !errors.Is(err, ErrManagerStopped) {
		t.Fatal("stopped construction published", err)
	}
	if err := m.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(m.sessions) != 1 || len(m.teamReservations) != 0 || m.creating != 0 {
		t.Fatal("stopped construction leaked a member/reservation")
	}
}

func TestSpawnTeammateStopCancelsAndJoinsInitialRun(t *testing.T) {
	provider := newDrainingProvider()
	m := makeManager(t, managerTestConfig(t.TempDir(), provider))
	parent := createManaged(t, m, CreateSessionRequest{Owner: "alice"})
	child := spawnMember(t, m, parent, "worker")
	receiveSignal(t, provider.entered)
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	err := m.Stop(ctx)
	receiveSignal(t, provider.cancelled)
	close(provider.release)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("stop skipped live worker", err)
	}
	if err := m.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	receiveSignal(t, child.teamRun.done)
	rows, err := m.teams.Peek(context.Background(), parent.core.team.Key())
	if err != nil || len(rows) != 0 {
		t.Fatal("cancelled run delivered a result", err)
	}
}

func TestSpawnTeammateBoundWorkspaceSurvivesLastMemberDeletion(t *testing.T) {
	checkout := t.TempDir()
	marker := filepath.Join(checkout, "keep")
	if err := os.WriteFile(marker, []byte("source"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := managerTestConfig(t.TempDir(), cronDoneProvider{})
	cfg.BindableRoots = []string{checkout}
	m := makeManager(t, cfg)
	parent := createManaged(t, m, CreateSessionRequest{Owner: "alice", Workspace: &checkout})
	child := spawnMember(t, m, parent, "worker")
	receiveSignal(t, child.teamRun.initialDone)
	if !child.workspaceBound {
		t.Fatal("bound root lost")
	}
	for _, session := range []*ManagedSession{parent, child} {
		if _, err := m.Delete("alice", session.ID(), DeleteSessionOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.WaitCleanup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(marker); err != nil || string(data) != "source" {
		t.Fatal("bound root reclaimed", err)
	}
}
