package sqlitestate

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/protocol"
)

type crashCheckpoint struct {
	Session agent.SessionID  `json:"session"`
	Owner   agent.LeaseOwner `json:"lease_owner"`
	PID     int              `json:"pid"`
}

// This helper runs only in a dedicated child test process. The parent kills it
// while Complete is blocked, bypassing both Manager.Stop and database Close.
func TestNativeSQLiteCrashProcessHelper(t *testing.T) {
	root := os.Getenv("MINILOOP_SQLITE_CRASH_TEST_ROOT")
	if root == "" {
		t.Skip("subprocess helper")
	}
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(root, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	var session *agent.ManagedSession
	provider := restartProvider(func(ctx context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
		if err := request.Validate(); err != nil {
			return protocol.ModelReply{}, err
		}
		messages, err := store.LoadMessages(ctx, session.ID(), nil)
		if err != nil || len(messages) != 1 {
			return protocol.ModelReply{}, errors.New("request preceded native transcript flush")
		}
		owner, held, err := store.LeaseHolder(ctx, session.ID())
		if err != nil || !held {
			return protocol.ModelReply{}, errors.New("writer has no lease")
		}
		data, err := json.Marshal(crashCheckpoint{session.ID(), owner, os.Getpid()})
		if err != nil {
			return protocol.ModelReply{}, err
		}
		pending := filepath.Join(root, "checkpoint.pending")
		if err := os.WriteFile(pending, data, 0600); err != nil {
			return protocol.ModelReply{}, err
		}
		if err := os.Rename(pending, filepath.Join(root, "checkpoint.json")); err != nil {
			return protocol.ModelReply{}, err
		}
		<-ctx.Done()
		return protocol.ModelReply{}, ctx.Err()
	})
	manager, err := agent.NewSessionManager(agent.ManagerConfig{
		WorkspaceRoot: filepath.Join(root, "workspaces"), StateLeaseTTL: 8 * time.Second,
		Services: agent.ManagerServices{StateStore: store, Provider: provider},
		Defaults: agent.SessionDefaults{MaxRounds: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	session, err = manager.Create(ctx, agent.CreateSessionRequest{Owner: "tenant"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Run(ctx, "interrupted process request"); err != nil {
		t.Fatal(err)
	}
	t.Fatal("crash helper unexpectedly finished its model call")
}

func TestNativeSQLiteRecoversKilledWriterWithoutStealingLease(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestNativeSQLiteCrashProcessHelper$", "-test.timeout=20s")
	child.Env = append(os.Environ(), "MINILOOP_SQLITE_CRASH_TEST_ROOT="+root)
	child.Stdout, child.Stderr = os.Stdout, os.Stderr
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	waited := false
	t.Cleanup(func() {
		if !waited {
			_ = child.Process.Kill()
			_ = child.Wait()
		}
	})
	var checkpoint crashCheckpoint
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		data, err := os.ReadFile(filepath.Join(root, "checkpoint.json"))
		if err == nil {
			if err := json.Unmarshal(data, &checkpoint); err != nil {
				t.Fatal(err)
			}
			break
		}
		if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		select {
		case <-ctx.Done():
			t.Fatal("writer did not reach native model checkpoint", ctx.Err())
		case <-ticker.C:
		}
	}
	if checkpoint.PID != child.Process.Pid || checkpoint.PID == os.Getpid() || checkpoint.Session == "" || checkpoint.Owner == "" {
		t.Fatal("invalid child checkpoint", checkpoint)
	}
	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	err := child.Wait()
	waited = true
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.Success() {
		t.Fatal("writer did not terminate abruptly", err)
	}
	store := openStore(t, filepath.Join(root, "state.db"))
	owner, held, err := store.LeaseHolder(ctx, checkpoint.Session)
	if err != nil || !held || owner != checkpoint.Owner {
		t.Fatal("process death erased the persisted lease", owner, held, err)
	}
	calls := 0
	provider := restartProvider(func(_ context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
		calls++
		if err := request.Validate(); err != nil || len(request.Messages) < 3 {
			t.Fatal("invalid request after native crash repair", err, request.Messages)
		}
		repair, err := json.Marshal(request.Messages[1])
		if err != nil || request.Messages[1].Role != protocol.RoleAssistant || !strings.Contains(string(repair), "[Turn interrupted: process stopped mid-generation]") {
			t.Fatal("crash repair missing from resumed provider request", string(repair), err)
		}
		return protocol.ModelReply{ID: "resumed", Type: protocol.ReplyMessage, Role: protocol.RoleAssistant, Model: request.Model, Content: []protocol.Block{protocol.NewTextBlock("resumed")}, StopReason: protocol.StopEndTurn}, nil
	})
	manager := restartManager(t, filepath.Join(root, "workspaces"), store, provider)
	session := restoreRestartSession(t, manager)
	state := session.PersistenceStatus()
	if !state.RestorePending || state.LeaseConfirmed || session.Owner() != "tenant" || len(session.Messages()) != 1 {
		t.Fatal("recovery stole lease or repaired before claim", state, session.Info())
	}
	if _, err := session.Run(ctx, "too early"); !errors.Is(err, agent.ErrSessionLeaseLost) || calls != 0 {
		t.Fatal("dead-process lease admitted model work before expiry", err, calls)
	}
	for held {
		select {
		case <-ctx.Done():
			t.Fatal("persisted lease never expired", ctx.Err())
		case <-ticker.C:
		}
		_, held, err = store.LeaseHolder(ctx, checkpoint.Session)
		if err != nil {
			t.Fatal(err)
		}
	}
	if result, err := session.Run(ctx, "continue after crash"); err != nil || result != "resumed" || calls != 1 {
		t.Fatal("resumed native request", result, err, calls)
	}
	state = session.PersistenceStatus()
	if state.Error != nil || state.RestorePending || !state.LeaseConfirmed {
		t.Fatal("native recovery degraded", state)
	}
	owner, held, err = store.LeaseHolder(ctx, checkpoint.Session)
	if err != nil || !held || owner == checkpoint.Owner {
		t.Fatal("recovery did not install distinct process ownership", owner, held, err)
	}
	stopRestartManager(t, manager)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store = openStore(t, filepath.Join(root, "state.db"))
	manager = restartManager(t, filepath.Join(root, "workspaces"), store, provider)
	session = restoreRestartSession(t, manager)
	if len(session.Messages()) < 4 || session.PersistenceStatus().RestorePending || session.Info().RunCount != 2 {
		t.Fatal("crash repair/resumed turn lost on next reopen", session.Info(), session.Messages())
	}
	stopRestartManager(t, manager)
}
