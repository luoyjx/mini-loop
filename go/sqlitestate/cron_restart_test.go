package sqlitestate

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/protocol"
)

type cronPromptHook func(context.Context, agent.TurnContext, string) (*string, error)

func (f cronPromptHook) RewriteUserPrompt(ctx context.Context, view agent.TurnContext, prompt string) (*string, error) {
	return f(ctx, view, prompt)
}

func TestManagedCronNativeSQLiteReopenRequiresArmAndRestoresLazily(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	path := filepath.Join(root, "state.db")
	store := openStore(t, path)
	provider := restartProvider(func(_ context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
		return protocol.ModelReply{ID: "reply", Type: protocol.ReplyMessage, Role: protocol.RoleAssistant, Model: request.Model, Content: []protocol.Block{protocol.NewTextBlock("finished")}, StopReason: protocol.StopEndTurn}, nil
	})
	manager := restartManager(t, root, store, provider)
	session, err := manager.Create(ctx, agent.CreateSessionRequest{Owner: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Run(ctx, "before scheduled restart"); err != nil {
		t.Fatal(err)
	}
	history := session.Messages()
	info := session.Info()
	recurring := false
	job, err := manager.ScheduleCron("alice", info.ID, agent.ScheduleCronRequest{Cron: "30 12 5 10 *", Prompt: "continue persisted history", Recurring: &recurring})
	if err != nil || !job.Job.Durable {
		t.Fatal(job, err)
	}
	stopRestartManager(t, manager)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store = openStore(t, path)
	var requests []protocol.ModelRequest
	var authority []agent.RunContextSnapshot
	config := agent.ManagerConfig{
		WorkspaceRoot: root,
		Defaults:      agent.SessionDefaults{PermissionMode: agent.ModeAuto, MaxRounds: 2},
		Services: agent.ManagerServices{
			StateStore: store,
			Provider: restartProvider(func(ctx context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
				if err := request.Validate(); err != nil {
					return protocol.ModelReply{}, err
				}
				requests = append(requests, request.Clone())
				count, err := store.MessageCount(ctx, info.ID, nil)
				if err != nil || count < len(history)+1 {
					return protocol.ModelReply{}, errors.New("scheduled provider preceded native transcript flush")
				}
				return provider.Complete(ctx, request)
			}),
			UserPromptHooks: []agent.UserPromptHook{cronPromptHook(func(_ context.Context, view agent.TurnContext, prompt string) (*string, error) {
				authority = append(authority, view.Authority.RunContext.Snapshot())
				if !strings.Contains(prompt, "continue persisted history") {
					return nil, errors.New("lost scheduled prompt")
				}
				return nil, nil
			})},
		},
	}
	manager, err = agent.NewSessionManager(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stopRestartManager(t, manager) })
	if err := manager.Start(); err != nil {
		t.Fatal(err)
	}
	scheduler := manager.CronScheduler()
	jobs := scheduler.Jobs()
	if len(jobs) != 1 || jobs[0].ID != job.Job.ID || scheduler.Armed(job.Job.ID) {
		t.Fatal("durable job was lost or armed at boot", jobs)
	}
	if _, err := manager.Get("alice", info.ID); !errors.Is(err, agent.ErrSessionNotFound) {
		t.Fatal("scheduled handle restored eagerly", err)
	}
	minute := time.Date(2026, 10, 5, 12, 30, 0, 0, time.Local)
	scheduler.Tick(minute)
	wait := func() {
		waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if err := scheduler.Wait(waitCtx); err != nil {
			t.Fatal(err)
		}
	}
	wait()
	if len(requests) != 0 {
		t.Fatal("unarmed job invoked provider")
	}
	if result := scheduler.Arm(job.Job.ID, nil); !strings.Contains(result, "Armed") {
		t.Fatal(result)
	}
	scheduler.Tick(minute)
	wait()
	session, err = manager.Get("alice", info.ID)
	if err != nil || len(requests) != 1 || session.Info().RunCount != 2 {
		t.Fatal("scheduled restoration/turn", len(requests), err, scheduler.Problems())
	}
	if _, err := manager.Get("bob", info.ID); !errors.Is(err, agent.ErrSessionNotFound) {
		t.Fatal("scheduled restoration lost owner", err)
	}
	if len(requests[0].Messages) <= len(history) || !sameRestartMessages(requests[0].Messages[:len(history)], history) {
		t.Fatal("scheduled request lost persisted history", requests[0])
	}
	if len(authority) != 1 || authority[0].Authority != agent.AuthorityUntrusted || authority[0].ActorID != nil || len(authority[0].ApprovedCapabilities) != 0 {
		t.Fatal("scheduled turn restored authority", authority)
	}
	if len(scheduler.Jobs()) != 0 {
		t.Fatal("one-shot retained after dispatch")
	}
	state := session.PersistenceStatus()
	if state.Error != nil || !state.Restored || !state.LeaseConfirmed || state.RestorePending {
		t.Fatal("native scheduled persistence degraded", state)
	}
	stopRestartManager(t, manager)
	if holder, held, err := store.LeaseHolder(ctx, info.ID); err != nil || held || holder != "" {
		t.Fatal("scheduled stop retained lease", holder, held, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store = openStore(t, path)
	manager = restartManager(t, root, store, provider)
	if len(manager.CronScheduler().Jobs()) != 0 {
		t.Fatal("one-shot removal did not survive another restart")
	}
	rows, err := store.LoadSessions(ctx)
	if err != nil || len(rows) != 1 || rows[0].SessionID != info.ID || rows[0].RunCount != 2 {
		t.Fatal("scheduled turn metadata was not durable", rows, err)
	}
	stopRestartManager(t, manager)
}
