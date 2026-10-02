package agent

import (
	"context"
	"fmt"

	"github.com/luoyjx/mini-loop/go/protocol"
)

// TurnContext is a detached view for loop extensions. Extensions must be safe
// for concurrent calls when shared with other sessions or child agents.
type TurnContext struct {
	Authority ToolAuthority
	Label     string
	Depth     int
	Messages  []protocol.Message
	Todos     []protocol.TodoItem
}

type UserPromptHook interface {
	// A nil replacement keeps the prompt; an empty replacement is meaningful.
	RewriteUserPrompt(context.Context, TurnContext, string) (*string, error)
}

type MessageInjector interface {
	Name() string
	Inject(context.Context, TurnContext) ([]protocol.Message, error)
}

func (s *Session) turnContext() TurnContext {
	view := TurnContext{Authority: ToolAuthority{SessionID: s.id, OwnerID: s.owner, Workspace: s.workspace, Mode: s.mode, RunContext: s.currentRun.clone()}, Label: s.label, Depth: s.depth, Messages: append([]protocol.Message(nil), s.messages...)}
	if s.todos != nil {
		view.Todos = s.todos.Snapshot()
	}
	return view
}

func (s *Session) rewritePrompt(ctx context.Context, prompt string) (string, error) {
	for _, hook := range s.promptHooks {
		replacement, err := hook.RewriteUserPrompt(ctx, s.turnContext(), prompt)
		if err != nil {
			return "", err
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if replacement != nil {
			prompt = *replacement
		}
	}
	return prompt, nil
}

func (s *Session) injectMessages(ctx context.Context) error {
	for _, injector := range s.injectors {
		messages, err := injector.Inject(ctx, s.turnContext())
		if err != nil {
			return fmt.Errorf("injector %q: %w", injector.Name(), err)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		// Validate the complete batch before changing history.
		for _, message := range messages {
			if err := message.Validate(); err != nil {
				return fmt.Errorf("injector %q: %w", injector.Name(), err)
			}
		}
		s.appendMessages(messages...)
	}
	return nil
}

const TodoReminder = "<reminder>Update your todos.</reminder>"

func (s *Session) remindTodos(blocks, results []protocol.Block) []protocol.Block {
	usedTodo := false
	for _, block := range blocks {
		if use, ok := block.ToolUse(); ok && use.Input.Name() == protocol.ToolTodoWrite {
			usedTodo = true
		}
	}
	if usedTodo {
		s.roundsWithoutTodo = 0
	} else {
		s.roundsWithoutTodo++
	}
	if s.todos != nil && s.todos.HasOpenItems() && s.roundsWithoutTodo >= 3 {
		s.roundsWithoutTodo = 0
		results = append(results, protocol.NewTextBlock(TodoReminder))
	}
	return results
}
