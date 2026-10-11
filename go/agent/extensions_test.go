package agent

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/protocol"
)

type promptHookFunc func(context.Context, TurnContext, string) (*string, error)

func (f promptHookFunc) RewriteUserPrompt(c context.Context, v TurnContext, s string) (*string, error) {
	return f(c, v, s)
}

type namedInjector struct {
	name   string
	inject func(context.Context, TurnContext) ([]protocol.Message, error)
}

func (i namedInjector) Name() string { return i.name }
func (i namedInjector) Inject(c context.Context, v TurnContext) ([]protocol.Message, error) {
	return i.inject(c, v)
}

func TestPromptHooksChainNilEmptyAndFailBeforeAppend(t *testing.T) {
	provider := &recordingProvider{}
	var seen []string
	config := runtimeConfig(t.TempDir(), provider)
	config.UserPromptHooks = []UserPromptHook{
		promptHookFunc(func(_ context.Context, v TurnContext, s string) (*string, error) {
			seen = append(seen, s)
			if v.Authority.OwnerID != "owner" || len(v.Messages) != 0 {
				t.Fatal("unbound prompt view")
			}
			return nil, nil
		}),
		promptHookFunc(func(_ context.Context, _ TurnContext, s string) (*string, error) {
			seen = append(seen, s)
			empty := ""
			return &empty, nil
		}),
		promptHookFunc(func(_ context.Context, _ TurnContext, s string) (*string, error) {
			seen = append(seen, s)
			return nil, nil
		}),
	}
	session, err := NewRuntimeSession(config)
	if err != nil {
		t.Fatal(err)
	}
	config.UserPromptHooks[0] = nil
	if _, err = session.Run(context.Background(), "start"); err != nil {
		t.Fatal(err)
	}
	contract := readSchedulingContract(t)
	if !reflect.DeepEqual(seen, contract.PromptSeen) {
		t.Fatal(seen)
	}
	value, _ := provider.requests[0].Messages[0].Content.Plain()
	if value != contract.Prompt {
		t.Fatal("empty rewrite discarded")
	}
	failed := errors.New("prompt failure")
	session.promptHooks = []UserPromptHook{promptHookFunc(func(context.Context, TurnContext, string) (*string, error) { return nil, failed })}
	count := len(session.Messages())
	if _, err = session.Run(context.Background(), "bad"); !errors.Is(err, failed) || len(session.Messages()) != count {
		t.Fatal("failed hook appended user prompt", err)
	}
}

func TestInjectorsValidateWholeBatchAndSeePriorInjections(t *testing.T) {
	provider := &recordingProvider{}
	config := runtimeConfig(t.TempDir(), provider)
	config.Injectors = []MessageInjector{
		namedInjector{"first", func(_ context.Context, v TurnContext) ([]protocol.Message, error) {
			v.Messages[0].Role = protocol.RoleAssistant
			return []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent("first")}}, nil
		}},
		namedInjector{"broken", func(_ context.Context, v TurnContext) ([]protocol.Message, error) {
			value, _ := v.Messages[len(v.Messages)-1].Content.Plain()
			if value != "first" {
				t.Fatal("injector cannot see prior batch")
			}
			return []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent("must not append")}, {Role: "invalid", Content: protocol.PlainContent("bad")}}, nil
		}},
	}
	session, err := NewRuntimeSession(config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = session.Run(context.Background(), "start"); err == nil || !strings.Contains(err.Error(), "broken") {
		t.Fatal("invalid injector accepted", err)
	}
	messages := session.Messages()
	if len(messages) != 2 || messages[0].Role != protocol.RoleUser || len(provider.requests) != 0 {
		t.Fatal("invalid batch corrupted history")
	}
}

func TestLoopExtensionsAndPoolsReachFreshChild(t *testing.T) {
	provider := &recordingProvider{}
	config := runtimeConfig(t.TempDir(), provider)
	var promptView, injectView TurnContext
	config.UserPromptHooks = []UserPromptHook{promptHookFunc(func(_ context.Context, v TurnContext, s string) (*string, error) {
		promptView = v
		value := s + " rewritten"
		return &value, nil
	})}
	config.Injectors = []MessageInjector{namedInjector{"hint", func(_ context.Context, v TurnContext) ([]protocol.Message, error) {
		injectView = v
		return []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent("hint")}}, nil
	}}}
	modelPool, _ := NewConcurrencyLimiter(1)
	toolPool, _ := NewConcurrencyLimiter(1)
	config.ModelLimiter, config.ToolLimiter = modelPool, toolPool
	capture := &cannedSubagents{}
	config.Subagents = capture
	session, err := NewRuntimeSession(config)
	if err != nil {
		t.Fatal(err)
	}
	session.todos.Update([]protocol.TodoItem{{Content: "parent only", Status: protocol.TodoPending, ActiveForm: "Working"}})
	if _, err = session.Delegate(context.Background(), "child", RoleGeneralPurpose); err != nil {
		t.Fatal(err)
	}
	parent := capture.request.Parent
	if parent.modelLimiter != modelPool || parent.toolLimiter != toolPool {
		t.Fatal("child pool identity changed")
	}
	childProvider := &InProcessSubagents{}
	if _, err = childProvider.RunSubagent(context.Background(), capture.request); err != nil {
		t.Fatal(err)
	}
	if promptView.Depth != 1 || injectView.Depth != 1 || len(promptView.Todos) != 0 || len(promptView.Messages) != 0 || promptView.Authority.OwnerID != session.Owner() {
		t.Fatal("child shared mutable state or lost authority")
	}
	request := provider.requests[0]
	first, _ := request.Messages[0].Content.Plain()
	last, _ := request.Messages[len(request.Messages)-1].Content.Plain()
	if first != "child rewritten" || last != "hint" {
		t.Fatal("child lost loop seams")
	}
	if len(session.Messages()) != 0 {
		t.Fatal("child modified parent history")
	}
}

