package agent

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/luoyjx/mini-loop/go/decisions"
	"github.com/luoyjx/mini-loop/go/memory"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/shell"
	"github.com/luoyjx/mini-loop/go/skills"
	"github.com/luoyjx/mini-loop/go/spill"
	"github.com/luoyjx/mini-loop/go/tasks"
	"github.com/luoyjx/mini-loop/go/userresources"
	"github.com/luoyjx/mini-loop/go/workspace"
	"github.com/luoyjx/mini-loop/go/worktrees"
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
	// ToolSelection narrows the installed catalogue before the gate is built.
	ToolSelection ToolSelection
	// Manager-owned process-local drafts; standalone sessions leave this nil.
	skillDrafts          *userresources.DraftStore
	MemoryTools          bool
	MemoryAuto           *bool
	Memory               *memory.ScopedStore
	UserResources        *userresources.Resources
	DecisionTools        bool
	DecisionProvider     decisions.Provider
	DecisionLLM          DecisionLLMConfig
	GoalTools            bool
	PlanModeTools        bool
	PlanApprover         PlanApprover
	StateStore           StateStore
	StateLeaseOwner      LeaseOwner
	StateLeaseTTL        time.Duration
	CronTools            bool
	Cron                 CronControl
	BackgroundTools      bool
	WorktreeTools        bool
	Worktrees            *worktrees.Manager
	WorkspaceBashFactory BashFactory
	TaskTools            bool
	Trajectories         TrajectoryWriter
	Build                string
	Spill                spill.Store
	ID                   SessionID
	Owner                OwnerID
	Provider             Provider
	Recovery             Recovery
	StreamProgress       StreamProgressConfig
	Bash                 BashExecutor
	Workspace            string
	Mode                 PermissionMode
	MaxRounds            int
	Skills               SkillSource
	Questions            Questioner
	Approver             Approver
	Hooks                GateHooks
	Model                string
	MaxTokens            int
	TokenThreshold       int
	SystemBuilder        SystemBuilder
	Compactor            Compactor
	Label                string
	Depth                int
	SubagentMaxDepth     int
	SubagentMaxRounds    int
	Subagents            SubagentProvider
	RoleToolPolicy       RoleToolPolicy
	ActionJournal        ActionJournal
	Approvals            *ApprovalBroker
	Secrets              ApprovalRedactor
	CachePolicy          CachePolicy
	StuckDetector        StuckDetector
	StopHooks            []StopHook
	EventSink            EventSink

	UserPromptHooks []UserPromptHook
	Injectors       []MessageInjector
	ModelLimiter    *ConcurrencyLimiter
	ToolLimiter     *ConcurrencyLimiter
}

