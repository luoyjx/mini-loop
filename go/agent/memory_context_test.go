package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/memory"
	"github.com/luoyjx/mini-loop/go/protocol"
)

type memoryChildProvider struct{}

func (memoryChildProvider) Complete(_ context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
	if request.Purpose == protocol.PurposeMemorySelection {
		return fakeReply([]protocol.Block{protocol.NewTextBlock("[0]")}, protocol.StopEndTurn), nil
	}
	for _, message := range request.Messages {
		if blocks, ok := message.Content.Blocks(); ok {
			for _, block := range blocks {
				if _, ok := block.ToolResult(); ok {
					return fakeReply([]protocol.Block{protocol.NewTextBlock("done")}, protocol.StopEndTurn), nil
				}
			}
		}
	}
	return fakeReply([]protocol.Block{protocol.NewToolUse("recall", protocol.RecallToolInput(protocol.RecallInput{}))}, protocol.StopToolUse), nil
}

type memorySelectionProvider struct {
	output   string
	fault    bool
	requests []protocol.ModelRequest
}

func (p *memorySelectionProvider) Complete(_ context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
	p.requests = append(p.requests, request.Clone())
	if request.Purpose == protocol.PurposeMemorySelection {
		if p.fault {
			return protocol.ModelReply{}, errors.New("selection failed")
		}
		reply := fakeReply([]protocol.Block{protocol.NewTextBlock(p.output)}, protocol.StopEndTurn)
		reply.Usage.InputTokens, reply.Usage.OutputTokens = 777, 3
		return reply, nil
	}
	return fakeReply([]protocol.Block{protocol.NewTextBlock("done")}, protocol.StopEndTurn), nil
}

func TestMemoryContextMatchesActualSourceSelectionAndRuntimeFacts(t *testing.T) {
	raw, err := os.ReadFile("../testdata/python-memory-context.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name, Query, Reply                   string
			Fault, Empty, Auto, Recall, Remember bool
			Prepared                             string
			Calls                                []struct {
				Model     string
				MaxTokens int `json:"max_tokens"`
				Messages  []protocol.Message
				System    *string
				Tools     []protocol.ToolSchema
			}
			Events []MemoryEvent
			Facts  []protocol.Message
		}
	}
	if err := json.Unmarshal(raw, &fixture); err != nil || len(fixture.Cases) != 12 {
		t.Fatal("incomplete source cases", err)
	}
	for _, row := range fixture.Cases {
		t.Run(row.Name, func(t *testing.T) {
			ctx := context.Background()
			store, err := memory.NewStore(ctx, t.TempDir(), nil)
			if err != nil {
				t.Fatal(err)
			}
			bound, _ := memory.Bind(store, "owner")
			foreign, _ := memory.Bind(store, "foreign")
			if _, err := foreign.Write(ctx, memory.Input{Name: "foreign", Type: memory.Project, Description: "foreign private", Body: "foreign memory"}); err != nil {
				t.Fatal(err)
			}
			if !row.Empty {
				for _, input := range []memory.Input{
					{Name: "alpha", Type: memory.Project, Description: "alpha facts", Body: "alpha body", Origin: memory.Explicit},
					{Name: "beta", Type: memory.Feedback, Description: "beta facts", Body: "beta body", Origin: memory.Imported},
					{Name: "gamma", Type: memory.Reference, Description: "gamma facts", Body: "gamma body", Origin: memory.AutoExtracted},
				} {
					if _, err := bound.Write(ctx, input); err != nil {
						t.Fatal(err)
					}
				}
			}
			provider := &memorySelectionProvider{output: row.Reply, fault: row.Fault}
			cfg := runtimeConfig(t.TempDir(), provider)
			cfg.MemoryTools, cfg.MemoryAuto, cfg.Memory, cfg.CachePolicy = true, &row.Auto, bound, NullCachePolicy{}
			session, err := NewRuntimeSession(cfg)
			if err != nil {
				t.Fatal(err)
			}
			var definitions []ToolDefinition
			for _, definition := range session.gate.catalog.ordered {
				if definition.name == protocol.ToolRemember && !row.Remember || definition.name == protocol.ToolRecall && !row.Recall {
					continue
				}
				definitions = append(definitions, definition)
			}
			session.gate.catalog, err = NewToolCatalog(definitions...)
			if err != nil {
				t.Fatal(err)
			}
			before := session.meter.Snapshot()
			got, err := session.prepareMemoryContext(ctx, row.Query)
			if err != nil || got != row.Prepared {
				t.Fatal(got, row.Prepared, err)
			}
			if len(provider.requests) != len(row.Calls) || !reflect.DeepEqual(before, session.meter.Snapshot()) {
				t.Fatal("side query changed meter or call count")
			}
			for i, call := range provider.requests {
				want := row.Calls[i]
				if call.Purpose != protocol.PurposeMemorySelection || call.Model != want.Model || call.MaxTokens != want.MaxTokens || !reflect.DeepEqual(call.Messages, want.Messages) || call.System != nil || call.Tools != nil {
					t.Fatal("selection request differs", call, want)
				}
			}
			var events []MemoryEvent
			for _, record := range session.Events() {
				if event, ok := record.Event.Memory(); ok {
					events = append(events, event)
					wire, err := json.Marshal(record)
					if err != nil {
						t.Fatal(err)
					}
					var restored SessionEventRecord
					if err := json.Unmarshal(wire, &restored); err != nil {
						t.Fatal(err)
					}
					copy, ok := restored.Event.Memory()
					if !ok || copy != event {
						t.Fatal("archival memory event changed")
					}
				}
			}
			if len(events) != len(row.Events) || len(events) != 0 && !reflect.DeepEqual(events, row.Events) {
				t.Fatal(events, row.Events)
			}
			if err := session.injectRuntimeFacts(ctx, ""); err != nil {
				t.Fatal(err)
			}
			if err := session.injectRuntimeFacts(ctx, ""); err != nil {
				t.Fatal(err)
			}
			if got := session.messages; len(got) != len(row.Facts) || len(got) != 0 && !reflect.DeepEqual(got, row.Facts) {
				t.Fatal("facts changed or repeated", got, row.Facts)
			}
		})
	}
}

