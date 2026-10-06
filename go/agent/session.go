// Package agent contains the first in-memory Go turn loop. Provider and tool
// execution are injected; no HTTP, persistence, or authority claim is implied.
package agent

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/luoyjx/mini-loop/go/decisions"
	"github.com/luoyjx/mini-loop/go/memory"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/skills"
	"github.com/luoyjx/mini-loop/go/userresources"
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
	memoryAuto                                        bool
	memory                                            *memory.ScopedStore
	ownerResources                                    *userresources.Resources
	decisionProvider                                  decisions.Provider
	decisionLLM                                       DecisionLLMConfig
	goals                                             goalState
	planMode                                          atomic.Bool
	planApprover                                      PlanApprover
	persistence                                       *sessionPersistence
	background                                        *backgroundState
	repairedToolUses                                  []string
	id                                                SessionID
	owner                                             OwnerID
	provider                                          Provider
	gate                                              *ToolGate
	mode                                              PermissionMode
	control                                           *sessionControl
	workspace                                         string
	executionWorkspace                                string
	maxRounds                                         int
	mu                                                sync.Mutex
	messages                                          []protocol.Message
	events                                            *sessionEvents
	todos                                             *TodoManager
	skills                                            SkillSource
	model                                             string
	maxTokens                                         int
	tokenThreshold                                    int
	systemBuilder                                     SystemBuilder
	explicitSystem                                    *string
	forkedFrom                                        *ForkLineage
	meter                                             TokenMeter
	runtimeFacts                                      string
	envelope                                          string
	compactor                                         Compactor
	files                                             *workspacepkg.Files
	compression                                       *compressionSignal
	label                                             string
	depth                                             int
	lineage                                           *SubagentLineage
	currentRun                                        RunContext
	questions                                         Questioner
	secrets                                           TextMasker
	subagents                                         SubagentProvider
	rolePolicy                                        RoleToolPolicy
	subagentMaxDepth, subagentMaxRounds               int
	cachePolicy                                       CachePolicy
	stuckDetector                                     StuckDetector
	stopHooks                                         []StopHook
	promptHooks                                       []UserPromptHook
	injectors                                         []MessageInjector
	modelLimiter, toolLimiter                         *ConcurrencyLimiter
	roundsWithoutTodo                                 int
	recentSteps                                       []ToolStep
	roundsWithoutTools                                int
	stuckNudges                                       int
	bash                                              BashExecutor
	lastModelSpan                                     SpanID
	lastStreamID                                      StreamID
	streamedText                                      string
	streamProgress                                    streamProgressPolicy
	recovery                                          Recovery
	recoveryModel                                     string
	activityID                                        ActivityID
	requestCatalog                                    ToolCatalogSnapshot
	loggedCatalogs, loggedSystems, loggedCapabilities map[string]bool
	turn                                              chan struct{}
	live                                              atomic.Pointer[liveRuntime]
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
	root := ""
	if bound, ok := executor.(interface{ Workspace() string }); ok {
		root = bound.Workspace()
	}
	session, err := NewSessionWithGate(id, owner, provider, gate, ModeInteractive, root, maxRounds)
	if session != nil {
		session.bash = executor
	}
	return session, err
}

func NewSessionWithGate(id SessionID, owner OwnerID, provider Provider, gate *ToolGate, mode PermissionMode, workspace string, maxRounds int) (*Session, error) {
	if id == "" || owner == "" || provider == nil || gate == nil || !mode.Valid() || maxRounds < 1 {
		return nil, errors.New("session requires id, owner, provider, tool gate, valid mode, and positive maxRounds")
	}
	session := &Session{id: id, owner: owner, provider: provider, gate: gate, mode: mode, workspace: workspace, maxRounds: maxRounds, events: &sessionEvents{}, model: DefaultModel, maxTokens: DefaultMaxTokens, tokenThreshold: DefaultTokenThreshold, systemBuilder: DefaultSystemBuilder{}, compactor: InMemoryCompactor{DefaultTokenThreshold, 50}}
	session.stopHooks = []StopHook{GoalContinuation{}}
	session.recovery, _ = NewDefaultRecovery(RecoveryConfig{})
	session.streamProgress = streamProgress(StreamProgressConfig{})
	session.cachePolicy, session.stuckDetector = NewDefaultCachePolicy(), NewDefaultStuckDetector()
	session.toolLimiter, _ = NewConcurrencyLimiter(DefaultToolConcurrency)
	session.turn = make(chan struct{}, 1)
	session.turn <- struct{}{}
	session.loggedCatalogs, session.loggedSystems, session.loggedCapabilities = make(map[string]bool), make(map[string]bool), make(map[string]bool)
	session.events.sessionID = id
	session.publishLive()
	session.bindEventHistory()
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
	session.executionWorkspace = session.workspace
	return session, nil
}

