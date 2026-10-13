package agent

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/luoyjx/mini-loop/go/decisions"
	"github.com/luoyjx/mini-loop/go/efficiency"
	"github.com/luoyjx/mini-loop/go/internal/pytext"
	"github.com/luoyjx/mini-loop/go/memory"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/userresources"
)

const DefaultSubagentMaxDepth = 2
const DefaultSubagentMaxRounds = 30
const SubagentDisplayCap = 2000

type SubagentLineage struct {
	Parent          string    `json:"parent"`
	DelegationDepth int       `json:"delegation_depth"`
	Role            AgentRole `json:"role,omitempty"`
}

// SubagentParent is a pinned environment, not the mutable parent session.
// Providers can inspect its identity/settings/catalogue or use the default
// in-process provider, without reentering the parent's serialized run lock.
type SubagentParent struct {
	tokenEfficiency                                *efficiency.Runtime
	observationRaw                                 ObservationRawConfig
	mcpServers                                     []MCPServer
	selfAudit                                      selfAuditBinding
	memoryAuto                                     bool
	memory                                         *memory.ScopedStore
	ownerResources                                 *userresources.Resources
	decisionProvider                               decisions.Provider
	decisionLLM                                    DecisionLLMConfig
	planApprover                                   PlanApprover
	background                                     *backgroundState
	bash                                           BashExecutor
	authority                                      ToolAuthority
	label                                          string
	depth                                          int
	model                                          string
	maxTokens, tokenThreshold, maxRounds, maxDepth int
	provider                                       Provider
	recovery                                       Recovery
	streamProgress                                 streamProgressPolicy
	catalog                                        *ToolCatalog
	policy                                         *PermissionPolicy
	hooks                                          GateHooks
	skills                                         SkillSource
	questions                                      Questioner
	compactor                                      Compactor
	subagents                                      SubagentProvider
	rolePolicy                                     RoleToolPolicy
	events                                         *sessionEvents
	secrets                                        TextMasker
	cachePolicy                                    CachePolicy
	stuckDetector                                  StuckDetector
	stopHooks                                      []StopHook
	promptHooks                                    []UserPromptHook
	injectors                                      []MessageInjector
	modelLimiter, toolLimiter                      *ConcurrencyLimiter
}

func (parent SubagentParent) Authority() ToolAuthority {
	value := parent.authority
	value.RunContext = value.RunContext.clone()
	return value
}
func (parent SubagentParent) Label() string { return parent.label }
func (parent SubagentParent) UserResources() (userresources.Resources, bool) {
	if parent.ownerResources == nil {
		return userresources.Resources{}, false
	}
	return *parent.ownerResources, true
}
func (parent SubagentParent) Depth() int { return parent.depth }

type SubagentSettings struct {
	Model          string
	MaxTokens      int
	TokenThreshold int
	MaxRounds      int
	MaxDepth       int
}

func (parent SubagentParent) Settings() SubagentSettings {
	return SubagentSettings{parent.model, parent.maxTokens, parent.tokenThreshold, parent.maxRounds, parent.maxDepth}
}
func (parent SubagentParent) CatalogNames() []protocol.ToolName { return parent.catalog.Names() }

type SubagentRequest struct {
	Parent     SubagentParent
	Prompt     string
	Role       AgentRole
	RunContext RunContext
}
type SubagentProvider interface {
	RunSubagent(context.Context, SubagentRequest) (string, error)
}

// InProcessSubagents builds a fresh child in the parent's resolved workspace,
// preserving the selected handlers, hooks, policy and context services.
type InProcessSubagents struct {
	mu          sync.Mutex
	lastLineage *SubagentLineage
}

func (provider *InProcessSubagents) LastLineage() (SubagentLineage, bool) {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	if provider.lastLineage == nil {
		return SubagentLineage{}, false
	}
	return *provider.lastLineage, true
}

type FixedSystem string

func (system FixedSystem) BuildSystem(SystemContext) (string, error) { return string(system), nil }

func (provider *InProcessSubagents) RunSubagent(ctx context.Context, request SubagentRequest) (string, error) {
	child, run, err := newSubagent(ctx, request)
	if err != nil {
		return "", err
	}
	provider.mu.Lock()
	provider.lastLineage = child.lineage
	provider.mu.Unlock()
	return child.RunWithContext(ctx, request.Prompt, run)
}

