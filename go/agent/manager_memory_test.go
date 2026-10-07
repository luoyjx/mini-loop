package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/memory"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/skills"
	"github.com/luoyjx/mini-loop/go/userresources"
)

func TestManagerDefaultMemoryBindingAndStartupFailure(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	manager := makeManager(t, managerTestConfig(root, &FakeProvider{}))
	if info, err := os.Stat(filepath.Join(root, ".memory")); err != nil || !info.IsDir() {
		t.Fatal("default memory root missing", err)
	}
	alice := createManaged(t, manager, CreateSessionRequest{Owner: "alice"})
	bob := createManaged(t, manager, CreateSessionRequest{Owner: "bob"})
	if _, err := alice.core.memory.Write(ctx, memory.Input{Name: "private", Body: "alice fact"}); err != nil {
		t.Fatal(err)
	}
	if records, err := bob.core.memory.List(ctx); err != nil || len(records) != 0 {
		t.Fatal("default shared memory leaked across owners", records, err)
	}
	for _, tool := range []protocol.ToolName{protocol.ToolRemember, protocol.ToolRecall} {
		if _, ok := alice.core.gate.catalog.Lookup(tool); ok {
			t.Fatal("storage activated a memory tool", tool)
		}
	}
	if alice.core.automaticMemoryEnabled() {
		t.Fatal("storage activated automatic memory")
	}
	blocked := t.TempDir()
	if err := os.WriteFile(filepath.Join(blocked, ".memory"), []byte("blocked"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, local := range []bool{false, true} {
		cfg := managerTestConfig(blocked, &FakeProvider{})
		if local {
			cfg.Services.UserResources = ownerResourceResolver(t, t.TempDir(), skills.EmptyCatalog())
		}
		if _, err := NewSessionManager(cfg); err == nil {
			t.Fatal("manager accepted blocked default memory root", local)
		}
	}
}

func TestManagerSharedMemorySurvivesForkAndRestoration(t *testing.T) {
	for _, scheduled := range []bool{false, true} {
		name := "ordinary"
		if scheduled {
			name = "scheduled"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			memoryRoot := filepath.Join(root, "memory")
			store, err := memory.NewStore(ctx, memoryRoot, nil)
			if err != nil {
				t.Fatal(err)
			}
			cfg := managerTestConfig(filepath.Join(root, "workspaces"), &FakeProvider{})
			cfg.Services.Memory = store
			cfg.Services.MemoryTools = true
			cfg.Services.StateStore = newRuntimeStateStore()
			manager := makeManager(t, cfg)
			alice := createManaged(t, manager, CreateSessionRequest{Owner: "alice"})
			bob := createManaged(t, manager, CreateSessionRequest{Owner: "bob"})
			anonymous := createManaged(t, manager, CreateSessionRequest{Owner: "anonymous"})
			for _, session := range []*ManagedSession{alice, bob, anonymous} {
				if session.core.memory == nil || session.core.memory.Owner() != memory.OwnerID(session.core.owner) {
					t.Fatal("shared memory not bound to admitted owner")
				}
				out := dispatchManagerMemory(t, session, protocol.RememberToolInput(protocol.RememberInput{Name: "private", Content: string(session.core.owner) + " private fact"}))
				if out.Failed || out.Denied {
					t.Fatal(out)
				}
			}
			fork, err := manager.Fork(ctx, "alice", alice.ID())
			if err != nil {
				t.Fatal(err)
			}
			checkManagerMemoryIsolation(t, fork, "alice")
			if out := dispatchManagerMemory(t, fork, protocol.RememberToolInput(protocol.RememberInput{Name: "fork", Content: "fork shared fact"})); out.Failed || out.Denied {
				t.Fatal(out)
			}
			if out := dispatchManagerMemory(t, alice, protocol.RecallToolInput(protocol.RecallInput{})); !strings.Contains(out.Output, "fork shared fact") {
				t.Fatal("fork did not reuse shared owner storage", out)
			}
			if err := manager.Stop(ctx); err != nil {
				t.Fatal(err)
			}
			// Reopen the file store; the injected state test double establishes the
			// manager restoration path, not native SQLite restart evidence.
			cfg.Services.Memory, err = memory.NewStore(ctx, memoryRoot, nil)
			if err != nil {
				t.Fatal(err)
			}
			restarted := makeManager(t, cfg)
			if scheduled {
				restored, err := restarted.RestoreScheduledSession(ctx, alice.ID())
				if err != nil {
					t.Fatal(err)
				}
				checkManagerMemoryIsolation(t, restored, "alice")
				missing, err := restarted.RestoreScheduledSession(ctx, "missing")
				if err != nil {
					t.Fatal(err)
				}
				checkManagerMemoryIsolation(t, missing, "anonymous")
			} else {
				rows, err := restarted.RestoreSessions(ctx)
				if err != nil || len(rows) != 4 {
					t.Fatal(rows, err)
				}
				for _, restored := range rows {
					checkManagerMemoryIsolation(t, restored, restored.core.owner)
				}
			}
		})
	}
}

func dispatchManagerMemory(t *testing.T, session *ManagedSession, input protocol.ToolInput) ToolOutcome {
	t.Helper()
	core := session.core
	out, err := core.gate.Dispatch(context.Background(), ToolAuthority{SessionID: core.id, OwnerID: core.owner, Workspace: core.executionRoot(), Mode: core.permissionMode()}, ToolCall{ID: "probe", Input: input})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func checkManagerMemoryIsolation(t *testing.T, session *ManagedSession, owner OwnerID) {
	t.Helper()
	if session.core.owner != owner || session.core.memory == nil || session.core.memory.Owner() != memory.OwnerID(owner) {
		t.Fatal("restored memory binding differs", owner)
	}
	out := dispatchManagerMemory(t, session, protocol.RecallToolInput(protocol.RecallInput{}))
	if out.Failed || out.Denied || !strings.Contains(out.Output, string(owner)+" private fact") {
		t.Fatal("owner memory missing", owner, out)
	}
	for _, foreign := range []OwnerID{"alice", "bob", "anonymous"} {
		if foreign != owner && strings.Contains(out.Output, string(foreign)+" private fact") {
			t.Fatal("foreign memory exposed", owner, foreign, out)
		}
	}
}

func TestManagerOwnerResourcesPrecedeSharedMemoryAndToolsRemainExplicit(t *testing.T) {
	ctx := context.Background()
	shared, err := memory.NewStore(ctx, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	bound, err := memory.Bind(shared, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bound.Write(ctx, memory.Input{Name: "legacy", Body: "legacy shared fact", Type: memory.Project, Origin: memory.Explicit}); err != nil {
		t.Fatal(err)
	}
	resolver := ownerResourceResolver(t, t.TempDir(), skills.EmptyCatalog())
	resources, err := resolver.ForOwner(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resources.Memory().Write(ctx, memory.Input{Name: "local", Body: "owner local fact", Type: memory.Project, Origin: memory.Explicit}); err != nil {
		t.Fatal(err)
	}
	cfg := managerTestConfig(t.TempDir(), &FakeProvider{})
	cfg.Services.Memory, cfg.Services.UserResources, cfg.Services.MemoryTools = shared, resolver, true
	manager := makeManager(t, cfg)
	session := createManaged(t, manager, CreateSessionRequest{Owner: "alice"})
	out := dispatchManagerMemory(t, session, protocol.RecallToolInput(protocol.RecallInput{}))
	if session.core.memory != resources.Memory() || out.Failed || !strings.Contains(out.Output, "owner local fact") || strings.Contains(out.Output, "legacy shared fact") {
		t.Fatal("owner resource precedence differs", out)
	}
	// A shared storage binding alone does not expand the default tool catalogue.
	cfg.Services.UserResources, cfg.Services.MemoryTools = nil, false
	defaultSession := createManaged(t, makeManager(t, cfg), CreateSessionRequest{Owner: "alice"})
	if defaultSession.core.memory == nil {
		t.Fatal("default runtime lost storage binding")
	}
	for _, tool := range []protocol.ToolName{protocol.ToolRemember, protocol.ToolRecall} {
		if _, ok := defaultSession.core.gate.catalog.Lookup(tool); ok {
			t.Fatal("memory tool activated without selection", tool)
		}
	}
}

func TestManagerMemoryResolverFailureCannotUseFallback(t *testing.T) {
	ctx := context.Background()
	shared, err := memory.NewStore(ctx, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	resolver := ownerResourceResolver(t, root, skills.EmptyCatalog())
	key, err := userresources.OwnerDirectoryKey("alice")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, string(key)), []byte("blocked owner directory"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := managerTestConfig(t.TempDir(), &FakeProvider{})
	cfg.Services.Memory, cfg.Services.UserResources = shared, resolver
	manager := makeManager(t, cfg)
	if _, err := manager.Create(ctx, CreateSessionRequest{Owner: "alice"}); err == nil {
		t.Fatal("failed owner resource admission used shared fallback")
	}
	if len(manager.List("alice")) != 0 {
		t.Fatal("failed owner resource admission published a handle")
	}
}