func (s *Session) TokenMeter() TokenMeterSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.meter.Snapshot()
}

func (s *Session) compact(ctx context.Context, envelope string, forced bool) error {
	// The wrapped provider owns annotation and model telemetry. Standalone
	// compactors can still use CompactionContext.CachePolicy explicitly.
	value := CompactionContext{Messages: append([]protocol.Message(nil), s.messages...), Files: s.files, Provider: sessionModelProvider{s}, Model: s.model, Meter: s.meter, Envelope: envelope, Secrets: s.secrets}
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
	s.publishLive()
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

// Run serializes turns within this session. Different sessions do not share a
// lock and can make model progress concurrently.
// UserResources returns the fixed owner bundle, not the resolver's current cache.
// The binding is immutable after construction; memory operations remain scoped.
func (s *Session) UserResources() (userresources.Resources, bool) {
	if s.ownerResources == nil {
		return userresources.Resources{}, false
	}
	return *s.ownerResources, true
}

func (s *Session) Run(ctx context.Context, prompt string) (string, error) {
	run, err := DefaultRunContext()
	if err != nil {
		return "", err
	}
	return s.RunWithContext(ctx, prompt, run)
}

func (s *Session) RunWithContext(ctx context.Context, prompt string, run RunContext) (string, error) {
	select {
	case <-s.turn:
	default:
		s.events.append(SessionEvent{kind: EventTurnQueued})
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-s.turn:
		}
	}
	defer func() { s.turn <- struct{}{} }()
	s.mu.Lock()
	defer s.mu.Unlock()
	defer s.publishLive()

	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := run.Validate(); err != nil {
		return "", err
	}
	s.recentSteps = nil
	s.roundsWithoutTools = 0
	s.stuckNudges = 0
	s.repairedToolUses = nil
	s.activityID = ""
	s.currentRun = run.clone()
	s.events.setScope(EventScope{s.label, s.depth, run.clone()})
	defer func() { s.currentRun = RunContext{} }()
	prompt, err := s.rewritePrompt(ctx, prompt)
	if err != nil {
		return "", err
	}
	prompt, err = s.prepareMemoryContext(ctx, prompt)
	if err != nil {
		return "", err
	}
	s.appendMessages(protocol.Message{Role: protocol.RoleUser, Content: protocol.PlainContent(prompt)})
	var lastText string
	resumptions := 0
	for round := 0; round < s.maxRounds; round++ {
		envelope := s.envelope
		if err := s.injectMessages(ctx); err != nil {
			return "", err
		}
		s.injectControls()
		if err := s.injectRuntimeFacts(ctx, envelope); err != nil {
			return "", err
		}
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
		s.envelope = envelope
		reply, err := s.completeModel(ctx, request, &s.requestCatalog)
		if err != nil {
			if ctx.Err() != nil {
				return "", stateRunError(ctx)
			}
			if errors.Is(err, ErrStateTranscript) || errors.Is(err, ErrSessionLeaseLost) {
				return "", err
			}
			detail := boundedError(err)
			text := "[Error] " + detail
			s.appendMessages(protocol.Message{Role: protocol.RoleAssistant, Content: protocol.BlockContent(protocol.NewTextBlock(text))})
			s.events.append(SessionEvent{kind: EventError, runError: RunErrorEvent{kind: ErrorProvider, detail: detail}})
			return text, nil
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
		s.appendMessages(protocol.Message{Role: protocol.RoleAssistant, Content: content})
		var roundText string
		hasTools := false
		for _, block := range blocks {
			if text, ok := block.Text(); ok {
				roundText += text.Text
			}
			if _, ok := block.ToolUse(); ok {
				hasTools = true
			}
		}
		if hasTools {
			s.appendText(roundText, PhaseCommentary)
			if title, ok := ActivityTitle(roundText); ok {
				id, err := newSpan("act_", 8)
				if err != nil {
					return "", err
				}
				s.activityID = ActivityID(id)
				s.events.append(SessionEvent{kind: EventActivityUpdate, activity: ActivityUpdateEvent{s.activityID, title}})
			}
		}

		results := make([]protocol.Block, 0)
		if s.compression != nil {
			s.compression.take()
		}
		results, err = s.dispatchBatch(ctx, run, blocks)
		if err != nil {
			return "", err
		}
		if roundText != "" {
			lastText = roundText
		}
		if len(results) == 0 {
			if reply.StopReason.Resumable() {
				resumptions++
				if resumptions <= maxResumptions {
					s.appendText(roundText, PhaseCommentary)
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
			s.roundsWithoutTools++
			s.publishLive()
			var continuation *string
			for _, hook := range s.stopHooks {
				continuation, err = hook.Stop(ctx, StopContext{session: s, Authority: ToolAuthority{SessionID: s.id, OwnerID: s.owner, Workspace: s.executionRoot(), Mode: s.permissionMode(), RunContext: run.clone()}, Messages: append([]protocol.Message(nil), s.messages...), LastText: lastText})
				if err != nil {
					return "", err
				}
				if ctx.Err() != nil {
					return "", ctx.Err()
				}
				if continuation != nil {
					break
				}
			}
			if continuation != nil {
				s.appendText(roundText, PhaseCommentary)
				signal, err := s.stuckDetector.Inspect(s.stuckState())
				if err != nil {
					return "", err
				}
				text := *continuation
				if signal != nil {
					nudge, headline := s.nudgeOrHalt(*signal)
					if !nudge {
						return stoppedText(headline, lastText), nil
					}
					text = signal.Reminder() + "\n\n" + text
				}
				s.appendMessages(protocol.Message{Role: protocol.RoleUser, Content: protocol.PlainContent(text)})
				continue
			}
			s.appendText(roundText, PhaseFinalAnswer)
			return lastText, nil
		}
		s.roundsWithoutTools = 0
		results = s.remindTodos(blocks, results)
		s.publishLive()
		signal, inspectErr := s.stuckDetector.Inspect(s.stuckState())
		if inspectErr != nil {
			s.appendMessages(protocol.Message{Role: protocol.RoleUser, Content: protocol.BlockContent(results...)})
			return "", inspectErr
		}
		if signal != nil {
			nudge, headline := s.nudgeOrHalt(*signal)
			if !nudge {
				s.appendMessages(protocol.Message{Role: protocol.RoleUser, Content: protocol.BlockContent(results...)})
				return stoppedText(headline, lastText), nil
			}
			results = append(results, protocol.NewTextBlock(signal.Reminder()))
		}
		s.appendMessages(protocol.Message{Role: protocol.RoleUser, Content: protocol.BlockContent(results...)})
		if s.compression != nil && s.compression.take() {
			if err := s.compact(ctx, envelope, true); err != nil {
				return "", err
			}
		}
	}
	s.events.append(SessionEvent{kind: EventError, runError: RunErrorEvent{kind: ErrorRoundExhaustion, rounds: s.maxRounds}})
	headline := fmt.Sprintf("[stopped after %d rounds without finishing]", s.maxRounds)
	return stoppedText(headline, lastText), nil
}

func stoppedText(headline, partial string) string {
	if partial != "" {
		return headline + "\nPartial output before the stop:\n" + partial
	}
	return headline
}