// newSubagent owns the shared fresh-child construction; normal delegation and
// zero-write reviewers derive the same typed runtime seams.
func newSubagent(ctx context.Context, request SubagentRequest) (*Session, RunContext, error) {
	if err := ctx.Err(); err != nil {
		return nil, RunContext{}, err
	}
	parent := request.Parent
	if parent.catalog == nil || parent.rolePolicy == nil || parent.provider == nil || parent.events == nil {
		return nil, RunContext{}, errors.New("subagent requires a bound parent environment")
	}
	childContext, err := request.RunContext.DerivePeerAgent(parent.label)
	if err != nil {
		return nil, RunContext{}, err
	}
	catalog, err := parent.rolePolicy.Select(request.Role, parent.catalog)
	if err != nil {
		return nil, RunContext{}, err
	}
	if catalog == nil {
		return nil, RunContext{}, errors.New("role policy returned a nil catalogue")
	}
	explore := normalizedRole(request.Role) == normalizedRole(RoleExplore)
	mode := ModeInteractive
	verb := "complete the task"
	if explore {
		mode = ModeReadonly
		verb = "explore and report"
	}
	id := SessionID(parent.authority.SessionID + ">" + SessionID(childContext.MessageID()))
	lineage := SubagentLineage{Parent: parent.label, DelegationDepth: parent.depth + 1}
	questions := parent.questions
	if _, boundBroker := questions.(*ApprovalSurface); boundBroker {
		questions = nil
	}
	handler := &runtimeHandler{
		selfAudit:    parent.selfAudit,
		memory:       parent.memory,
		planApprover: parent.planApprover,
		binding:      ToolAuthority{SessionID: id, OwnerID: parent.authority.OwnerID, Workspace: parent.authority.Workspace, Mode: mode},
		todos:        &TodoManager{}, events: &sessionEvents{parent: parent.events, secrets: parent.secrets, sessionID: parent.events.sessionID}, skills: parent.skills, questions: questions, compression: &compressionSignal{},
	}
	tokenEfficiency := parent.tokenEfficiency
	if tokenEfficiency != nil {
		if store := tokenEfficiency.RawStore(); store != nil && store.Workspace() != handler.binding.Workspace {
			tokenEfficiency = tokenEfficiency.WithRawStore(nil)
		}
	}
	var allocatedRaw *efficiency.RawStore
	constructed := false
	defer func() {
		if !constructed && allocatedRaw != nil {
			allocatedRaw.Close()
		}
	}()
	if tokenEfficiency != nil && tokenEfficiency.RawStore() == nil && tokenEfficiency.ObservationEnforced() && parent.observationRaw.Persist && mode != ModeReadonly {
		if _, recovery := catalog.Lookup(protocol.ToolReadTokenArtifact); recovery {
			rawConfig := efficiency.DefaultRawStoreConfig(handler.binding.Workspace)
			if limits := parent.observationRaw.StoreLimits; limits != nil {
				rawConfig.TTLSeconds, rawConfig.MaxArtifactBytes, rawConfig.MaxTotalBytes = limits.TTLSeconds, limits.MaxArtifactBytes, limits.MaxTotalBytes
			}
			allocatedRaw, err = efficiency.NewRawStore(rawConfig)
			if err != nil {
				return nil, RunContext{}, err
			}
			tokenEfficiency = tokenEfficiency.WithRawStore(allocatedRaw)
		}
	}
	definitions := append([]ToolDefinition(nil), catalog.ordered...)
	// Built-in stateful handlers must bind the fresh child. Custom handlers
	// receive its authority through the same gate and retain their own contract.
	for i := range definitions {
		if previous, bound := definitions[i].handler.(tokenArtifactHandler); bound {
			store := previous.store
			if tokenEfficiency != nil {
				store = tokenEfficiency.RawStore()
			}
			definitions[i], err = tokenArtifactDefinition(handler.binding, store)
			if err != nil {
				return nil, RunContext{}, err
			}
		}
		if previous, bound := definitions[i].handler.(*runtimeHandler); bound {
			if protocol.IsASTTool(definitions[i].name) {
				handler.ast = previous.ast
			}
			if definitions[i].name == protocol.ToolDiagnostics {
				handler.diagnostics = previous.diagnostics
			}
			definitions[i].handler = handler
		}
	}
	for _, definition := range definitions {
		if definition.name == protocol.ToolConnectMCP && definition.handler == handler {
			// Source binds the selected connector to fresh child state. Managed
			// factory closures retain their enclosing parent's drained lifetime.
			handler.mcp, err = newMCPState(parent.mcpServers)
			if err != nil {
				return nil, RunContext{}, err
			}
			break
		}
	}
	catalog, err = NewToolCatalog(definitions...)
	if err != nil {
		return nil, RunContext{}, err
	}
	hooks := parent.hooks
	hooks.Guards = append([]GuardHook(nil), parent.hooks.Guards...)
	for i, guard := range hooks.Guards {
		if _, bound := guard.(teamAuthorityGuard); bound {
			// Source delegated children inherit tools, but no manager/bus state.
			// They require their own identity guard, never the parent's binding.
			hooks.Guards[i] = teamAuthorityGuard{handler}
		}
	}
	gate, err := NewToolGate(catalog, parent.policy, hooks)
	if err != nil {
		return nil, RunContext{}, err
	}
	gate.secrets = parent.secrets
	child, err := NewSessionWithGate(id, parent.authority.OwnerID, parent.provider, gate, mode, parent.authority.Workspace, parent.maxRounds)
	if err != nil {
		return nil, RunContext{}, err
	}
	child.label, child.depth = parent.label+">"+pytext.Lower(string(request.Role)), parent.depth+1
	child.tokenEfficiency = tokenEfficiency
	child.observationRaw = parent.observationRaw.clone()
	// Source children get fresh session state; the parent's concise opt-in and
	// request projection ledger are not inherited. Policies retain shared identity.
	child.secrets = parent.secrets
	child.bash = nil
	for _, definition := range definitions {
		if definition.name == protocol.ToolBash {
			child.bash = parent.bash
		}
		if handler, ok := definition.handler.(bashHandler); ok {
			child.bash = handler.executor
		}
	}
	child.selfAudit = parent.selfAudit
	child.planApprover = parent.planApprover
	child.ownerResources = parent.ownerResources
	child.memory = parent.memory
	child.memoryAuto = parent.memoryAuto
	child.decisionProvider, child.decisionLLM = parent.decisionProvider, parent.decisionLLM
	child.recovery = parent.recovery
	child.streamProgress = parent.streamProgress
	child.model, child.maxTokens, child.tokenThreshold = parent.model, parent.maxTokens, parent.tokenThreshold
	child.skills, child.questions, child.compactor = parent.skills, questions, parent.compactor
	if parent.cachePolicy != nil {
		child.cachePolicy = parent.cachePolicy
	}
	if parent.stuckDetector != nil {
		child.stuckDetector = parent.stuckDetector
	}
	child.stopHooks = append([]StopHook(nil), parent.stopHooks...)
	child.promptHooks = append([]UserPromptHook(nil), parent.promptHooks...)
	child.injectors = cloneInjectors(parent.injectors)
	child.modelLimiter, child.toolLimiter = parent.modelLimiter, parent.toolLimiter
	child.subagents, child.rolePolicy = parent.subagents, parent.rolePolicy
	child.subagentMaxDepth, child.subagentMaxRounds = parent.maxDepth, parent.maxRounds
	child.todos, child.events, child.compression = handler.todos, handler.events, handler.compression
	child.bindEventHistory()
	child.systemBuilder = FixedSystem(fmt.Sprintf("You are a %s subagent in %s. Use tools to %s, then give a concise final summary. No preamble.", request.Role, parent.authority.Workspace, verb))
	child.lineage = &lineage
	handler.session = child
	child.mcp = handler.mcp
	for _, definition := range definitions {
		if definition.handler == handler && (definition.name == protocol.ToolBackgroundRun || definition.name == protocol.ToolCheckBackground) {
			child.background, err = parent.background.child(ctx, string(childContext.MessageID()))
			if err != nil {
				return nil, RunContext{}, err
			}
			handler.background = child.background
			break
		}
	}
	constructed = true
	return child, childContext, nil
}

