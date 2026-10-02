// Package agent contains the first in-memory Go turn loop. Provider and tool
// execution are injected; no HTTP, persistence, or authority claim is implied.
package agent

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/luoyjx/mini-loop/go/protocol"
)

type SessionID string
type OwnerID string

type StopReason string

const (
	StopEndTurn StopReason = "end_turn"
	StopToolUse StopReason = "tool_use"
)

type ModelReply struct {
	Content    []protocol.Block
	StopReason StopReason
}

type Provider interface {
	Complete(context.Context, []protocol.Message) (ModelReply, error)
}

// BashExecutor is deliberately narrow. Future tools must have their own typed
// request and result path through the common execution gate.
type BashExecutor interface {
	ExecuteBash(context.Context, protocol.BashInput) (string, error)
}

type Session struct {
	id        SessionID
	owner     OwnerID
	provider  Provider
	executor  BashExecutor
	maxRounds int
	mu        sync.Mutex
	messages  []protocol.Message
}

func NewSession(id SessionID, owner OwnerID, provider Provider, executor BashExecutor, maxRounds int) (*Session, error) {
	if id == "" || owner == "" || provider == nil || executor == nil || maxRounds < 1 {
		return nil, errors.New("session requires id, owner, provider, executor, and positive maxRounds")
	}
	return &Session{id: id, owner: owner, provider: provider, executor: executor, maxRounds: maxRounds}, nil
}

func (s *Session) ID() SessionID { return s.id }

func (s *Session) Owner() OwnerID { return s.owner }

// Messages returns a snapshot. Message content is immutable outside the
// protocol package, so callers cannot mutate a session through this copy.
func (s *Session) Messages() []protocol.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]protocol.Message(nil), s.messages...)
}

// Run serializes turns within this session. Different sessions do not share a
// lock and can make model progress concurrently.
func (s *Session) Run(ctx context.Context, prompt string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return "", err
	}
	s.messages = append(s.messages, protocol.Message{Role: protocol.RoleUser, Content: protocol.PlainContent(prompt)})
	var lastText string
	for round := 0; round < s.maxRounds; round++ {
		if err := protocol.ValidateTranscript(s.messages); err != nil {
			return "", fmt.Errorf("transcript before model call: %w", err)
		}
		reply, err := s.provider.Complete(ctx, append([]protocol.Message(nil), s.messages...))
		if err != nil {
			return "", err
		}
		if reply.StopReason != StopEndTurn && reply.StopReason != StopToolUse {
			return "", fmt.Errorf("unsupported stop reason %q", reply.StopReason)
		}
		content := protocol.BlockContent(reply.Content...)
		if err := content.Validate(); err != nil {
			return "", fmt.Errorf("model content: %w", err)
		}
		blocks, _ := content.Blocks()
		for _, block := range blocks {
			if use, ok := block.ToolUse(); ok && use.Name != protocol.ToolBash {
				return "", fmt.Errorf("tool %q has no executor in the initial Go loop", use.Name)
			}
		}
		s.messages = append(s.messages, protocol.Message{Role: protocol.RoleAssistant, Content: content})

		results := make([]protocol.Block, 0)
		var roundText string
		for _, block := range blocks {
			if text, ok := block.Text(); ok && text.Text != "" {
				roundText += text.Text
			}
			use, ok := block.ToolUse()
			if !ok {
				continue
			}
			input, _ := use.Input.Bash()
			output, executeErr := s.executor.ExecuteBash(ctx, input)
			if executeErr != nil {
				// The result remains paired even when execution fails. Richer
				// unknown-effect and approval states belong to G2.
				output = executeErr.Error()
			}
			results = append(results, protocol.NewToolResult(use.ID, output, executeErr != nil))
		}
		if roundText != "" {
			lastText = roundText
		}
		if len(results) == 0 {
			return lastText, nil
		}
		s.messages = append(s.messages, protocol.Message{Role: protocol.RoleUser, Content: protocol.BlockContent(results...)})
	}
	return lastText, fmt.Errorf("hit maxRounds (%d) without finishing", s.maxRounds)
}
