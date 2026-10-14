package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/luoyjx/mini-loop/go/protocol"
)

type stuckFollowupCompactor struct {
	change  bool
	failure error
}

func (compactor stuckFollowupCompactor) MaybeCompact(_ context.Context, value CompactionContext) (CompactionResult, error) {
	messages := append([]protocol.Message(nil), value.Messages...)
	if compactor.change {
		messages[0] = protocol.Message{Role: protocol.RoleUser, Content: protocol.PlainContent("rewritten prior prompt")}
	}
	return CompactionResult{Messages: messages}, compactor.failure
}

func (compactor stuckFollowupCompactor) Compact(ctx context.Context, value CompactionContext) (CompactionResult, error) {
	return compactor.MaybeCompact(ctx, value)
}

func stuckFollowupFaultRow(t *testing.T) stuckThresholdLoop {
	t.Helper()
	for _, row := range readStuckThresholdFixture(t).Loops {
		if row.Name == "repeat-result-negative-fault" {
			return row
		}
	}
	t.Fatal("missing actual Source fault recipe")
	return stuckThresholdLoop{}
}

func TestStuckFollowupDoesNotAdmitChangedHistoryOrOtherFailures(t *testing.T) {
	for _, test := range []string{"changed-prefix", "extra-gap", "custom-same-text", "other-named-batch", "canceled"} {
		t.Run(test, func(t *testing.T) {
			row := stuckFollowupFaultRow(t)
			core, _, provider, handler := newStuckThresholdLoop(t, row, false)
			_, err := core.RunWithContext(context.Background(), row.Turns[0].Prompt, stuckThresholdRunContext(t, 1))
			assertStuckThresholdFault(t, err, row.Turns[0].Error)
			if core.unpublishedBatch == nil {
				t.Fatal("actual built-in fault did not record its retained batch")
			}
			provider.requests, provider.turnCalls = nil, 0
			handler.commands = nil
			ctx := context.Background()
			var custom error
			switch test {
			case "changed-prefix":
				core.compactor = stuckFollowupCompactor{change: true}
			case "extra-gap":
				core.appendMessages(protocol.Message{Role: protocol.RoleUser, Content: protocol.PlainContent("gap separator")}, protocol.Message{Role: protocol.RoleAssistant, Content: protocol.BlockContent(protocol.NewBashUse("unregistered", "never"))})
			case "custom-same-text":
				custom = errors.New(protocol.ValidateTranscript(core.messages).Error())
				core.compactor = stuckFollowupCompactor{failure: custom}
			case "other-named-batch":
				custom = protocol.ValidateTranscript([]protocol.Message{{Role: protocol.RoleAssistant, Content: protocol.BlockContent(protocol.NewBashUse("other", "never"))}})
				core.compactor = stuckFollowupCompactor{failure: custom}
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			_, err = core.RunWithContext(ctx, row.Turns[1].Prompt, stuckThresholdRunContext(t, 2))
			if err == nil || (custom != nil && !errors.Is(err, custom)) {
				t.Fatalf("changed history/custom failure was swallowed: %v; original %v", err, custom)
			}
			if len(provider.requests) != 0 || len(handler.commands) != 0 {
				t.Fatal("guard failure reached provider or repeated the completed effect")
			}
		})
	}
}

func TestStuckFollowupObservationIsNotRebuiltFromMessages(t *testing.T) {
	row := stuckFollowupFaultRow(t)
	original, _, _, _ := newStuckThresholdLoop(t, row, false)
	_, err := original.RunWithContext(context.Background(), row.Turns[0].Prompt, stuckThresholdRunContext(t, 1))
	assertStuckThresholdFault(t, err, row.Turns[0].Error)
	copySession, _, provider, handler := newStuckThresholdLoop(t, row, false)
	copySession.messages = original.Messages()
	_, err = copySession.RunWithContext(context.Background(), "copied history", stuckThresholdRunContext(t, 2))
	if err == nil || len(provider.requests) != 0 || len(handler.commands) != 0 {
		t.Fatalf("history alone recreated dispatch provenance: %v", err)
	}
}
