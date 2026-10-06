package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/memory"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/secrets"
	"github.com/luoyjx/mini-loop/go/skills"
)

func TestMemoryToolsMatchActualSourceOwnerIsolationAndGate(t *testing.T) {
	raw, err := os.ReadFile("../testdata/python-memory-tools.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Metadata []struct {
			Name         protocol.ToolName
			Risk         ToolRisk
			Readonly     bool
			ParallelSafe bool `json:"parallel_safe"`
			Capabilities []Capability
		}
		Steps []struct {
			Owner          OwnerID
			Name           protocol.ToolName
			Input          json.RawMessage
			Output         string
			Failed, Denied bool
		}
	}
	if err := json.Unmarshal(raw, &fixture); err != nil || len(fixture.Steps) != 14 {
		t.Fatal(err)
	}
	ctx := context.Background()
	store, err := memory.NewStore(ctx, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	sessions := map[OwnerID]*Session{}
	for _, owner := range []OwnerID{"alice", "bob", "readonly"} {
		bound, err := memory.Bind(store, memory.OwnerID(owner))
		if err != nil {
			t.Fatal(err)
		}
		cfg := runtimeConfig(t.TempDir(), &FakeProvider{})
		cfg.Owner, cfg.Memory, cfg.MemoryTools, cfg.Mode = owner, bound, true, ModeAuto
		if owner == "readonly" {
			cfg.Mode = ModeReadonly
		}
		session, err := NewRuntimeSession(cfg)
		if err != nil {
			t.Fatal(err)
		}
		sessions[owner] = session
		for _, expected := range fixture.Metadata {
			definition, ok := session.gate.catalog.Lookup(expected.Name)
			if !ok || definition.risk != expected.Risk || definition.readonly != expected.Readonly || definition.parallelSafe != expected.ParallelSafe || len(definition.capabilities) != len(expected.Capabilities) {
				t.Fatal("metadata differs", expected)
			}
		}
	}
	for i, step := range fixture.Steps {
		input, err := protocol.DecodeToolInput(step.Name, step.Input)
		if err != nil {
			t.Fatal(i, err)
		}
		session := sessions[step.Owner]
		out, err := session.gate.Dispatch(ctx, ToolAuthority{SessionID: session.id, OwnerID: step.Owner, Workspace: session.executionRoot(), Mode: session.permissionMode()}, ToolCall{ID: "probe", Input: input})
		if err != nil || out.Failed != step.Failed || out.Denied != step.Denied || !out.Denied && out.Output != step.Output {
			t.Fatalf("step %d: %#v err=%v want=%#v", i, out, err, step)
		}
	}
	readonly, _ := memory.Bind(store, "readonly")
	if records, err := readonly.List(ctx); err != nil || len(records) != 0 {
		t.Fatal("readonly persisted memory", records, err)
	}
}

func TestMemoryToolsValidateBindingBeforeWritesAndKeepDefaultOff(t *testing.T) {
	ctx := context.Background()
	store, err := memory.NewStore(ctx, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	foreign, _ := memory.Bind(store, "foreign")
	root := filepath.Join(t.TempDir(), "uncreated")
	for _, bound := range []*memory.ScopedStore{nil, foreign} {
		cfg := runtimeConfig(root, &FakeProvider{})
		cfg.MemoryTools, cfg.Memory = true, bound
		if _, err := NewRuntimeSession(cfg); err == nil {
			t.Fatal("unbound or foreign memory admitted")
		}
		if _, err := os.Stat(root); !os.IsNotExist(err) {
			t.Fatal("invalid memory caused filesystem effects", err)
		}
	}
	cfg := runtimeConfig(t.TempDir(), &FakeProvider{})
	session, err := NewRuntimeSession(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := session.gate.catalog.Lookup(protocol.ToolRemember); ok {
		t.Fatal("memory became default")
	}
}

func TestMemoryToolsMaskDiskArgumentsAndRejectForeignAuthority(t *testing.T) {
	ctx := context.Background()
	registry := secrets.New(secrets.Config{})
	registry.RegisterValue("KEY", "memory-private-secret")
	root := t.TempDir()
	store, err := memory.NewStore(ctx, root, registry)
	if err != nil {
		t.Fatal(err)
	}
	bound, _ := memory.Bind(store, "owner")
	cfg := runtimeConfig(t.TempDir(), &FakeProvider{})
	cfg.Memory, cfg.MemoryTools, cfg.Secrets, cfg.Mode = bound, true, registry, ModeAuto
	session, err := NewRuntimeSession(cfg)
	if err != nil {
		t.Fatal(err)
	}
	authority := ToolAuthority{SessionID: session.id, OwnerID: "owner", Workspace: session.executionRoot(), Mode: ModeAuto}
	call := ToolCall{ID: "remember", Input: protocol.RememberToolInput(protocol.RememberInput{Name: "fact", Content: "memory-private-secret"})}
	authority.OwnerID = "foreign"
	if out, err := session.gate.Dispatch(ctx, authority, call); err == nil && !out.Failed {
		t.Fatal("foreign handler admitted", out)
	}
	if records, _ := bound.List(ctx); len(records) != 0 {
		t.Fatal("foreign memory write", records)
	}
	authority.OwnerID = "owner"
	if out, err := session.gate.Dispatch(ctx, authority, call); err != nil || out.IsError() {
		t.Fatal(out, err)
	}
	files, _ := os.ReadDir(root)
	for _, file := range files {
		raw, err := os.ReadFile(filepath.Join(root, file.Name()))
		if err != nil || strings.Contains(string(raw), "memory-private-secret") {
			t.Fatal("unmasked memory disk sink", string(raw), err)
		}
	}
	out, err := session.gate.Dispatch(ctx, authority, ToolCall{ID: "recall", Input: protocol.RecallToolInput(protocol.RecallInput{})})
	if err != nil || out.IsError() || strings.Contains(out.Output, "memory-private-secret") {
		t.Fatal(out, err)
	}
}

func TestManagedMemoryToolsAndSelectedChildRetainOwnerStore(t *testing.T) {
	ctx := context.Background()
	resolver := ownerResourceResolver(t, t.TempDir(), skills.EmptyCatalog())
	bound, err := resolver.ForOwner(ctx, "owner")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bound.Memory().Write(ctx, memory.Input{Name: "parent", Type: memory.Project, Body: "parent memory", Origin: memory.Explicit}); err != nil {
		t.Fatal(err)
	}
	provider := resourceProvider{tools: []protocol.Block{protocol.NewToolUse("recall", protocol.RecallToolInput(protocol.RecallInput{}))}}
	cfg := managerTestConfig(t.TempDir(), provider)
	cfg.Services.UserResources, cfg.Services.MemoryTools, cfg.Services.RoleToolPolicy = resolver, true, allRoleTools{}
	manager := makeManager(t, cfg)
	parent := createManaged(t, manager, CreateSessionRequest{Owner: "owner"})
	if parent.core.memory != bound.Memory() {
		t.Fatal("managed memory binding differs")
	}
	if _, err := parent.core.Delegate(ctx, "recall memory", RoleExplore); err != nil {
		t.Fatal(err)
	}
	served := false
	for _, event := range parent.core.Events() {
		if result, ok := event.Event.ToolResult(); ok && result.Name == protocol.ToolRecall {
			if result.Failed || !strings.Contains(result.Output, "parent memory") {
				t.Fatal(result)
			}
			served = true
		}
	}
	if !served {
		t.Fatal("child did not recall bound memory")
	}
}
