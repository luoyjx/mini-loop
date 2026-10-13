package agent

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/luoyjx/mini-loop/go/protocol"
)

type permissionPublicationCancelSink struct {
	capture     *permissionCapture
	cancel      context.CancelFunc
	permissions atomic.Int32
}

func (sink *permissionPublicationCancelSink) OnEvent(ctx context.Context, record SessionEventRecord) error {
	if err := sink.capture.OnEvent(ctx, record); err != nil {
		return err
	}
	if record.Event.Kind() == EventPermission && sink.permissions.Add(1) == 1 {
		// Cancel the actual run, not the observer's detached context or a
		// later approval callback. Publication is the boundary under test.
		sink.cancel()
	}
	return nil
}

func TestPermissionPublicationCancellationStopsLaterApprovalAndHandler(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	capture := &permissionCapture{}
	sink := &permissionPublicationCancelSink{capture: capture, cancel: cancel}
	handler := permissionSafeHandler{capture: capture, name: protocol.ToolBash}
	definition, err := NewToolDefinition(protocol.ToolBash, ToolTraits{Risk: RiskExternal}, handler)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := NewToolCatalog(definition)
	if err != nil {
		t.Fatal(err)
	}
	var approvalCalls, secondApprovalCalls atomic.Int32
	approver := approverFunc(func(_ context.Context, request ApprovalRequest) (bool, error) {
		approvalCalls.Add(1)
		if request.Rule == "external-action" {
			secondApprovalCalls.Add(1)
		}
		return true, nil
	})
	session, err := NewManagedSession(RuntimeConfig{
		ID: "permission-publication-cancel", Owner: "fixture-owner",
		Provider: resourceProvider{[]protocol.Block{protocol.NewBashUse("permission-tool", "rm safe")}},
		Tools:    catalog, Bash: echoExecutor{}, Workspace: t.TempDir(),
		Mode: ModeInteractive, MaxRounds: 4, Model: "fixture-model",
		Recovery: DirectRecovery{}, Approver: approver, EventSink: sink, Label: "main",
	})
	if err != nil {
		t.Fatal(err)
	}
	run, err := ExplicitHumanRunContext(HumanRunConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.RunWithContext(ctx, "observe permission cancellation", run); !errors.Is(err, context.Canceled) {
		t.Fatalf("permission publication did not cancel the run: %v", err)
	}
	if approvalCalls.Load() != 1 || secondApprovalCalls.Load() != 0 {
		t.Fatalf("approval advanced after publication cancellation: all=%d second=%d",
			approvalCalls.Load(), secondApprovalCalls.Load())
	}
	capture.mu.Lock()
	handlerCalls := len(capture.handled)
	capture.mu.Unlock()
	if handlerCalls != 0 {
		t.Fatalf("handler executed after permission cancellation: %d", handlerCalls)
	}
	permissions := 0
	for _, record := range session.Events() {
		permission, ok := record.Event.Permission()
		if !ok {
			continue
		}
		permissions++
		want := PermissionEvent{PermissionAllow, "destructive-shell", protocol.ToolBash, "Potentially destructive shell command"}
		if permission != want || record.Scope.RunContext.MessageID() != run.MessageID() || record.SessionID != session.ID() {
			t.Fatalf("first published permission lost its decision or identity: %+v", record)
		}
	}
	if permissions != 1 || sink.permissions.Load() != 1 {
		t.Fatalf("first permission was lost or a later decision was emitted: backlog=%d sink=%d",
			permissions, sink.permissions.Load())
	}
}