func TestTodoReminderCounterPersistsAcrossTurnsAndAttemptResets(t *testing.T) {
	session, err := NewRuntimeSession(runtimeConfig(t.TempDir(), &FakeProvider{}))
	if err != nil {
		t.Fatal(err)
	}
	session.stuckDetector = NullStuckDetector{}
	session.todos.Update([]protocol.TodoItem{{Content: "open", Status: protocol.TodoPending, ActiveForm: "Working"}})
	contract := readSchedulingContract(t)
	for turn, expected := range contract.TodoTurns {
		before := len(session.Messages())
		if _, err = session.Run(context.Background(), "go"); err != nil {
			t.Fatal(err)
		}
		var current []string
		for _, message := range session.Messages()[before:] {
			blocks, _ := message.Content.Blocks()
			for _, block := range blocks {
				if text, ok := block.Text(); ok && text.Text == TodoReminder {
					current = append(current, text.Text)
				}
			}
		}
		if session.roundsWithoutTodo != expected.Counter || len(current) != len(expected.Reminders) || (len(current) > 0 && !reflect.DeepEqual(current, expected.Reminders)) {
			t.Fatalf("turn %d differs from Python: %v counter=%d", turn, current, session.roundsWithoutTodo)
		}
	}
	var reminders int
	for _, message := range session.Messages() {
		blocks, _ := message.Content.Blocks()
		for _, block := range blocks {
			if text, ok := block.Text(); ok && text.Text == TodoReminder {
				reminders++
			}
		}
	}
	if reminders != 1 || session.roundsWithoutTodo != 1 {
		t.Fatalf("reminders=%d counter=%d", reminders, session.roundsWithoutTodo)
	}
	session.roundsWithoutTodo = 2
	use := protocol.NewToolUse("todo", protocol.TodoWriteToolInput(protocol.TodoWriteInput{}))
	session.remindTodos([]protocol.Block{use}, []protocol.Block{protocol.NewToolResult("todo", "denied", false)})
	if session.roundsWithoutTodo != 0 {
		t.Fatal("attempted TodoWrite did not reset nag")
	}
	session.todos.Update(nil)
	for i := 0; i < 3; i++ {
		if len(session.remindTodos([]protocol.Block{protocol.NewBashUse("x", "x")}, nil)) != 0 {
			t.Fatal("nag without open todos")
		}
	}
}

func TestRuntimeRejectsNilLoopExtensionsAndZeroPools(t *testing.T) {
	for _, kind := range []string{"hook", "injector", "pool"} {
		config := runtimeConfig("/invalid/unavailable/workspace", &FakeProvider{})
		switch kind {
		case "hook":
			config.UserPromptHooks = []UserPromptHook{nil}
		case "injector":
			config.Injectors = []MessageInjector{nil}
		case "pool":
			config.ToolLimiter = &ConcurrencyLimiter{}
		}
		if _, err := NewRuntimeSession(config); err == nil || strings.Contains(err.Error(), "workspace") {
			t.Fatalf("%s validated after filesystem: %v", kind, err)
		}
	}
}

type injectionCompactor struct {
	t     *testing.T
	calls int
}

func (c *injectionCompactor) MaybeCompact(_ context.Context, v CompactionContext) (CompactionResult, error) {
	c.calls++
	var facts, hints int
	for _, m := range v.Messages {
		if text, ok := m.Content.Plain(); ok {
			if text == "hint" {
				hints++
			}
			if strings.HasPrefix(text, "<runtime-state>") {
				facts++
			}
		}
	}
	if hints != c.calls || facts != 1 {
		c.t.Fatalf("injector/runtime facts did not precede compaction: hints=%d facts=%d round=%d", hints, facts, c.calls)
	}
	return CompactionResult{Messages: v.Messages}, nil
}
func (c *injectionCompactor) Compact(ctx context.Context, v CompactionContext) (CompactionResult, error) {
	return c.MaybeCompact(ctx, v)
}

type twoRoundInjectionProvider struct{ calls int }

func (p *twoRoundInjectionProvider) Complete(_ context.Context, r protocol.ModelRequest) (protocol.ModelReply, error) {
	p.calls++
	if p.calls == 1 {
		return fakeReply([]protocol.Block{protocol.NewBashUse("x", "go")}, protocol.StopToolUse), nil
	}
	return fakeReply([]protocol.Block{protocol.NewTextBlock("done")}, protocol.StopEndTurn), nil
}
func TestInjectorRunsEveryRoundBeforeRuntimeFactsAndCompaction(t *testing.T) {
	provider := &twoRoundInjectionProvider{}
	compactor := &injectionCompactor{t: t}
	config := runtimeConfig(t.TempDir(), provider)
	config.Compactor = compactor
	var calls int
	config.Injectors = []MessageInjector{namedInjector{"hint", func(_ context.Context, v TurnContext) ([]protocol.Message, error) {
		calls++
		if calls == 1 {
			for _, m := range v.Messages {
				if text, ok := m.Content.Plain(); ok && strings.HasPrefix(text, "<runtime-state>") {
					t.Fatal("runtime facts ran before custom injector")
				}
			}
		}
		return []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent("hint")}}, nil
	}}}
	session, err := NewRuntimeSession(config)
	if err != nil {
		t.Fatal(err)
	}
	session.todos.Update([]protocol.TodoItem{{Content: "open", Status: protocol.TodoPending, ActiveForm: "Working"}})
	if _, err = session.Run(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || compactor.calls != 2 || provider.calls != 2 {
		t.Fatal("injector skipped a round")
	}
}
