// Package agent contains the first in-memory Go turn loop. Provider and tool
// execution are injected; no HTTP, persistence, or authority claim is implied.
package agent

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/skills"
	workspacepkg "github.com/luoyjx/mini-loop/go/workspace"
)

type SessionID string
type OwnerID string

type Provider interface {
	Complete(context.Context, protocol.ModelRequest) (protocol.ModelReply, error)
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
	id                                  SessionID
	owner                               OwnerID
	provider                            Provider
	gate                                *ToolGate
	mode                                PermissionMode
	workspace                           string
	maxRounds                           int
	mu                                  sync.Mutex
	messages                            []protocol.Message
	events                              *sessionEvents
	todos                               *TodoManager
	skills                              SkillSource
	model                               string
	maxTokens                           int
	tokenThreshold                      int
	systemBuilder                       SystemBuilder
	meter                               TokenMeter
	runtimeFacts                        string
	envelope                            string
	compactor                           Compactor
	files                               *workspacepkg.Files
	compression                         *compressionSignal
	label                               string
	depth                               int
	lineage                             *SubagentLineage
	currentRun                          RunContext
	questions                           Questioner
	secrets                             TextMasker
	subagents                           SubagentProvider
	rolePolicy                          RoleToolPolicy
	subagentMaxDepth, subagentMaxRounds int
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
	session := &Session{id: id, owner: owner, provider: provider, gate: gate, mode: mode, workspace: workspace, maxRounds: maxRounds, events: &sessionEvents{}, model: DefaultModel, maxTokens: DefaultMaxTokens, tokenThreshold: DefaultTokenThreshold, systemBuilder: DefaultSystemBuilder{}, compactor: InMemoryCompactor{DefaultTokenThreshold, 50}}
	session.label, session.skills = string(id), skills.EmptyCatalog()
	session.subagents, session.rolePolicy = &InProcessSubagents{}, DefaultRoleToolPolicy()
	session.subagentMaxDepth, session.subagentMaxRounds = DefaultSubagentMaxDepth, DefaultSubagentMaxRounds
	session.events.setScope(EventScope{Label: session.label})
	if workspace != "" {
		files, err := workspacepkg.NewFiles(workspace)
		if err != nil {
			return nil, err
		}
		session.workspace, session.files, session.compactor = files.Root(), files, NewDefaultCompactor()
	}
	return session, nil
}

func (s *Session) TokenMeter() TokenMeterSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.meter.Snapshot()
}

func (s *Session) compact(ctx context.Context, envelope string, forced bool) error {
	value := CompactionContext{Messages: append([]protocol.Message(nil), s.messages...), Files: s.files, Provider: s.provider, Model: s.model, Meter: s.meter, Envelope: envelope, Secrets: s.secrets}
	var result CompactionResult
	var err error
	if forced {
		result, err = s.compactor.Compact(ctx, value)
	} else {
		result, err = s.compactor.MaybeCompact(ctx, value)
	}
	// A Go extension may return its zero result on failure. That means no
	// rewrite; preserve both the existing history and the original error.
	if err != nil && result.Messages == nil {
		return err
	}
	if validationErr := protocol.ValidateTranscript(result.Messages); validationErr != nil {
		return fmt.Errorf("compactor transcript: %w", validationErr)
	}
	if len(result.Messages) == 0 {
		return errors.New("compactor returned an empty transcript")
	}
	s.messages = append([]protocol.Message(nil), result.Messages...)
	for _, event := range result.Events {
		s.events.append(SessionEvent{kind: EventCompact, compact: event})
	}
	return err
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
	stops := make([]ProviderStopEvent, 0)
	for _, record := range s.events.snapshot() {
		if stop, ok := record.Event.Stop(); ok {
			stops = append(stops, stop)
		}
	}
	return stops
}

func (s *Session) Events() []SessionEventRecord { return s.events.snapshot() }

func (s *Session) Todos() []protocol.TodoItem {
	if s.todos == nil {
		return []protocol.TodoItem{}
	}
	return s.todos.Snapshot()
}

func (s *Session) recordStop(event ProviderStopEvent) {
	s.events.append(SessionEvent{kind: SessionEventKind(event.Kind()), stop: event})
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
	run, err := DefaultRunContext()
	if err != nil {
		return "", err
	}
	return s.RunWithContext(ctx, prompt, run)
}

func (s *Session) RunWithContext(ctx context.Context, prompt string, run RunContext) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := run.Validate(); err != nil {
		return "", err
	}
	s.currentRun = run.clone()
	s.events.setScope(EventScope{s.label, s.depth, run.clone()})
	defer func() { s.currentRun = RunContext{} }()
	s.messages = append(s.messages, protocol.Message{Role: protocol.RoleUser, Content: protocol.PlainContent(prompt)})
	var lastText string
	resumptions := 0
	for round := 0; round < s.maxRounds; round++ {
		envelope := s.envelope
		s.injectRuntimeFacts(envelope)
		if err := s.compact(ctx, envelope, false); err != nil {
			return "", err
		}
		if err := protocol.ValidateTranscript(s.messages); err != nil {
			return "", fmt.Errorf("transcript before model call: %w", err)
		}
		request, envelope, err := s.buildRequest()
		if err != nil {
			return "", err
		}
		if err := request.Validate(); err != nil {
			return "", fmt.Errorf("model request: %w", err)
		}
		s.envelope = envelope
		reply, err := s.provider.Complete(ctx, request)
		if err != nil {
			return "", err
		}
		if err := reply.Validate(); err != nil {
			return "", fmt.Errorf("model reply: %w", err)
		}
		s.meter.Observe(reply.Usage, s.messages, envelope)
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
		if s.compression != nil {
			s.compression.take()
		}
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
				ToolAuthority{SessionID: s.id, OwnerID: s.owner, Workspace: s.workspace, Mode: s.mode, RunContext: run.clone()},
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
					s.recordStop(ProviderStopEvent{kind: EventTurnPaused, reason: reply.StopReason, resumption: resumptions})
					continue
				}
				s.recordStop(ProviderStopEvent{kind: EventProviderStopUnhandled, reason: reply.StopReason, detail: fmt.Sprintf("still paused after %d resumptions", maxResumptions)})
			}
			if reply.StopReason == protocol.StopRefusal {
				s.recordStop(ProviderStopEvent{kind: EventProviderRefusal, reason: reply.StopReason})
				if lastText == "" {
					lastText = refusalNotice
				}
			}
			if !reply.StopReason.Known() {
				s.recordStop(ProviderStopEvent{kind: EventProviderStopUnhandled, reason: reply.StopReason, detail: "unrecognized stop reason, treated as end of turn"})
			}
			return lastText, nil
		}
		s.messages = append(s.messages, protocol.Message{Role: protocol.RoleUser, Content: protocol.BlockContent(results...)})
		if s.compression != nil && s.compression.take() {
			if err := s.compact(ctx, envelope, true); err != nil {
				return "", err
			}
		}
	}
	s.events.append(SessionEvent{kind: EventError, runError: RunErrorEvent{kind: ErrorRoundExhaustion, rounds: s.maxRounds}})
	headline := fmt.Sprintf("[stopped after %d rounds without finishing]", s.maxRounds)
	if lastText != "" {
		headline += "\nPartial output before the stop:\n" + lastText
	}
	return headline, nil
}
