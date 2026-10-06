package agent

import (
	"context"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/memory"
	"github.com/luoyjx/mini-loop/go/protocol"
)

func TestRememberWaitsForMemoryLifecycleAndCancellationCannotWrite(t *testing.T) {
	ctx := context.Background()
	store, err := memory.NewStore(ctx, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	bound, _ := memory.Bind(store, "alice")
	cfg := runtimeConfig(t.TempDir(), &FakeProvider{})
	cfg.Owner, cfg.Memory, cfg.MemoryTools, cfg.Mode = "alice", bound, true, ModeAuto
	session, err := NewRuntimeSession(cfg)
	if err != nil {
		t.Fatal(err)
	}
	input, err := protocol.DecodeToolInput(protocol.ToolRemember, []byte(`{"name":"blocked","content":"must not persist"}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := bound.WithLifecycle(ctx, func() error {
		waiting, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
		defer cancel()
		_, err := session.gate.Dispatch(waiting, ToolAuthority{SessionID: session.id, OwnerID: "alice", Workspace: session.executionRoot(), Mode: ModeAuto}, ToolCall{ID: "blocked", Input: input})
		if err != context.DeadlineExceeded {
			t.Errorf("remember bypassed lifecycle or swallowed cancellation: %v", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	records, err := bound.List(ctx)
	if err != nil || len(records) != 0 {
		t.Fatal("cancelled remember changed memory", records, err)
	}
	outcome, err := session.gate.Dispatch(ctx, ToolAuthority{SessionID: session.id, OwnerID: "alice", Workspace: session.executionRoot(), Mode: ModeAuto}, ToolCall{ID: "released", Input: input})
	if err != nil || outcome.Failed || outcome.Denied {
		t.Fatal(outcome, err)
	}
	records, err = bound.List(ctx)
	if err != nil || len(records) != 1 || records[0].Owner != "alice" {
		t.Fatal(records, err)
	}
}
