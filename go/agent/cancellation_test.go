package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/luoyjx/mini-loop/go/protocol"
)

type cancelledBatchProvider struct{}

func (cancelledBatchProvider) Complete(context.Context, []protocol.Message) (protocol.ModelReply, error) {
	return fakeReply([]protocol.Block{
		protocol.NewBashUse("u1", "echo first"),
		protocol.NewBashUse("u2", "echo second"),
	}, protocol.StopToolUse), nil
}

type blockingExecutor struct {
	entered       chan struct{}
	calls         int
	completeFirst bool
}

func (executor *blockingExecutor) ExecuteBash(ctx context.Context, _ protocol.BashInput) (string, error) {
	executor.calls++
	if executor.completeFirst && executor.calls == 1 {
		return "first completed", nil
	}
	close(executor.entered)
	<-ctx.Done()
	return "", ctx.Err()
}

func TestCancelledBatchPairsEveryToolUseAsUnknown(t *testing.T) {
	executor := &blockingExecutor{entered: make(chan struct{})}
	session, err := NewSession("s", "owner", cancelledBatchProvider{}, executor, 2)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, runErr := session.Run(ctx, "go"); done <- runErr }()
	<-executor.entered
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("run ended with %v", err)
	}
	if executor.calls != 1 {
		t.Fatalf("cancelled batch ran %d tools", executor.calls)
	}
	messages := session.Messages()
	if err := protocol.ValidateTranscript(messages); err != nil {
		t.Fatal(err)
	}
	blocks, ok := messages[len(messages)-1].Content.Blocks()
	if !ok || len(blocks) != 2 {
		t.Fatalf("missing paired results: %+v", messages)
	}
	for _, block := range blocks {
		result, ok := block.ToolResult()
		if !ok || result.Content != unknownToolResult || result.IsError {
			t.Fatalf("cancelled effect was mislabeled: %+v", result)
		}
	}
}

func TestCancelledBatchPreservesCompletedResult(t *testing.T) {
	executor := &blockingExecutor{entered: make(chan struct{}), completeFirst: true}
	session, err := NewSession("s", "owner", cancelledBatchProvider{}, executor, 2)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, runErr := session.Run(ctx, "go"); done <- runErr }()
	<-executor.entered
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("run ended with %v", err)
	}
	if executor.calls != 2 {
		t.Fatalf("cancelled batch ran %d tools", executor.calls)
	}
	messages := session.Messages()
	if err := protocol.ValidateTranscript(messages); err != nil {
		t.Fatal(err)
	}
	blocks, ok := messages[len(messages)-1].Content.Blocks()
	if !ok || len(blocks) != 2 {
		t.Fatalf("missing paired results: %+v", messages)
	}
	first, ok := blocks[0].ToolResult()
	if !ok || first.ToolUseID != "u1" || first.Content != "first completed" || first.IsError {
		t.Fatalf("completed effect was lost: %+v", first)
	}
	second, ok := blocks[1].ToolResult()
	if !ok || second.ToolUseID != "u2" || second.Content != unknownToolResult || second.IsError {
		t.Fatalf("interrupted effect was mislabeled: %+v", second)
	}
}
