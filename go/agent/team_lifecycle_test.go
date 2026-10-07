package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/tasks"
	"github.com/luoyjx/mini-loop/go/teams"
	"github.com/luoyjx/mini-loop/go/worktrees"
)

type lifecycleOutgoing struct {
	Content  string     `json:"content"`
	Type     string     `json:"type"`
	Metadata teams.Data `json:"metadata"`
}

type teamRestartCase struct {
	RestoredCount    int `json:"restored_count"`
	Owner            OwnerID
	SameWorkspace    bool `json:"same_workspace"`
	OwnTeam          bool `json:"own_team"`
	Name             teams.MemberName
	Label            string
	Mode             PermissionMode
	RolePresent      bool `json:"role_present"`
	TasksPresent     bool `json:"tasks_present"`
	RecursiveSpawn   bool `json:"recursive_spawn"`
	RunnerPresent    bool `json:"runner_present"`
	OldRosterPresent bool `json:"old_roster_present"`
	OldInboxCount    int  `json:"old_inbox_count"`
	NewInboxCount    int  `json:"new_inbox_count"`
	Status           SessionStatus
	RunCount         int `json:"run_count"`
	History          []protocol.Message
}

func TestTeamRestoreMatchesActualSourceSQLiteReconstruction(t *testing.T) {
	var fixture struct{ Restart teamRestartCase }
	data, err := os.ReadFile(filepath.Join("..", "testdata", "python-team-lifecycle.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	want := fixture.Restart
	if want.RestoredCount == 0 {
		t.Fatal("missing actual-source restart evidence")
	}
	// Native backing proves adapter reconstruction only, not native SQL or physical restart.
	store := newRuntimeStateStore()
	provider := stateProviderFunc(func(context.Context, protocol.ModelRequest) (protocol.ModelReply, error) {
		return fakeReply([]protocol.Block{protocol.NewTextBlock("done")}, protocol.StopEndTurn), nil
	})
	cfg := managerTestConfig(t.TempDir(), provider)
	cfg.Services.StateStore = store
	cfg.TeamIdlePoll, cfg.TeamIdleTimeout = time.Hour, 2*time.Hour
	manager := makeManager(t, cfg)
	lead := createManaged(t, manager, CreateSessionRequest{Owner: "alice"})
	spawn, err := manager.SpawnTeammate(context.Background(), "alice", lead.ID(), SpawnTeammateRequest{Name: "bob", Role: "research", Prompt: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	child, err := manager.Get("alice", spawn.Session)
	if err != nil {
		t.Fatal(err)
	}
	receiveSignal(t, child.teamRun.initialDone)
	oldIdentity, oldWorkspace := *child.core.team, child.core.workspace
	if _, err := manager.teams.Send(context.Background(), teams.SendRequest{From: lead.core.team.Key(), To: oldIdentity.Key(), Content: "pending old-team mail"}); err != nil {
		t.Fatal(err)
	}
	if err := manager.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	fresh := makeManager(t, cfg)
	restored, err := fresh.RestoreSessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	child, err = fresh.Get("alice", spawn.Session)
	if err != nil {
		t.Fatal(err)
	}
	_, recursive := child.core.gate.catalog.Lookup(protocol.ToolSpawnTeammate)
	roster := (managerTeamDirectory{fresh}).Member(oldIdentity) != teams.MemberMissing
	_, hasRole := child.core.systemBuilder.(teammateSystemBuilder)
	equalRestoreMessages(t, child.Messages(), want.History)
	oldMail, err := fresh.teams.Peek(context.Background(), oldIdentity.Key())
	if err != nil {
		t.Fatal(err)
	}
	newMail, err := fresh.PeekTeam(context.Background(), "alice", child.ID())
	if err != nil {
		t.Fatal(err)
	}
	info := child.Info()
	got := teamRestartCase{
		RestoredCount: len(restored), Owner: child.Owner(),
		SameWorkspace: child.core.workspace == oldWorkspace,
		OwnTeam:       child.core.team.Team == teams.TeamID(child.ID()),
		Name:          child.core.team.Name, Label: child.core.label, Mode: child.core.permissionMode(),
		RolePresent: hasRole, TasksPresent: child.core.taskDiagnostics.Load() != nil,
		RecursiveSpawn: recursive, RunnerPresent: child.teamRun != nil, OldRosterPresent: roster,
		OldInboxCount: len(oldMail), NewInboxCount: len(newMail.Inbox),
		Status: info.Status, RunCount: info.RunCount,
	}
	// History is compared separately using its protocol representation above.
	want.History = nil
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("restored teammate projection differs: got %+v, want %+v", got, want)
	}
}

type teamIdleBlockingProvider struct {
	initial atomic.Bool
	blocked *drainingManagerProvider
}

func (provider *teamIdleBlockingProvider) Complete(ctx context.Context, req protocol.ModelRequest) (protocol.ModelReply, error) {
	if provider.initial.CompareAndSwap(false, true) {
		return cronDoneProvider{}.Complete(ctx, req)
	}
	return provider.blocked.Complete(ctx, req)
}

func TestOwnedTeammateIdleRunIsCancelledAndJoinedOnDelete(t *testing.T) {
	blocked := newDrainingProvider()
	provider := &teamIdleBlockingProvider{blocked: blocked}
	cfg := managerTestConfig(t.TempDir(), provider)
	cfg.TeamIdlePoll, cfg.TeamIdleTimeout = time.Millisecond, time.Minute
	m := makeManager(t, cfg)
	lead := createManaged(t, m, CreateSessionRequest{Owner: "alice"})
	child := spawnMember(t, m, lead, "worker")
	receiveSignal(t, child.teamRun.initialDone)
	if _, err := m.teams.Send(context.Background(), teams.SendRequest{From: lead.core.team.Key(), To: child.core.team.Key(), Content: "wake"}); err != nil {
		t.Fatal(err)
	}
	receiveSignal(t, blocked.entered)
	if _, err := m.Delete("alice", child.ID(), DeleteSessionOptions{}); err != nil {
		t.Fatal(err)
	}
	receiveSignal(t, blocked.cancelled)
	select {
	case <-child.teamRun.done:
		close(blocked.release)
		t.Fatal("lifetime finished with a live idle-run provider")
	default:
	}
	close(blocked.release)
	if err := m.WaitCleanup(context.Background()); err != nil {
		t.Fatal(err)
	}
	receiveSignal(t, child.teamRun.done)
	rows, err := m.teams.Read(context.Background(), lead.core.team.Key())
	if err != nil || len(rows) != 1 {
		t.Fatal("cancelled idle run reported result/timeout", rows, err)
	}
}

func TestOwnedTeammateShutdownStopsIdleLifetimeWithoutUnregistering(t *testing.T) {
	cfg := managerTestConfig(t.TempDir(), cronDoneProvider{})
	cfg.TeamIdlePoll, cfg.TeamIdleTimeout = time.Millisecond, time.Minute
	cfg.Services.TeamTools = true
	m := makeManager(t, cfg)
	lead := createManaged(t, m, CreateSessionRequest{Owner: "alice"})
	child := spawnMember(t, m, lead, "worker")
	receiveSignal(t, child.teamRun.initialDone)
	if _, err := m.teamProtocols.RequestShutdown(context.Background(), lead.core.team.Team, "worker", "stop"); err != nil {
		t.Fatal(err)
	}
	receiveSignal(t, child.teamRun.done)
	if _, err := m.Get("alice", child.ID()); err != nil || child.core.teamShutdown.Load() {
		t.Fatal("shutdown registration/assignment", err)
	}
	rows, err := m.teams.Read(context.Background(), lead.core.team.Key())
	if err != nil || len(rows) != 2 {
		t.Fatal(rows, err)
	}
	typeData, _ := rows[1].Data().Lookup("type")
	if kind, _ := typeData.Text(); kind != "shutdown_response" {
		t.Fatal("missing automatic acknowledgment", kind)
	}
}

func TestTeamInjectionRejectsRetiredBindingBeforeMailboxIO(t *testing.T) {
	cfg := managerTestConfig(t.TempDir(), cronDoneProvider{})
	cfg.Services.TeamTools = true
	m := makeManager(t, cfg)
	lead := createManaged(t, m, CreateSessionRequest{Owner: "alice"})
	if _, err := m.teams.Send(context.Background(), teams.SendRequest{From: lead.core.team.Key(), To: lead.core.team.Key(), Content: "private"}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Delete("alice", lead.ID(), DeleteSessionOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := lead.core.injectTeam(context.Background()); err == nil {
		t.Fatal("retired injector consumed private data")
	}
	rows, err := m.teams.Peek(context.Background(), lead.core.team.Key())
	if err != nil || len(rows) != 1 {
		t.Fatal("retired binding reached IO", rows, err)
	}
}

func TestTeamIdleDefaultsAndInvalidDurations(t *testing.T) {
	m := makeManager(t, managerTestConfig(t.TempDir(), cronDoneProvider{}))
	if m.config.TeamIdlePoll != time.Second || m.config.TeamIdleTimeout != time.Minute {
		t.Fatal("source defaults differ")
	}
	for _, poll := range []bool{false, true} {
		root := filepath.Join(t.TempDir(), "not-created")
		cfg := managerTestConfig(root, cronDoneProvider{})
		if poll {
			cfg.TeamIdlePoll = -time.Nanosecond
		} else {
			cfg.TeamIdleTimeout = -time.Nanosecond
		}
		if _, err := NewSessionManager(cfg); err == nil {
			t.Fatal("invalid duration admitted")
		}
		if _, err := os.Stat(root); !os.IsNotExist(err) {
			t.Fatal("invalid configuration created workspace", err)
		}
	}
}

type lifecycleCase struct {
	PollMillis                       int `json:"poll_ms"`
	TimeoutMillis                    int `json:"timeout_ms"`
	Name                             string
	Rows                             []string `json:"rows_json"`
	Unconfigured, Teamless, Shutdown bool
	Output                           []string
	Error, Raw                       string
	Counts                           []int
	Remaining                        int
	Tasks                            []tasks.Task
	WorktreeExists                   bool `json:"worktree_exists"`
	ShutdownBefore                   bool `json:"shutdown_before"`
	Prompts, Workspaces              []string
	Contexts                         []RunContextSnapshot
	FinalTasks                       []tasks.Task `json:"final_tasks"`
	Outgoing                         []lifecycleOutgoing
	Initial                          struct {
		SharedWorkspace bool `json:"shared_workspace"`
		SharedSkills    bool `json:"shared_skills"`
		SharedMemory    bool `json:"shared_memory"`
		Owner           OwnerID
		Mode            PermissionMode
		Role            string
		RecursiveSpawn  bool `json:"recursive_spawn"`
		Result          string
	}
}

type teamLifecycleProvider struct {
	mu                  sync.Mutex
	core                *Session
	prompts, workspaces []string
	contexts            []RunContextSnapshot
}

func (provider *teamLifecycleProvider) Complete(ctx context.Context, req protocol.ModelRequest) (protocol.ModelReply, error) {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	if core := provider.core; core != nil {
		for i := len(core.messages) - 1; i >= 0; i-- {
			if text, ok := core.messages[i].Content.Plain(); ok && core.messages[i].Role == protocol.RoleUser {
				provider.prompts = append(provider.prompts, text)
				break
			}
		}
		provider.workspaces = append(provider.workspaces, core.executionRoot())
		provider.contexts = append(provider.contexts, core.currentRun.Snapshot())
	}
	return cronDoneProvider{}.Complete(ctx, req)
}

func lifecycleFleet(t *testing.T, recipe lifecycleCase, provider Provider, inject bool) (*SessionManager, *ManagedSession, *ManagedSession) {
	t.Helper()
	cfg := managerTestConfig(t.TempDir(), provider)
	cfg.Services.TeamTools = inject
	cfg.TeamIdlePoll, cfg.TeamIdleTimeout = time.Hour, 2*time.Hour
	m := makeManager(t, cfg)
	lead := createManaged(t, m, CreateSessionRequest{Owner: "alice"})
	spawn, err := m.SpawnTeammate(context.Background(), "alice", lead.ID(), SpawnTeammateRequest{Name: "bob", Role: "research", Prompt: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	child, err := m.Get("alice", spawn.Session)
	if err != nil {
		t.Fatal(err)
	}
	receiveSignal(t, child.teamRun.initialDone)
	child.teamRun.cancel()
	receiveSignal(t, child.teamRun.done)
	initial := recipe.Initial
	_, recursive := child.core.gate.catalog.Lookup(protocol.ToolSpawnTeammate)
	if (child.core.workspace == lead.core.workspace) != initial.SharedWorkspace || (child.core.skills == lead.core.skills) != initial.SharedSkills || (child.core.memory == lead.core.memory) != initial.SharedMemory || child.Owner() != initial.Owner || child.core.permissionMode() != initial.Mode || recursive != initial.RecursiveSpawn || initial.Role != "research" || strings.ReplaceAll(spawn.Render(), string(child.ID()), "<session>") != initial.Result {
		t.Fatal("actual spawn contract differs", initial)
	}
	if _, err := m.teams.Read(context.Background(), lead.core.team.Key()); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(m.WorkspaceRoot(), ".teams", string(lead.ID()), "inboxes", "bob.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Join(recipe.Rows, "\n")+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return m, lead, child
}

func lifecycleOutgoingRows(t *testing.T, m *SessionManager, lead *ManagedSession) []lifecycleOutgoing {
	t.Helper()
	rows, err := m.teams.Read(context.Background(), lead.core.team.Key())
	if err != nil {
		t.Fatal(err)
	}
	out := []lifecycleOutgoing{}
	for _, row := range rows {
		content, _ := row.Content()
		kind, _ := row.Data().Lookup("type")
		typeName, _ := kind.Text()
		metadata, _ := row.Data().Lookup("metadata")
		out = append(out, lifecycleOutgoing{content, typeName, metadata.Sorted()})
	}
	return out
}

func checkLifecycleOutgoing(t *testing.T, got, want []lifecycleOutgoing) {
	t.Helper()
	for i := range want {
		want[i].Metadata = want[i].Metadata.Sorted()
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatal("outgoing mailbox differs", got, want)
	}
}

func TestTeamLifecycleMatchesActualPythonSpawnInjectorsAndIdleTurns(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-team-lifecycle.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct{ Injectors, Idle []lifecycleCase }
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Injectors) != 8 || len(fixture.Idle) != 11 {
		t.Fatal("incomplete actual source corpus")
	}
	for _, recipe := range fixture.Injectors {
		t.Run("inject/"+recipe.Name, func(t *testing.T) {
			m, lead, child := lifecycleFleet(t, recipe, cronDoneProvider{}, true)
			if recipe.Unconfigured {
				child.core.teamManager = nil
			}
			if recipe.Teamless {
				child.core.team = nil
			}
			startEvents, startMessages := len(child.core.Events()), len(child.core.Messages())
			child.core.mu.Lock()
			err := child.core.injectTeam(context.Background())
			child.core.mu.Unlock()
			if (err != nil) != (recipe.Error != "") {
				t.Fatal("source error disposition", err, recipe.Error)
			}
			got := []string{}
			for _, message := range child.core.Messages()[startMessages:] {
				text, _ := message.Content.Plain()
				got = append(got, strings.ReplaceAll(text, string(lead.ID()), "team"))
			}
			counts := []int{}
			for _, record := range child.core.Events()[startEvents:] {
				if event, ok := record.Event.TeamInbox(); ok {
					counts = append(counts, event.Count)
					wire, err := json.Marshal(record)
					if err != nil {
						t.Fatal(err)
					}
					restored, err := DecodeStoredEvent(wire)
					if err != nil {
						t.Fatal(err)
					}
					if value, ok := restored.Event.TeamInbox(); !ok || value != event {
						t.Fatal("team event roundtrip")
					}
				}
			}
			rows, err := m.teams.Peek(context.Background(), teams.Key(teams.TeamID(lead.ID()), "bob"))
			if err != nil || len(rows) != recipe.Remaining || !reflect.DeepEqual(got, recipe.Output) || !reflect.DeepEqual(counts, recipe.Counts) || child.core.teamShutdown.Load() != recipe.Shutdown {
				t.Fatal("injector differs", got, counts, len(rows), err)
			}
			rawRows := []jsonvalue.Value{}
			for _, encoded := range recipe.Rows {
				value, err := teams.DecodeData(encoded)
				if err != nil {
					t.Fatal(err)
				}
				rawRows = append(rawRows, value)
			}
			encodedRaw, err := jsonvalue.AppendLegacyDefault(jsonvalue.ArrayValue(rawRows))
			text := string(encodedRaw)
			if err != nil || text != recipe.Raw {
				t.Fatal("raw Python JSON prompt differs", text, recipe.Raw, err)
			}
			checkLifecycleOutgoing(t, lifecycleOutgoingRows(t, m, lead), recipe.Outgoing)
		})
	}
	for _, recipe := range fixture.Idle {
		t.Run("idle/"+recipe.Name, func(t *testing.T) {
			provider := &teamLifecycleProvider{prompts: []string{}, workspaces: []string{}, contexts: []RunContextSnapshot{}}
			m, lead, child := lifecycleFleet(t, recipe, provider, false)
			board := child.core.taskDiagnostics.Load()
			for _, task := range recipe.Tasks {
				if task.Status == "" {
					task.Status = tasks.Pending
				}
				if err := board.Save(task); err != nil {
					t.Fatal(err)
				}
			}
			wt, err := worktrees.New(worktrees.Config{Repository: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			m.config.Services.Worktrees = wt
			target, err := wt.PathFor("checkout")
			if err != nil {
				t.Fatal(err)
			}
			if recipe.WorktreeExists {
				if err := os.MkdirAll(target, 0700); err != nil {
					t.Fatal(err)
				}
			}
			child.core.teamShutdown.Store(recipe.ShutdownBefore)
			m.config.TeamIdlePoll, m.config.TeamIdleTimeout = time.Duration(recipe.PollMillis)*time.Millisecond, time.Duration(recipe.TimeoutMillis)*time.Millisecond
			provider.core = child.core
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := m.teammateIdleLoop(ctx, child); err != nil {
				t.Fatal(err)
			}
			for i, text := range provider.prompts {
				provider.prompts[i] = strings.ReplaceAll(text, string(lead.ID()), "team")
			}
			for i, path := range provider.workspaces {
				provider.workspaces[i] = strings.NewReplacer(child.core.workspace, "<workspace>", target, "<worktree>").Replace(path)
			}
			for i := range provider.contexts {
				provider.contexts[i].MessageID = "<message>"
			}
			final, err := board.List()
			if err != nil || !reflect.DeepEqual(provider.prompts, recipe.Prompts) || !reflect.DeepEqual(provider.workspaces, recipe.Workspaces) || !reflect.DeepEqual(provider.contexts, recipe.Contexts) || !reflect.DeepEqual(final, recipe.FinalTasks) || child.core.teamShutdown.Load() != recipe.Shutdown {
				t.Fatal("idle loop differs", provider.prompts, recipe.Prompts, provider.contexts, recipe.Contexts, provider.workspaces, recipe.Workspaces, final, recipe.FinalTasks, err)
			}
			if child.core.workspace != lead.core.workspace || child.core.taskDiagnostics.Load().Root() != board.Root() {
				t.Fatal("claim changed lifecycle root/task board")
			}
			checkLifecycleOutgoing(t, lifecycleOutgoingRows(t, m, lead), recipe.Outgoing)
		})
	}
}
