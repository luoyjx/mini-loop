package sqlitestate

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/teams"
)

type teamRestartObservation struct {
	RestoredCount int                  `json:"restored_count"`
	Owner         agent.OwnerID        `json:"owner"`
	SameWorkspace bool                 `json:"same_workspace"`
	OwnTeam       bool                 `json:"own_team"`
	Name          teams.MemberName     `json:"name"`
	Mode          agent.PermissionMode `json:"mode"`
	Status        agent.SessionStatus  `json:"status"`
	RunCount      int                  `json:"run_count"`
	History       []protocol.Message   `json:"history"`
	OldInboxCount int                  `json:"old_inbox_count"`
	NewInboxCount int                  `json:"new_inbox_count"`
}

type teamNextTurnObservation struct {
	Result          string              `json:"result"`
	ModelCalls      int                 `json:"model_calls"`
	RequestMessages []protocol.Message  `json:"request_messages"`
	RecursiveSpawn  bool                `json:"recursive_spawn"`
	History         []protocol.Message  `json:"history"`
	Status          agent.SessionStatus `json:"status"`
	RunCount        int                 `json:"run_count"`
	OldInboxCount   int                 `json:"old_inbox_count"`
}

func TestTeamNativeSQLiteRestartAndResumedTurnMatchSource(t *testing.T) {
	var fixture struct {
		Restart struct {
			teamRestartObservation
			Next   teamNextTurnObservation `json:"next_turn"`
			Second teamRestartObservation  `json:"second_restart"`
		}
	}
	data, err := os.ReadFile(filepath.Join("..", "testdata", "python-team-lifecycle.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Restart.RestoredCount != 2 || fixture.Restart.Next.ModelCalls != 1 || fixture.Restart.Second.RunCount != 2 {
		t.Fatal("missing actual Source two-reopen contract")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	root := t.TempDir()
	workspaceRoot := filepath.Join(root, "ws")
	database := filepath.Join(root, "state.db")
	var mu sync.Mutex
	var requests []protocol.ModelRequest
	provider := restartProvider(func(_ context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
		mu.Lock()
		requests = append(requests, request.Clone())
		mu.Unlock()
		return protocol.ModelReply{ID: "reply", Type: protocol.ReplyMessage, Role: protocol.RoleAssistant, Model: request.Model, Content: []protocol.Block{protocol.NewTextBlock("done")}, StopReason: protocol.StopEndTurn}, nil
	})
	newManager := func(store *Store) *agent.SessionManager {
		manager, err := agent.NewSessionManager(agent.ManagerConfig{
			WorkspaceRoot: workspaceRoot,
			Services:      agent.ManagerServices{Provider: provider, StateStore: store},
			Defaults:      agent.SessionDefaults{PermissionMode: agent.ModeAuto, MaxRounds: 2},
			TeamIdlePoll:  time.Hour, TeamIdleTimeout: 2 * time.Hour,
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { stopRestartManager(t, manager) })
		return manager
	}
	store := openStore(t, database)
	manager := newManager(store)
	lead, err := manager.Create(ctx, agent.CreateSessionRequest{Owner: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	spawned, err := manager.SpawnTeammate(ctx, "alice", lead.ID(), agent.SpawnTeammateRequest{Name: "bob", Role: "research", Prompt: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	child, err := manager.Get("alice", spawned.Session)
	if err != nil {
		t.Fatal(err)
	}
	subscription := child.Subscribe(true)
	defer subscription.Close()
initial:
	for {
		select {
		case record := <-subscription.Events():
			if record.Event.Kind() == agent.EventDone {
				break initial
			}
			if record.Event.Kind() == agent.EventError {
				t.Fatal("initial teammate turn failed", record)
			}
		case <-ctx.Done():
			t.Fatal("initial teammate turn", ctx.Err())
		}
	}
	oldWorkspace := child.Info().Workspace
	teamRoot := filepath.Join(workspaceRoot, ".teams")
	oldKey := teams.Key(teams.TeamID(lead.ID()), "bob")
	bus := teams.New(teams.Config{Root: &teamRoot})
	if _, err := bus.Send(ctx, teams.SendRequest{From: teams.Key(teams.TeamID(lead.ID()), teams.Lead), To: oldKey, Content: "pending old-team mail"}); err != nil {
		t.Fatal(err)
	}
	stopRestartManager(t, manager)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	observe := func(manager *agent.SessionManager, restored int) (*agent.ManagedSession, teamRestartObservation) {
		child, err := manager.Get("alice", spawned.Session)
		if err != nil {
			t.Fatal(err)
		}
		info := child.Info()
		view, err := manager.PeekTeam(ctx, "alice", child.ID())
		if err != nil {
			t.Fatal(err)
		}
		if view.Identity == nil {
			t.Fatal("restored team identity missing")
		}
		old, err := bus.Peek(ctx, oldKey)
		if err != nil {
			t.Fatal(err)
		}
		return child, teamRestartObservation{RestoredCount: restored, Owner: child.Owner(), SameWorkspace: info.Workspace == oldWorkspace, OwnTeam: view.Identity.Team == teams.TeamID(child.ID()), Name: view.Identity.Name, Mode: info.PermissionMode, Status: info.Status, RunCount: info.RunCount, History: child.Messages(), OldInboxCount: len(old), NewInboxCount: len(view.Inbox)}
	}
	reopened := openStore(t, database)
	fresh := newManager(reopened)
	restored, err := fresh.RestoreSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	child, got := observe(fresh, len(restored))
	if !reflect.DeepEqual(got, fixture.Restart.teamRestartObservation) {
		t.Fatalf("first native reopen: got %+v want %+v", got, fixture.Restart.teamRestartObservation)
	}
	mu.Lock()
	before := len(requests)
	mu.Unlock()
	if before != 1 {
		t.Fatalf("restore invoked model before an explicit turn: %d", before)
	}
	final, err := child.Run(ctx, "after restart")
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	calls := len(requests) - before
	request := requests[len(requests)-1].Clone()
	mu.Unlock()
	recursive := false
	for _, tool := range request.Tools {
		if tool.Name == protocol.ToolSpawnTeammate {
			recursive = true
		}
	}
	old, err := bus.Peek(ctx, oldKey)
	if err != nil {
		t.Fatal(err)
	}
	info := child.Info()
	next := teamNextTurnObservation{Result: final, ModelCalls: calls, RequestMessages: request.Messages, RecursiveSpawn: recursive, History: child.Messages(), Status: info.Status, RunCount: info.RunCount, OldInboxCount: len(old)}
	if !reflect.DeepEqual(next, fixture.Restart.Next) {
		t.Fatalf("resumed native turn: got %+v want %+v", next, fixture.Restart.Next)
	}
	stopRestartManager(t, fresh)
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
	lastStore := openStore(t, database)
	last := newManager(lastStore)
	restored, err = last.RestoreSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, got = observe(last, len(restored))
	if !reflect.DeepEqual(got, fixture.Restart.Second) {
		t.Fatalf("second native reopen: got %+v want %+v", got, fixture.Restart.Second)
	}
	mu.Lock()
	after := len(requests)
	mu.Unlock()
	if after != before+fixture.Restart.Next.ModelCalls {
		t.Fatal("second restoration unexpectedly invoked a model")
	}
}