func capSubagentDisplay(value string) string {
	runes := []rune(value)
	return string(runes[:min(SubagentDisplayCap, len(runes))])
}

// Delegate is the programmatic counterpart to task. Both paths enforce depth
// before invoking any custom provider or emitting a start event.
func (s *Session) Delegate(ctx context.Context, prompt string, role AgentRole) (string, error) {
	run, err := DefaultRunContext()
	if err != nil {
		return "", err
	}
	return s.DelegateWithContext(ctx, prompt, role, run)
}
func (s *Session) DelegateWithContext(ctx context.Context, prompt string, role AgentRole, run RunContext) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := run.Validate(); err != nil {
		return "", err
	}
	s.events.setScope(EventScope{s.label, s.depth, run.clone()})
	return s.runSubagent(ctx, prompt, role, run)
}
func (s *Session) runSubagent(ctx context.Context, prompt string, role AgentRole, run RunContext) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	childDepth := s.depth + 1
	if childDepth > s.subagentMaxDepth {
		s.events.append(SessionEvent{kind: EventSubagentRefused, subagent: SubagentEvent{kind: EventSubagentRefused, role: role, childDepth: childDepth, limit: s.subagentMaxDepth}})
		return fmt.Sprintf("(delegation refused: depth %d exceeds subagent_max_depth=%d; do the work directly)", childDepth, s.subagentMaxDepth), nil
	}
	s.events.appendRecorded(SessionEvent{kind: EventSubagentStart, subagent: SubagentEvent{kind: EventSubagentStart, role: role, prompt: capSubagentDisplay(prompt)}}, trajectoryDetails{kind: EventSubagentStart, text: prompt})
	parent := s.subagentParent(run)
	summary, err := s.subagents.RunSubagent(ctx, SubagentRequest{parent, prompt, role, run.clone()})
	if err != nil {
		return "", err
	}
	s.events.appendRecorded(SessionEvent{kind: EventSubagentEnd, subagent: SubagentEvent{kind: EventSubagentEnd, role: role, summary: capSubagentDisplay(summary)}}, trajectoryDetails{kind: EventSubagentEnd, text: summary})
	if summary == "" {
		return "(subagent produced no summary)", nil
	}
	return summary, nil
}

