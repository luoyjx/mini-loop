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

type Provider interface {
	Complete(context.Context, []protocol.Message) (protocol.ModelReply, error)
}

const (
	maxResumptions    = 8
	refusalNotice     = "[the model declined to answer this request and returned no content]"
	unknownToolResult = "[unknown] This tool was dispatched but the process terminated before its result was recorded. Whether it completed is not known. Do not retry it; check whether it already took effect first."
)

// BashExecutor is deliberately narrow. Future tools must have their own typed
// request and result path through the common execution gate.
type BashExecutor interface {
	ExecuteBash(context.Context, protocol.BashInput) (string, error)
}

type Session struct {
	id         SessionID
	owner      OwnerID
	provider   Provider
	gate       *ToolGate
	mode       PermissionMode
	workspace  string
	maxRounds  int
	mu         sync.Mutex
	messages   []protocol.Message
	stopEvents []ProviderStopEvent
}

func NewSession(id SessionID, owner OwnerID, provider Provider, executor BashExecutor, maxRounds int) (*Session, error) {
	catalog, err := NewBashToolCatalog(executor)
	if err != nil {
		return nil, err
	}
	gate, err := NewToolGate(catalog, DefaultPermissionPolicy(nil), GateHooks{})
	if err != nil {
		return nil, err
	}
	return NewSessionWithGate(id, owner, provider, gate, ModeInteractive, "", maxRounds)
}

func NewSessionWithGate(id SessionID, owner OwnerID, provider Provider, gate *ToolGate, mode PermissionMode, workspace string, maxRounds int) (*Session, error) {
	if id == "" || owner == "" || provider == nil || gate == nil || !mode.Valid() || maxRounds < 1 {
		return nil, errors.New("session requires id, owner, provider, tool gate, valid mode, and positive maxRounds")
	}
	return &Session{id: id, owner: owner, provider: provider, gate: gate, mode: mode, workspace: workspace, maxRounds: maxRounds}, nil
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

func (s *Session) StopEvents() []ProviderStopEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]ProviderStopEvent(nil), s.stopEvents...)
}

// An interrupted batch must still answer every tool_use before another model
// request. Completed results keep their real output; remaining effects are
// explicitly unknown so a model does not blindly retry a side effect.
func (s *Session) closeInterruptedBatch(blocks []protocol.Block, completed []protocol.Block, start int) {
	results := append([]protocol.Block(nil), completed...)
	for _, block := range blocks[start:] {
		if use, ok := block.ToolUse(); ok {
			results = append(results, protocol.NewToolResult(use.ID, unknownToolResult, false))
		}
	}
	if len(results) != 0 {
		s.messages = append(s.messages, protocol.Message{Role: protocol.RoleUser, Content: protocol.BlockContent(results...)})
	}
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
	resumptions := 0
	for round := 0; round < s.maxRounds; round++ {
		if err := protocol.ValidateTranscript(s.messages); err != nil {
			return "", fmt.Errorf("transcript before model call: %w", err)
		}
		reply, err := s.provider.Complete(ctx, append([]protocol.Message(nil), s.messages...))
		if err != nil {
			return "", err
		}
		if err := reply.Validate(); err != nil {
			return "", fmt.Errorf("model reply: %w", err)
		}
		var content protocol.Content
		if len(reply.Content) == 0 {
			// The request transcript forbids an empty block array. An empty
			// final response is represented by an empty string in this slice.
			content = protocol.PlainContent("")
		} else {
			content = protocol.BlockContent(reply.Content...)
		}
		blocks := append([]protocol.Block(nil), reply.Content...)
		s.messages = append(s.messages, protocol.Message{Role: protocol.RoleAssistant, Content: content})

		results := make([]protocol.Block, 0)
		var roundText string
		for i, block := range blocks {
			if text, ok := block.Text(); ok && text.Text != "" {
				roundText += text.Text
			}
			use, ok := block.ToolUse()
			if !ok {
				continue
			}
			outcome, dispatchErr := s.gate.Dispatch(ctx,
				ToolAuthority{SessionID: s.id, OwnerID: s.owner, Workspace: s.workspace, Mode: s.mode},
				ToolCall{ID: use.ID, Input: use.Input})
			if dispatchErr != nil {
				s.closeInterruptedBatch(blocks, results, i)
				return "", dispatchErr
			}
			results = append(results, protocol.NewToolResult(use.ID, outcome.Output, outcome.IsError()))
		}
		if roundText != "" {
			lastText = roundText
		}
		if len(results) == 0 {
			if reply.StopReason.Resumable() {
				resumptions++
				if resumptions <= maxResumptions {
					s.stopEvents = append(s.stopEvents, ProviderStopEvent{kind: EventTurnPaused, reason: reply.StopReason, resumption: resumptions})
					continue
				}
				s.stopEvents = append(s.stopEvents, ProviderStopEvent{kind: EventProviderStopUnhandled, reason: reply.StopReason, detail: fmt.Sprintf("still paused after %d resumptions", maxResumptions)})
			}
			if reply.StopReason == protocol.StopRefusal {
				s.stopEvents = append(s.stopEvents, ProviderStopEvent{kind: EventProviderRefusal, reason: reply.StopReason})
				if lastText == "" {
					lastText = refusalNotice
				}
			}
			if !reply.StopReason.Known() {
				s.stopEvents = append(s.stopEvents, ProviderStopEvent{kind: EventProviderStopUnhandled, reason: reply.StopReason, detail: "unrecognized stop reason, treated as end of turn"})
			}
			return lastText, nil
		}
		s.messages = append(s.messages, protocol.Message{Role: protocol.RoleUser, Content: protocol.BlockContent(results...)})
	}
	return lastText, fmt.Errorf("hit maxRounds (%d) without finishing", s.maxRounds)
}
