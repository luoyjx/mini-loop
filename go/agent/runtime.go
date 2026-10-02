package agent

import (
	"context"
	"errors"
	"sync"

	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/shell"
	"github.com/luoyjx/mini-loop/go/skills"
	"github.com/luoyjx/mini-loop/go/workspace"
)

type SkillSource interface {
	Descriptions() string
	Load(context.Context, protocol.LoadSkillInput) (string, error)
}

type QuestionRequest struct {
	Authority ToolAuthority
	Question  string
}

type QuestionAnswerKind string

const (
	QuestionAnswered   QuestionAnswerKind = "answered"
	QuestionUnanswered QuestionAnswerKind = "unanswered"
)

// Text is present for an answered question, including an empty answer. A
// decline/timeout is its own variant and cannot become an approval boolean.
type QuestionAnswer struct {
	kind QuestionAnswerKind
	text string
}

func AnswerQuestion(text string) QuestionAnswer        { return QuestionAnswer{QuestionAnswered, text} }
func NoQuestionAnswer() QuestionAnswer                 { return QuestionAnswer{kind: QuestionUnanswered} }
func (answer QuestionAnswer) Kind() QuestionAnswerKind { return answer.kind }
func (answer QuestionAnswer) Text() (string, bool) {
	return answer.text, answer.kind == QuestionAnswered
}

type Questioner interface {
	AskQuestion(context.Context, QuestionRequest) (QuestionAnswer, error)
}

// RuntimeConfig describes explicit dependencies. A nil Skills source is an
// empty catalogue; a nil Questions surface reports the Python bare-Agent
// unavailability notice. This callback is not a durable approval broker.
type RuntimeConfig struct {
	ID                SessionID
	Owner             OwnerID
	Provider          Provider
	Bash              BashExecutor
	Workspace         string
	Mode              PermissionMode
	MaxRounds         int
	Skills            SkillSource
	Questions         Questioner
	Approver          Approver
	Hooks             GateHooks
	Model             string
	MaxTokens         int
	TokenThreshold    int
	SystemBuilder     SystemBuilder
	Compactor         Compactor
	Label             string
	Depth             int
	SubagentMaxDepth  int
	SubagentMaxRounds int
	Subagents         SubagentProvider
	RoleToolPolicy    RoleToolPolicy
	ActionJournal     ActionJournal
	Approvals         *ApprovalBroker
	Secrets           ApprovalRedactor
}

type runtimeHandler struct {
	mu          sync.Mutex
	binding     ToolAuthority
	todos       *TodoManager
	events      *sessionEvents
	skills      SkillSource
	questions   Questioner
	compression *compressionSignal
	session     *Session
}

type compressionSignal struct {
	mu      sync.Mutex
	pending bool
}

func (signal *compressionSignal) request() {
	signal.mu.Lock()
	defer signal.mu.Unlock()
	signal.pending = true
}
func (signal *compressionSignal) take() bool {
	signal.mu.Lock()
	defer signal.mu.Unlock()
	pending := signal.pending
	signal.pending = false
	return pending
}

func (handler *runtimeHandler) ExecuteTool(ctx context.Context, authority ToolAuthority, input protocol.ToolInput) (string, error) {
	if authority.SessionID != handler.binding.SessionID || authority.OwnerID != handler.binding.OwnerID {
		return "", errors.New("runtime handler identity does not match bound session")
	}
	root, err := workspace.ResolvePath(authority.Workspace)
	if err != nil || root != handler.binding.Workspace {
		return "", errors.New("runtime handler workspace does not match bound session")
	}
	handler.mu.Lock()
	defer handler.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	switch input.Name() {
	case protocol.ToolTask:
		value, _ := input.Task()
		role := RoleExplore
		if value.AgentType != nil {
			role = AgentRole(*value.AgentType)
		}
		if handler.session == nil {
			return "", errors.New("task handler has no bound session")
		}
		run := authority.RunContext
		if run.MessageID() == "" {
			var err error
			run, err = DefaultRunContext()
			if err != nil {
				return "", err
			}
		}
		return handler.session.runSubagent(ctx, value.Prompt, role, run)
	case protocol.ToolCompress:
		handler.compression.request()
		return "Compressing conversation...", nil
	case protocol.ToolTodoWrite:
		value, _ := input.TodoWrite()
		output, err := handler.todos.Update(value.Items)
		if err == nil {
			handler.events.append(SessionEvent{kind: EventTodo, todos: handler.todos.Snapshot()})
		}
		return output, err
	case protocol.ToolLoadSkill:
		value, _ := input.LoadSkill()
		return handler.skills.Load(ctx, value)
	case protocol.ToolAskUser:
		if handler.questions == nil {
			return "[ask_user unavailable on this surface: no approval broker]", nil
		}
		value, _ := input.AskUser()
		answer, err := handler.questions.AskQuestion(ctx, QuestionRequest{authority, value.Question})
		if err != nil {
			return "", err
		}
		switch answer.kind {
		case QuestionAnswered:
			return "The user answered: " + answer.text, nil
		case QuestionUnanswered:
			return "[no answer] The user declined or did not respond in time. Proceed on your best judgment and say what you assumed.", nil
		default:
			return "", errors.New("question surface returned an invalid answer variant")
		}
	default:
		return "", errors.New("unsupported runtime handler")
	}
}