// Caller holds the session's turn lock. The returned environment detaches mutable
// slices and binds the current workspace without reentering the parent run.
func (s *Session) subagentParent(run RunContext) SubagentParent {
	parent := SubagentParent{
		tokenEfficiency:  s.tokenEfficiency,
		observationRaw:   s.observationRaw.clone(),
		selfAudit:        s.selfAudit,
		memoryAuto:       s.memoryAuto,
		memory:           s.memory,
		ownerResources:   s.ownerResources,
		decisionProvider: s.decisionProvider, decisionLLM: s.decisionLLM,
		planApprover: s.planApprover,
		background:   s.background,
		bash:         s.bash,
		authority:    ToolAuthority{SessionID: s.id, OwnerID: s.owner, Workspace: s.executionRoot(), Mode: s.permissionMode(), RunContext: run.clone()}, label: s.label, depth: s.depth,
		model: s.model, maxTokens: s.maxTokens, tokenThreshold: s.tokenThreshold, maxRounds: s.subagentMaxRounds, maxDepth: s.subagentMaxDepth,
		provider: s.provider, recovery: s.recovery, streamProgress: s.streamProgress, catalog: s.gate.catalog, policy: s.gate.policy, hooks: GateHooks{Before: append([]BeforeHook(nil), s.gate.before...), Guards: append([]GuardHook(nil), s.gate.guards...), After: append([]AfterHook(nil), s.gate.after...), Observers: append([]ResultObserver(nil), s.gate.observers...)},
		promptHooks: append([]UserPromptHook(nil), s.promptHooks...), injectors: cloneInjectors(s.injectors), modelLimiter: s.modelLimiter, toolLimiter: s.toolLimiter,
		skills: s.skills, questions: s.questions, compactor: s.compactor, subagents: s.subagents, rolePolicy: s.rolePolicy, events: s.events, secrets: s.secrets, cachePolicy: s.cachePolicy, stuckDetector: s.stuckDetector, stopHooks: append([]StopHook(nil), s.stopHooks...),
	}
	if s.mcp != nil {
		parent.mcpServers = append([]MCPServer(nil), s.mcp.servers...)
	}
	return parent
}