type runtimeHandler struct {
	memory               *memory.ScopedStore
	planApprover         PlanApprover
	cron                 CronControl
	background           *backgroundState
	worktrees            *worktrees.Manager
	workspaceBashFactory BashFactory
	taskStore            *tasks.Store
	mu                   sync.Mutex
	binding              ToolAuthority
	todos                *TodoManager
	events               *sessionEvents
	skills               SkillSource
	questions            Questioner
	compression          *compressionSignal
	session              *Session
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
	if input.Name() == protocol.ToolBash {
		out, _, err := handler.executeDetailedTool(ctx, authority, input)
		return out, err
	}

	handler.mu.Lock()
	defer handler.mu.Unlock()
	if authority.SessionID != handler.binding.SessionID || authority.OwnerID != handler.binding.OwnerID {
		return "", errors.New("runtime handler identity does not match bound session")
	}
	root, err := workspace.ResolvePath(authority.Workspace)
	if err != nil || root != handler.binding.Workspace {
		return "", errors.New("runtime handler workspace does not match bound session")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	switch input.Name() {
	case protocol.ToolRemember, protocol.ToolRecall:
		return handler.executeMemory(ctx, input)
	case protocol.ToolDecision:
		return handler.executeDecision(ctx, authority, input)
	case protocol.ToolGoalCreate, protocol.ToolGoalStatus, protocol.ToolGoalComplete, protocol.ToolGoalBlock, protocol.ToolGoalResume:
		return handler.executeGoal(ctx, authority, input)
	case protocol.ToolEnterPlanMode, protocol.ToolExitPlanMode:
		return handler.executePlanMode(ctx, authority, input)
	case protocol.ToolScheduleCron, protocol.ToolListCrons, protocol.ToolCancelCron:
		return handler.executeCron(input)
	case protocol.ToolBackgroundRun, protocol.ToolCheckBackground:
		return handler.executeBackground(ctx, input)
	case protocol.ToolCreateWorktree, protocol.ToolRemoveWorktree, protocol.ToolKeepWorktree, protocol.ToolListWorktrees, protocol.ToolEnterWorktree:
		return handler.executeWorktree(ctx, input)
	case protocol.ToolCreateTask, protocol.ToolListTasks, protocol.ToolGetTask, protocol.ToolClaimTask, protocol.ToolCompleteTask:
		if handler.taskStore == nil {
			var err error
			handler.taskStore, err = tasks.New(tasks.Config{Workspace: handler.binding.Workspace, Secrets: handler.session.secrets})
			if err != nil {
				return "", err
			}
		}
		handler.session.taskDiagnostics.Store(handler.taskStore)
		return handler.executeTaskBoard(input)
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
		output, err := handler.skills.Load(ctx, value)
		var refusal *skills.RefusalError
		if errors.As(err, &refusal) {
			return "Error: " + refusal.Error(), nil
		}
		return output, err
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
	if config.StateStore != nil || config.StateLeaseOwner != "" || config.StateLeaseTTL != 0 {
		return nil, errors.New("state persistence requires NewManagedSession")
	}
	decisionLLM, err := config.DecisionLLM.normalized()
	if err != nil {
		return nil, err
	}
	for _, hook := range config.UserPromptHooks {
		if hook == nil {
			return nil, errors.New("user prompt hook cannot be nil")
		}
	}
	for _, injector := range config.Injectors {
		if injector == nil {
			return nil, errors.New("message injector cannot be nil")
		}
	}
	for _, limiter := range []*ConcurrencyLimiter{config.ModelLimiter, config.ToolLimiter} {
		if limiter != nil && limiter.slots == nil {
			return nil, errors.New("uninitialized concurrency limiter")
		}
	}
	if config.Approvals != nil && (config.Approver != nil || config.Questions != nil) {
		return nil, errors.New("approval broker conflicts with an explicit approver/question surface")
	}
	if config.ID == "" || config.Owner == "" || config.Provider == nil || config.Bash == nil || !config.Mode.Valid() || config.MaxRounds < 1 {
		return nil, errors.New("runtime session requires valid identity, provider, executor, mode and round limit")
	}
	var resources *userresources.Resources
	if config.UserResources != nil {
		bound := *config.UserResources
		if bound.Owner() != userresources.OwnerID(config.Owner) || bound.Skills() == nil || bound.Memory() == nil {
			return nil, errors.New("user resource snapshot must be complete and bound to the session owner")
		}
		resources = &bound
		config.Skills = bound.Skills()
		config.Memory = bound.Memory()
	}
	if config.Memory != nil && config.Memory.Owner() != memory.OwnerID(config.Owner) {
		return nil, errors.New("memory store must be bound to the session owner")
	}
	if config.MemoryTools && config.Memory == nil {
		return nil, errors.New("memory tools require a bound memory store")
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
	if executor, ok := config.Bash.(*shell.Executor); ok && config.Spill != nil {
		config.Bash, err = executor.WithSpill(config.Spill)
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
	handler := &runtimeHandler{planApprover: config.PlanApprover, cron: config.Cron,
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
	handler.memory = config.Memory
	if config.MemoryTools {
		for _, schema := range protocol.MemorySchemas() {
			traits := ToolTraits{Risk: RiskWrite}
			if schema.Name == protocol.ToolRecall {
				traits = ToolTraits{Risk: RiskRead, Readonly: true}
			}
			definition, err := NewToolDefinitionWithSchema(schema, traits, handler)
			if err != nil {
				return nil, err
			}
			definitions = append(definitions, definition)
		}
	}
	if config.DecisionTools {
		definition, err := NewToolDefinitionWithSchema(protocol.DecisionSchema(), ToolTraits{Risk: RiskExternal}, handler)
		if err != nil {
			return nil, err
		}
		definitions = append(definitions, definition)
	}
	handler.worktrees, handler.workspaceBashFactory = config.Worktrees, config.WorkspaceBashFactory
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
	if config.BackgroundTools {
		native, ok := config.Bash.(*shell.Executor)
		if !ok || native == nil || native.Workspace() != files.Root() {
			return nil, errors.New("background tools require a native shell bound to the session workspace")
		}
		handler.background = &backgroundState{executor: native}
		for i := range definitions {
			if definitions[i].name == protocol.ToolBash {
				definitions[i].handler = handler
				definitions[i].classifier = backgroundBashClassifier{}
			}
		}
	}
	if config.GoalTools {
		for _, schema := range protocol.GoalSchemas() {
			traits := ToolTraits{Risk: RiskWrite}
			if schema.Name == protocol.ToolGoalStatus {
				traits = ToolTraits{Risk: RiskRead, Readonly: true}
			}
			definition, err := NewToolDefinitionWithSchema(schema, traits, handler)
			if err != nil {
				return nil, err
			}
			definitions = append(definitions, definition)
		}
	}
	if config.PlanModeTools {
		for _, schema := range protocol.PlanModeSchemas() {
			definition, err := NewToolDefinitionWithSchema(schema, ToolTraits{Risk: RiskRead, Readonly: true}, handler)
			if err != nil {
				return nil, err
			}
			definitions = append(definitions, definition)
		}
	}
	if config.CronTools {
		for _, schema := range protocol.CronSchemas() {
			traits := ToolTraits{Risk: RiskWrite}
			if schema.Name == protocol.ToolListCrons {
				traits = ToolTraits{Risk: RiskRead, Readonly: true}
			}
			definition, err := NewToolDefinitionWithSchema(schema, traits, handler)
			if err != nil {
				return nil, err
			}
			definitions = append(definitions, definition)
		}
	}
	if config.TaskTools {
		for _, schema := range protocol.TaskBoardSchemas() {
			traits := ToolTraits{Risk: RiskWrite}
			if schema.Name == protocol.ToolListTasks || schema.Name == protocol.ToolGetTask {
				traits = ToolTraits{Risk: RiskRead, Readonly: true}
			}
			definition, err := NewToolDefinitionWithSchema(schema, traits, handler)
			if err != nil {
				return nil, err
			}
			definitions = append(definitions, definition)
		}
	}
	if config.BackgroundTools {
		for _, schema := range protocol.BackgroundSchemas() {
			traits := ToolTraits{Risk: RiskExec}
			if schema.Name == protocol.ToolCheckBackground {
				traits = ToolTraits{Risk: RiskRead, Readonly: true}
			}
			definition, err := NewToolDefinitionWithSchema(schema, traits, handler)
			if err != nil {
				return nil, err
			}
			definitions = append(definitions, definition)
		}
	}
	if config.WorktreeTools {
		for _, schema := range protocol.WorktreeSchemas() {
			traits := ToolTraits{Risk: RiskWrite}
			if schema.Name == protocol.ToolCreateWorktree || schema.Name == protocol.ToolRemoveWorktree {
				traits.Risk = RiskExec
			}
			if schema.Name == protocol.ToolListWorktrees {
				traits = ToolTraits{Risk: RiskRead, Readonly: true}
			}
			definition, err := NewToolDefinitionWithSchema(schema, traits, handler)
			if err != nil {
				return nil, err
			}
			definitions = append(definitions, definition)
		}
	}
	catalog, err := NewToolCatalog(config.ToolSelection.filter(definitions)...)
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
	if config.Recovery != nil {
		session.recovery = config.Recovery
	}
	session.planApprover = config.PlanApprover
	session.skillDrafts = config.skillDrafts
	session.ownerResources = resources
	session.memory = config.Memory
	session.memoryAuto = config.MemoryAuto == nil || *config.MemoryAuto
	session.decisionProvider, session.decisionLLM = config.DecisionProvider, decisionLLM
	session.streamProgress = streamProgress(config.StreamProgress)
	if config.CachePolicy != nil {
		session.cachePolicy = config.CachePolicy
	}
	if config.StuckDetector != nil {
		session.stuckDetector = config.StuckDetector
	}
	if config.StopHooks != nil {
		session.stopHooks = append([]StopHook(nil), config.StopHooks...)
	}
	session.promptHooks = append([]UserPromptHook(nil), config.UserPromptHooks...)
	session.injectors = append([]MessageInjector(nil), config.Injectors...)
	session.modelLimiter = config.ModelLimiter
	if config.ToolLimiter != nil {
		session.toolLimiter = config.ToolLimiter
	}
	session.bash = config.Bash
	session.todos, session.events = handler.todos, handler.events
	session.secrets, session.events.secrets = config.Secrets, config.Secrets
	session.events.sessionID, session.events.sink = config.ID, config.EventSink
	session.bindEventHistory()
	handler.session = session
	session.background = handler.background
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
