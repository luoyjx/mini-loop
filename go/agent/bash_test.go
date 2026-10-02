//go:build darwin || linux

package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/secrets"
	"github.com/luoyjx/mini-loop/go/shell"
)

func TestRealBashGatePreservesFailureMetadataAndSettlesBeforeObservers(t *testing.T) {
	root := t.TempDir()
	registry := secrets.New(secrets.Config{})
	registry.RegisterValue("BASH_GATE_TOKEN", "gate-credential-value")
	executor, err := shell.New(shell.Config{Workspace: root, Secrets: registry})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := NewBashToolCatalog(executor)
	if err != nil {
		t.Fatal(err)
	}
	journal, _ := NewInMemoryActionJournal(10)
	observations := 0
	gate, err := NewJournaledToolGate(catalog, DefaultPermissionPolicy(nil), GateHooks{
		After: []AfterHook{afterHookFunc(func(_ context.Context, _ ToolAuthority, _ ToolCall, output string) (string, error) {
			return output + "\ngate-credential-value", nil
		})},
		Observers: []ResultObserver{observerFunc(func(ctx context.Context, authority ToolAuthority, _ ToolCall, outcome ToolOutcome) error {
			observations++
			record, exists, err := journal.Get(ctx, authority.ActionID)
			if err != nil || !exists || record.Status != ActionFailed || record.Result == nil || !outcome.Replayed && *record.Result != outcome.Output {
				t.Fatal("observer saw an unsettled failed action")
			}
			if !outcome.Replayed {
				metadata, ok := outcome.CommandResult()
				if !ok || metadata.ExitCode == nil || *metadata.ExitCode != 7 || metadata.TimedOut || metadata.Overflowed {
					t.Fatal("command metadata lost")
				}
				*metadata.ExitCode = 999
				again, _ := outcome.CommandResult()
				if *again.ExitCode != 7 {
					t.Fatal("metadata aliases observer copy")
				}
			}
			return nil
		})},
	}, journal)
	if err != nil {
		t.Fatal(err)
	}
	gate.secrets = registry
	authority := gateTestAuthority(ModeAuto)
	authority.Workspace = executor.Workspace()
	authority.RunContext = actionContext(t)
	call := gateTestCall("printf '%s' \"$BASH_GATE_TOKEN\"; printf landed > canary; exit 7")
	outcome, err := gate.Dispatch(context.Background(), authority, call)
	if err != nil || !outcome.Failed || outcome.Output != secrets.Mask+"\n(exit 7)\n"+secrets.Mask {
		t.Fatalf("outcome %#v, err %v", outcome, err)
	}
	if err := os.Remove(filepath.Join(root, "canary")); err != nil {
		t.Fatal(err)
	}
	replay, err := gate.Dispatch(context.Background(), authority, call)
	if err != nil || !replay.Replayed || replay.Failed || observations != 2 {
		t.Fatal("terminal replay changed source semantics")
	}
	if _, ok := replay.CommandResult(); ok {
		t.Fatal("replay invented process metadata")
	}
	if _, err := os.Stat(filepath.Join(root, "canary")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("terminal replay executed shell")
	}
}
func TestRealBashWorkspaceBindingAndReadonly(t *testing.T) {
	root := t.TempDir()
	executor, err := shell.New(shell.Config{Workspace: root})
	if err != nil {
		t.Fatal(err)
	}
	catalog, _ := NewBashToolCatalog(executor)
	gate, _ := NewToolGate(catalog, DefaultPermissionPolicy(nil), GateHooks{})
	authority := gateTestAuthority(ModeAuto)
	authority.Workspace = t.TempDir()
	outcome, err := gate.Dispatch(context.Background(), authority, gateTestCall("touch canary"))
	if err != nil || !outcome.Failed {
		t.Fatal("foreign workspace not rejected")
	}
	authority.Workspace = executor.Workspace()
	authority.Mode = ModeReadonly
	outcome, err = gate.Dispatch(context.Background(), authority, gateTestCall("touch canary"))
	if err != nil || !outcome.Denied {
		t.Fatal("readonly executor bypassed gate")
	}
	if _, err := os.Stat(filepath.Join(root, "canary")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("denied effect landed")
	}
}
func TestRuntimeUsesRealBashAndRepairsCancellation(t *testing.T) {
	root := t.TempDir()
	executor, err := shell.New(shell.Config{Workspace: root})
	if err != nil {
		t.Fatal(err)
	}
	session, err := NewRuntimeSession(RuntimeConfig{ID: "shell", Owner: "owner", Workspace: root, Provider: FakeProvider{}, Bash: executor, Mode: ModeAuto, MaxRounds: 3})
	if err != nil {
		t.Fatal(err)
	}
	answer, err := session.Run(context.Background(), "real-shell")
	if err != nil || answer != "Done. Tool said: handled: real-shell" {
		t.Fatalf("answer %q, error %v", answer, err)
	}
	if err := protocol.ValidateTranscript(session.Messages()); err != nil {
		t.Fatal(err)
	}
	// The fake's command includes this trusted test prompt. Wait for a canary
	// before cancelling, so the assertion measures a running process.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := session.Run(ctx, "x; touch ready; sleep 10"); done <- err }()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(root, "ready")); err == nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if _, err := os.Stat(filepath.Join(root, "ready")); err != nil {
		t.Fatal("shell never started")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel error %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancel blocked")
	}
	if executor.Interrupt() != 0 {
		t.Fatal("cancel left foreground process live")
	}
	if err := protocol.ValidateTranscript(session.Messages()); err != nil {
		t.Fatal("cancelled batch not paired:", err)
	}
}

func TestRuntimeRegistryBindsSharedExecutorWithoutChangingOtherSessions(t *testing.T) {
	root := t.TempDir()
	executor, err := shell.New(shell.Config{Workspace: root})
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"credential-alpha", "credential-bravo"} {
		registry := secrets.New(secrets.Config{})
		registry.RegisterValue("SESSION_BASH_TOKEN", value)
		session, err := NewRuntimeSession(RuntimeConfig{ID: SessionID(value), Owner: "owner", Workspace: root, Provider: FakeProvider{}, Bash: executor, Secrets: registry, Mode: ModeAuto, MaxRounds: 3})
		if err != nil {
			t.Fatal(err)
		}
		outcome, err := session.gate.Dispatch(context.Background(), ToolAuthority{SessionID: session.ID(), OwnerID: session.Owner(), Workspace: session.workspace, Mode: ModeAuto}, gateTestCall("printf '%s' \"$SESSION_BASH_TOKEN\""))
		if err != nil || outcome.Output != secrets.Mask {
			t.Fatal("session registry did not reach command environment and direct mask")
		}
	}
	output, err := executor.ExecuteBash(context.Background(), protocol.BashInput{Command: "printf '%s' \"${SESSION_BASH_TOKEN-unset}\""})
	if err != nil || output != "unset" {
		t.Fatal("runtime changed shared caller executor credential scope")
	}
}

func TestBashOnlyConstructorDerivesRealExecutorWorkspace(t *testing.T) {
	executor, err := shell.New(shell.Config{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	session, err := NewSession("shell-only", "owner", FakeProvider{}, executor, 3)
	if err != nil {
		t.Fatal(err)
	}
	if session.workspace != executor.Workspace() || session.files == nil {
		t.Fatal("bound executor workspace was lost")
	}
	answer, err := session.Run(context.Background(), "bound")
	if err != nil || answer != "Done. Tool said: handled: bound" {
		t.Fatalf("answer %q, error %v", answer, err)
	}
}
