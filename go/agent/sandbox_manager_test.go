package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/sandbox"
	"github.com/luoyjx/mini-loop/go/secrets"
)

func TestManagerSandboxRejectsCompetingFactoryBeforeAllocation(t *testing.T) {
	root := filepath.Join(t.TempDir(), "not-created")
	cfg := managerTestConfig(root, &recordingProvider{})
	cfg.Services.Sandbox = sandbox.NewUnavailable("fixture unavailable")
	called := false
	cfg.Services.BashFactory = bashFactoryFunc(func(context.Context, SessionBinding) (BashExecutor, error) {
		called = true
		return nil, nil
	})
	manager, err := NewSessionManager(cfg)
	if manager != nil || err == nil || !strings.Contains(err.Error(), "cannot both") {
		t.Fatal(manager, err)
	}
	if called {
		t.Fatal("conflicting factory was invoked")
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("invalid config allocated workspace", err)
	}
}

func TestManagerUnavailableSandboxReportsBeforeAndAfterSessionCreation(t *testing.T) {
	cfg := managerTestConfig(t.TempDir(), &recordingProvider{})
	cfg.Services.Sandbox = sandbox.NewUnavailable("fixture unavailable")
	manager := makeManager(t, cfg)
	for index := 0; index < 2; index++ {
		view, err := manager.ObserveRuntimePosture(false, secrets.Environment{})
		if err != nil || view.Sandbox != "UnavailableSandbox" || view.SandboxReason == nil || *view.SandboxReason != "fixture unavailable" {
			t.Fatal(view, err)
		}
		*view.SandboxReason = "changed"
		if index == 0 {
			createManaged(t, manager, CreateSessionRequest{Owner: "alice"})
		}
	}
}

func TestManagedSeatbeltTurnsChildrenAndWorkspaceRebinding(t *testing.T) {
	if !sandbox.Available() {
		t.Skip(*sandbox.UnavailableReason())
	}
	root := t.TempDir()
	t.Setenv("HOME", root)
	credential := filepath.Join(root, ".ssh", "canary")
	if err := os.Mkdir(filepath.Dir(credential), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(credential, []byte("PRIVATE-CANARY"), 0600); err != nil {
		t.Fatal(err)
	}
	extra := filepath.Join(root, "extra")
	next := filepath.Join(root, "entered")
	for _, path := range []string{extra, next} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	backend, err := sandbox.NewSeatbelt(sandbox.Config{WritableRoots: []string{filepath.Join(root, "template")}, UnreadableRoots: []string{filepath.Dir(credential)}, ExtraWritable: []string{extra}})
	if err != nil {
		t.Fatal(err)
	}
	provider := &fakeSequenceProvider{}
	cfg := managerTestConfig(filepath.Join(root, "sessions"), provider)
	cfg.Defaults.SubagentMaxRounds = 2
	cfg.Services.Sandbox = backend
	manager := makeManager(t, cfg)
	view, err := manager.ObserveRuntimePosture(false, secrets.Environment{})
	if err != nil || view.Sandbox != "SeatbeltSandbox" || view.SandboxReason != nil || len(manager.List("alice")) != 0 {
		t.Fatal(view, err)
	}
	parent := createManaged(t, manager, CreateSessionRequest{Owner: "alice"})
	old := parent.Info().Workspace
	quote := func(path string) string { return "'" + strings.ReplaceAll(path, "'", "'\"'\"'") + "'" }
	command := func(name, deniedPath string) string {
		return "printf proof > " + name + " && printf extra > " + quote(filepath.Join(extra, name)) + "; if cat " + quote(credential) + "; then printf LEAKED; else printf READ-BLOCKED; fi; if printf forbidden > " + quote(deniedPath) + "; then printf WIDENED; else printf WRITE-BLOCKED; fi"
	}
	run := func(session *Session, name, deniedPath string) {
		t.Helper()
		provider.replies = []protocol.ModelReply{
			fakeReply([]protocol.Block{protocol.NewBashUse(name, command(name, deniedPath))}, protocol.StopToolUse),
			fakeReply([]protocol.Block{protocol.NewTextBlock("done")}, protocol.StopEndTurn),
		}
		var text string
		var err error
		if session == parent.core {
			text, err = parent.Run(context.Background(), "verify confinement")
		} else {
			text, err = session.Run(context.Background(), "verify confinement")
		}
		if err != nil || text != "done" {
			t.Fatal(text, err)
		}
		// The actual model-facing result must report both denied effects.
		resultText := ""
		for _, message := range session.Messages() {
			blocks, _ := message.Content.Blocks()
			for _, block := range blocks {
				if result, ok := block.ToolResult(); ok && result.ToolUseID == name {
					resultText = result.Content
				}
			}
		}
		if !strings.Contains(resultText, "READ-BLOCKED") || !strings.Contains(resultText, "WRITE-BLOCKED") || strings.Contains(resultText, "PRIVATE-CANARY") {
			t.Fatal("model result lost confinement", resultText)
		}
		if _, err := os.Stat(deniedPath); !os.IsNotExist(err) {
			t.Fatal("denied effect took place", err)
		}
		if data, err := os.ReadFile(filepath.Join(session.executionWorkspace, name)); err != nil || string(data) != "proof" {
			t.Fatal("workspace write missing", err)
		}
		if data, err := os.ReadFile(filepath.Join(extra, name)); err != nil || string(data) != "extra" {
			t.Fatal("explicit extra root was lost", err)
		}
	}
	run(parent.core, "parent-proof", filepath.Join(root, "outside"))
	contextSnapshot, err := DefaultRunContext()
	if err != nil {
		t.Fatal(err)
	}
	pinned := parent.core.subagentParent(contextSnapshot)
	child, _, err := newSubagent(context.Background(), SubagentRequest{Parent: pinned, Role: RoleWorker, Prompt: "verify", RunContext: contextSnapshot})
	if err != nil {
		t.Fatal(err)
	}
	if err := parent.core.runtime.enterWorkspace(context.Background(), next); err != nil {
		t.Fatal(err)
	}
	run(parent.core, "entered-proof", filepath.Join(old, "stale"))
	run(child, "child-proof", filepath.Join(next, "child-escape"))
	if child.executionWorkspace != old || parent.core.executionWorkspace == old {
		t.Fatal("child or parent scope moved incorrectly")
	}
	view, err = manager.ObserveRuntimePosture(false, secrets.Environment{})
	if err != nil || view.Sandbox != "SeatbeltSandbox" || view.SandboxReason != nil {
		t.Fatal(view, err)
	}
}