// NewRuntimeSession adds per-session todos, on-demand skills and textual human
// questions plus deferred compaction to the implemented workspace tools.
// Task delegates to a fresh child through the explicit subagent seam.
func NewRuntimeSession(config RuntimeConfig) (*Session, error) {
	if config.Approvals != nil && (config.Approver != nil || config.Questions != nil) {
		return nil, errors.New("approval broker conflicts with an explicit approver/question surface")
	}
	if config.ID == "" || config.Owner == "" || config.Provider == nil || config.Bash == nil || !config.Mode.Valid() || config.MaxRounds < 1 {
		return nil, errors.New("runtime session requires valid identity, provider, executor, mode and round limit")
	}
	if config.MaxTokens < 0 || config.TokenThreshold < 0 {
		return nil, errors.New("runtime model budgets cannot be negative")
	}
	if config.Depth < 0 || config.SubagentMaxDepth < 0 || config.SubagentMaxRounds < 0 {
		return nil, errors.New("subagent depth and round budgets cannot be negative")
	}
	files, err := workspace.NewFiles(config.Workspace)
	if err != nil {
		return nil, err
	}
	// Bind credential scope to an independent real executor before any command
	// can project/truncate output. Shared caller executors remain unchanged.
	if executor, ok := config.Bash.(*shell.Executor); ok && config.Secrets != nil {
		if source, ok := config.Secrets.(shell.SecretSource); ok {
			config.Bash, err = executor.WithSecrets(source)
		} else {
			config.Bash, err = executor.WithMasker(config.Secrets)
		}
		if err != nil {
			return nil, err
		}
	}
	base, err := NewWorkspaceToolCatalog(config.Bash, files)
	if err != nil {
		return nil, err
	}
	source := config.Skills
	if source == nil {
		source = skills.EmptyCatalog()
	}
	handler := &runtimeHandler{
		binding: ToolAuthority{SessionID: config.ID, OwnerID: config.Owner, Workspace: files.Root(), Mode: config.Mode},
		todos:   &TodoManager{}, events: &sessionEvents{}, skills: source, questions: config.Questions, compression: &compressionSignal{},
	}
	approver, questions := config.Approver, config.Questions
	if config.Approvals != nil {
		surface, err := config.Approvals.ForSession(handler.binding, sessionApprovalSink{handler.events})
		if err != nil {
			return nil, err
		}
		if config.Secrets != nil {
			surface.redactor = config.Secrets
		}
		approver, questions = surface, surface
		handler.questions = surface
	}
	definitions := append([]ToolDefinition(nil), base.ordered...)
	for _, name := range []protocol.ToolName{protocol.ToolTodoWrite, protocol.ToolTask, protocol.ToolLoadSkill, protocol.ToolCompress, protocol.ToolAskUser} {
		traits := ToolTraits{Risk: RiskRead, Readonly: true}
		if name == protocol.ToolTodoWrite || name == protocol.ToolCompress {
			traits = ToolTraits{Risk: RiskWrite}
		}
		if name == protocol.ToolTask {
			traits = ToolTraits{Risk: RiskExec}
		}
		definition, err := NewToolDefinition(name, traits, handler)
		if err != nil {
			return nil, err
		}
		definitions = append(definitions, definition)
	}
	catalog, err := NewToolCatalog(definitions...)
	if err != nil {
		return nil, err
	}
	gate, err := NewJournaledToolGate(catalog, DefaultPermissionPolicy(approver), config.Hooks, config.ActionJournal)
	if err != nil {
		return nil, err
	}
	gate.secrets = config.Secrets
	session, err := NewSessionWithGate(config.ID, config.Owner, config.Provider, gate, config.Mode, files.Root(), config.MaxRounds)
	if err != nil {
		return nil, err
	}
	session.todos, session.events = handler.todos, handler.events
	session.secrets, session.events.secrets = config.Secrets, config.Secrets
	handler.session = session
	session.questions = questions
	if config.Label != "" {
		session.label = config.Label
	}
	session.depth = config.Depth
	if config.SubagentMaxDepth != 0 {
		session.subagentMaxDepth = config.SubagentMaxDepth
	}
	if config.SubagentMaxRounds != 0 {
		session.subagentMaxRounds = config.SubagentMaxRounds
	}
	if config.Subagents != nil {
		session.subagents = config.Subagents
	}
	if config.RoleToolPolicy != nil {
		session.rolePolicy = config.RoleToolPolicy
	}
	session.events.setScope(EventScope{Label: session.label, Depth: session.depth})
	session.skills, session.compression = source, handler.compression
	if config.Model != "" {
		session.model = config.Model
	}
	if config.MaxTokens != 0 {
		session.maxTokens = config.MaxTokens
	}
	if config.TokenThreshold != 0 {
		session.tokenThreshold = config.TokenThreshold
	}
	if config.SystemBuilder != nil {
		session.systemBuilder = config.SystemBuilder
	}
	if config.Compactor != nil {
		session.compactor = config.Compactor
	} else {
		compactor := NewDefaultCompactor()
		compactor.TokenThreshold = session.tokenThreshold
		session.compactor = compactor
	}
	return session, nil
}
