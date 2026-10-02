package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/luoyjx/mini-loop/go/protocol"
)

const DefaultSubagentMaxDepth = 2
const DefaultSubagentMaxRounds = 30
const SubagentDisplayCap = 2000

type SubagentLineage struct {
	Parent          string `json:"parent"`
	DelegationDepth int    `json:"delegation_depth"`
}

// SubagentParent is a pinned environment, not the mutable parent session.
// Providers can inspect its identity/settings/catalogue or use the default
// in-process provider, without reentering the parent's serialized run lock.
type SubagentParent struct {
	authority                                      ToolAuthority
	label                                          string
	depth                                          int
	model                                          string
	maxTokens, tokenThreshold, maxRounds, maxDepth int
	provider                                       Provider
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
}

func (parent SubagentParent) Authority() ToolAuthority {
	value := parent.authority
	value.RunContext = value.RunContext.clone()
	return value
}
func (parent SubagentParent) Label() string { return parent.label }
func (parent SubagentParent) Depth() int    { return parent.depth }

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
	if err := ctx.Err(); err != nil {
		return "", err
	}
	parent := request.Parent
	if parent.catalog == nil || parent.rolePolicy == nil || parent.provider == nil || parent.events == nil {
		return "", errors.New("subagent requires a bound parent environment")
	}
	childContext, err := request.RunContext.DerivePeerAgent(parent.label)
	if err != nil {
		return "", err
	}
	catalog, err := parent.rolePolicy.Select(request.Role, parent.catalog)
	if err != nil {
		return "", err
	}
	if catalog == nil {
		return "", errors.New("role policy returned a nil catalogue")
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
		binding: ToolAuthority{SessionID: id, OwnerID: parent.authority.OwnerID, Workspace: parent.authority.Workspace, Mode: mode},
		todos:   &TodoManager{}, events: &sessionEvents{parent: parent.events, secrets: parent.secrets, sessionID: parent.events.sessionID}, skills: parent.skills, questions: questions, compression: &compressionSignal{},
	}
	definitions := append([]ToolDefinition(nil), catalog.ordered...)
	// Built-in stateful handlers must bind the fresh child. Custom handlers
	// receive its authority through the same gate and retain their own contract.
	for i := range definitions {
		if _, bound := definitions[i].handler.(*runtimeHandler); bound {
			definitions[i].handler = handler
		}
	}
	catalog, err = NewToolCatalog(definitions...)
	if err != nil {
		return "", err
	}
	gate, err := NewToolGate(catalog, parent.policy, parent.hooks)
	if err != nil {
		return "", err
	}
	gate.secrets = parent.secrets
	child, err := NewSessionWithGate(id, parent.authority.OwnerID, parent.provider, gate, mode, parent.authority.Workspace, parent.maxRounds)
	if err != nil {
		return "", err
	}
	child.label, child.depth = parent.label+">"+strings.ToLower(string(request.Role)), parent.depth+1
	child.secrets = parent.secrets
	child.bash = nil
	for _, definition := range definitions {
		if handler, ok := definition.handler.(bashHandler); ok {
			child.bash = handler.executor
		}
	}
	child.model, child.maxTokens, child.tokenThreshold = parent.model, parent.maxTokens, parent.tokenThreshold
	child.skills, child.questions, child.compactor = parent.skills, questions, parent.compactor
	if parent.cachePolicy != nil {
		child.cachePolicy = parent.cachePolicy
	}
	if parent.stuckDetector != nil {
		child.stuckDetector = parent.stuckDetector
	}
	child.stopHooks = append([]StopHook(nil), parent.stopHooks...)
	child.subagents, child.rolePolicy = parent.subagents, parent.rolePolicy
	child.subagentMaxDepth, child.subagentMaxRounds = parent.maxDepth, parent.maxRounds
	child.todos, child.events, child.compression = handler.todos, handler.events, handler.compression
	child.bindEventHistory()
	child.systemBuilder = FixedSystem(fmt.Sprintf("You are a %s subagent in %s. Use tools to %s, then give a concise final summary. No preamble.", request.Role, parent.authority.Workspace, verb))
	child.lineage = &lineage
	handler.session = child
	provider.mu.Lock()
	provider.lastLineage = &lineage
	provider.mu.Unlock()
	return child.RunWithContext(ctx, request.Prompt, childContext)
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
	s.events.append(SessionEvent{kind: EventSubagentStart, subagent: SubagentEvent{kind: EventSubagentStart, role: role, prompt: capSubagentDisplay(prompt)}})
	parent := SubagentParent{
		authority: ToolAuthority{SessionID: s.id, OwnerID: s.owner, Workspace: s.workspace, Mode: s.mode, RunContext: run.clone()}, label: s.label, depth: s.depth,
		model: s.model, maxTokens: s.maxTokens, tokenThreshold: s.tokenThreshold, maxRounds: s.subagentMaxRounds, maxDepth: s.subagentMaxDepth,
		provider: s.provider, catalog: s.gate.catalog, policy: s.gate.policy, hooks: GateHooks{Before: append([]BeforeHook(nil), s.gate.before...), Guards: append([]GuardHook(nil), s.gate.guards...), After: append([]AfterHook(nil), s.gate.after...), Observers: append([]ResultObserver(nil), s.gate.observers...)},
		skills: s.skills, questions: s.questions, compactor: s.compactor, subagents: s.subagents, rolePolicy: s.rolePolicy, events: s.events, secrets: s.secrets, cachePolicy: s.cachePolicy, stuckDetector: s.stuckDetector, stopHooks: append([]StopHook(nil), s.stopHooks...),
	}
	summary, err := s.subagents.RunSubagent(ctx, SubagentRequest{parent, prompt, role, run.clone()})
	if err != nil {
		return "", err
	}
	s.events.append(SessionEvent{kind: EventSubagentEnd, subagent: SubagentEvent{kind: EventSubagentEnd, role: role, summary: capSubagentDisplay(summary)}})
	if summary == "" {
		return "(subagent produced no summary)", nil
	}
	return summary, nil
}