func TestMemoryContextRunsAfterPromptRewriteAndBeforeMainRequest(t *testing.T) {
	ctx := context.Background()
	store, _ := memory.NewStore(ctx, t.TempDir(), nil)
	bound, _ := memory.Bind(store, "owner")
	bound.Write(ctx, memory.Input{Name: "alpha", Type: memory.Project, Description: "alpha", Body: "old fact"})
	provider := &memorySelectionProvider{output: "[0]"}
	cfg := runtimeConfig(t.TempDir(), provider)
	cfg.MemoryTools, cfg.Memory = true, bound
	cfg.UserPromptHooks = []UserPromptHook{promptHookFunc(func(_ context.Context, _ TurnContext, text string) (*string, error) {
		value := "rewritten " + text
		return &value, nil
	})}
	session, err := NewRuntimeSession(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Run(ctx, "alpha request"); err != nil {
		t.Fatal(err)
	}
	if len(provider.requests) != 2 || provider.requests[0].Purpose != protocol.PurposeMemorySelection || provider.requests[1].Purpose != protocol.PurposeAgentTurn {
		t.Fatal(provider.requests)
	}
	selection, _ := provider.requests[0].Messages[0].Content.Plain()
	main, _ := provider.requests[1].Messages[0].Content.Plain()
	if !strings.Contains(selection, "Request:\nrewritten alpha request") || !strings.Contains(main, "<memory_context>") || !strings.HasSuffix(main, "rewritten alpha request") {
		t.Fatal(selection, main)
	}
}

type cancelledMemoryProvider struct{ called chan struct{} }

func (p cancelledMemoryProvider) Complete(ctx context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
	if request.Purpose != protocol.PurposeMemorySelection {
		return protocol.ModelReply{}, errors.New("unexpected main request")
	}
	close(p.called)
	<-ctx.Done()
	return protocol.ModelReply{}, ctx.Err()
}

func TestMemorySelectionCancellationDoesNotFallbackOrAppendTurn(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	store, err := memory.NewStore(ctx, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	bound, _ := memory.Bind(store, "owner")
	if _, err := bound.Write(ctx, memory.Input{Name: "alpha", Type: memory.Project, Body: "alpha"}); err != nil {
		t.Fatal(err)
	}
	provider := cancelledMemoryProvider{called: make(chan struct{})}
	cfg := runtimeConfig(t.TempDir(), provider)
	cfg.MemoryTools, cfg.Memory = true, bound
	session, err := NewRuntimeSession(cfg)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := session.Run(ctx, "alpha"); done <- err }()
	select {
	case <-provider.called:
	case err := <-done:
		t.Fatal("selection did not start", err)
	case <-ctx.Done():
		t.Fatal("selection did not start", ctx.Err())
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if len(session.Messages()) != 0 {
		t.Fatal("cancelled selection appended live turn")
	}
	for _, record := range session.Events() {
		if _, ok := record.Event.Memory(); ok {
			t.Fatal("cancelled selection fell back")
		}
	}
}
