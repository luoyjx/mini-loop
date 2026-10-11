package agent

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/workflows"
	"github.com/luoyjx/mini-loop/go/workspace"
)

// WorkflowContextResolver supplies a trusted live context. Saved run snapshots
// are deliberately not an input to this authority seam.
type WorkflowContextResolver func(context.Context, workflows.NodeAttempt) (RunContext, error)

type WorkflowRunnerConfig struct {
	Provider                  Provider
	Owner                     OwnerID
	Workspace                 string
	ResolveContext            WorkflowContextResolver
	MaxRounds                 *int
	Model                     string
	MaxTokens, TokenThreshold int
	Catalog                   *ToolCatalog
	RolePolicy                RoleToolPolicy
	Recovery                  Recovery
	CachePolicy               CachePolicy
	StuckDetector             StuckDetector
	Skills                    SkillSource
	Secrets                   TextMasker
	ModelLimiter, ToolLimiter *ConcurrencyLimiter
	EventSink                 EventSink
}

type WorkflowWorkerSnapshot struct {
	ToolNames  []protocol.ToolName
	RunContext RunContextSnapshot
	Label      string
	MaxRounds  int
}

// FreshWorkflowRunner owns no parent transcript, injectors, hooks or persistence.
// Custom catalogues retain their handler bindings; readonly permission is a
// separate backstop even when an operator role policy selects a mutating tool.
type FreshWorkflowRunner struct {
	config    WorkflowRunnerConfig
	maxRounds int
	mu        sync.Mutex
	last      *WorkflowWorkerSnapshot
}

func NewFreshWorkflowRunner(config WorkflowRunnerConfig) (*FreshWorkflowRunner, error) {
	rounds := 8
	if config.MaxRounds != nil {
		rounds = *config.MaxRounds
	}
	if rounds <= 0 {
		return nil, &workflows.RunnerError{Kind: workflows.RunnerValueError, Detail: "max_rounds must be positive"}
	}
	if config.Provider == nil || config.Owner == "" || config.ResolveContext == nil {
		return nil, errors.New("workflow runner requires provider, owner and live context resolver")
	}
	if config.MaxTokens < 0 || config.TokenThreshold < 0 {
		return nil, errors.New("workflow model budgets cannot be negative")
	}
	for _, limiter := range []*ConcurrencyLimiter{config.ModelLimiter, config.ToolLimiter} {
		if limiter != nil && limiter.slots == nil {
			return nil, errors.New("uninitialized concurrency limiter")
		}
	}
	files, err := workspace.NewFiles(config.Workspace)
	if err != nil {
		return nil, err
	}
	config.Workspace = files.Root()
	if config.Catalog == nil {
		var definitions []ToolDefinition
		for _, name := range []protocol.ToolName{protocol.ToolReadFile, protocol.ToolGlob} {
			capability := CapabilityRepoRead
			if name == protocol.ToolGlob {
				capability = CapabilityRepoSearch
			}
			definition, err := NewToolDefinition(name, ToolTraits{Risk: RiskRead, Readonly: true, ParallelSafe: true, Capabilities: []Capability{capability}}, workspaceFileHandler{files, name})
			if err != nil {
				return nil, err
			}
			definitions = append(definitions, definition)
		}
		config.Catalog, err = NewToolCatalog(definitions...)
		if err != nil {
			return nil, err
		}
	}
	if config.RolePolicy == nil {
		config.RolePolicy = DefaultRoleToolPolicy()
	}
	// The caller's round pointer is no longer retained after construction.
	config.MaxRounds = nil
	return &FreshWorkflowRunner{config: config, maxRounds: rounds}, nil
}

func (runner *FreshWorkflowRunner) LastWorker() (WorkflowWorkerSnapshot, bool) {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	if runner.last == nil {
		return WorkflowWorkerSnapshot{}, false
	}
	view := *runner.last
	view.ToolNames = append([]protocol.ToolName(nil), view.ToolNames...)
	view.RunContext = view.RunContext.Clone()
	return view, true
}

type workflowArtifactCapture struct {
	mu         sync.Mutex
	schema     workflows.Value
	submission *workflows.ArtifactSubmission
}

func (capture *workflowArtifactCapture) ExecuteTool(ctx context.Context, authority ToolAuthority, input protocol.ToolInput) (string, error) {
	value, ok := input.ReturnArtifact()
	if !ok {
		return "", errors.New("artifact handler received a different tool input")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	capture.mu.Lock()
	defer capture.mu.Unlock()
	if capture.submission != nil {
		return "Error: return_artifact was already submitted", nil
	}
	if err := workflows.ValidateValue(capture.schema, value.Value); err != nil {
		return "", err
	}
	submission := workflows.ReturnArtifact(value.Value)
	capture.submission = &submission
	return "Structured artifact accepted. Stop and finish this node.", nil
}

func (runner *FreshWorkflowRunner) Run(ctx context.Context, execution workflows.AttemptExecution) (*workflows.ArtifactSubmission, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	node, attempt := execution.Node, execution.Attempt.Clone()
	if err := workflows.ValidateSchema(node.OutputSchema); err != nil {
		return nil, err
	}
	catalog, err := runner.config.RolePolicy.Select(RoleExplore, runner.config.Catalog)
	if err != nil {
		return nil, err
	}
	if catalog == nil {
		return nil, errors.New("workflow role policy returned a nil catalogue")
	}
	capture := &workflowArtifactCapture{schema: node.OutputSchema}
	schema, err := protocol.WorkflowArtifactSchema(node.OutputSchema)
	if err != nil {
		return nil, err
	}
	definition, err := NewToolDefinitionWithSchema(schema, ToolTraits{Risk: RiskRead, Readonly: true}, capture)
	if err != nil {
		return nil, err
	}
	// Source register replaces a same-named operator tool; never dispatch the
	// supplied handler in place of the node's owned structured capture.
	definitions := append([]ToolDefinition(nil), catalog.ordered...)
	replaced := false
	for i := range definitions {
		if definitions[i].name == protocol.ToolReturnArtifact {
			definitions[i] = definition
			replaced = true
		}
	}
	if !replaced {
		definitions = append(definitions, definition)
	}
	catalog, err = NewToolCatalog(definitions...)
	if err != nil {
		return nil, err
	}
	gate, err := NewToolGate(catalog, DefaultPermissionPolicy(nil), GateHooks{})
	if err != nil {
		return nil, err
	}
	gate.secrets = runner.config.Secrets
	rounds := runner.maxRounds
	if node.MaxRounds != nil && *node.MaxRounds != 0 {
		rounds = min(rounds, *node.MaxRounds)
	}
	worker, err := NewSessionWithGate(SessionID(attempt.AgentID), runner.config.Owner, runner.config.Provider, gate, ModeReadonly, runner.config.Workspace, rounds)
	if err != nil {
		return nil, err
	}
	canonicalSchema, err := workflows.CanonicalJSON(node.OutputSchema)
	if err != nil {
		return nil, err
	}
	canonicalInputs, err := workflows.CanonicalJSON(execution.Inputs)
	if err != nil {
		return nil, err
	}
	worker.label, worker.depth = "workflow>"+string(node.ID), 1
	worker.systemBuilder = FixedSystem("You are an isolated read-only workflow worker. Use only the provided read-only repository tools for evidence. Do not assume access to a parent conversation. You must finish by calling return_artifact exactly once with a value matching this JSON Schema:\n" + string(canonicalSchema))
	worker.stopHooks = nil
	worker.secrets, worker.events.secrets = runner.config.Secrets, runner.config.Secrets
	worker.events.sink = runner.config.EventSink
	worker.events.setScope(EventScope{Label: worker.label, Depth: 1})
	worker.modelLimiter = runner.config.ModelLimiter
	if runner.config.ToolLimiter != nil {
		worker.toolLimiter = runner.config.ToolLimiter
	}
	if runner.config.Model != "" {
		worker.model = runner.config.Model
	}
	if runner.config.MaxTokens != 0 {
		worker.maxTokens = runner.config.MaxTokens
	}
	if runner.config.TokenThreshold != 0 {
		worker.tokenThreshold = runner.config.TokenThreshold
	}
	worker.compactor = InMemoryCompactor{worker.tokenThreshold, 50}
	if runner.config.Recovery != nil {
		worker.recovery = runner.config.Recovery
	}
	if runner.config.CachePolicy != nil {
		worker.cachePolicy = runner.config.CachePolicy
	}
	if runner.config.StuckDetector != nil {
		worker.stuckDetector = runner.config.StuckDetector
	}
	if runner.config.Skills != nil {
		worker.skills = runner.config.Skills
	}
	launch, err := runner.config.ResolveContext(ctx, attempt)
	if err != nil {
		return nil, err
	}
	if err = launch.Validate(); err != nil {
		return nil, err
	}
	peer, err := launch.DeriveNamedPeerAgent("workflow:"+string(attempt.RunID), ActorID(attempt.AgentID))
	if err != nil {
		return nil, err
	}
	runner.mu.Lock()
	runner.last = &WorkflowWorkerSnapshot{ToolNames: catalog.Names(), RunContext: peer.Snapshot(), Label: worker.label, MaxRounds: rounds}
	runner.mu.Unlock()
	task := node.PromptTemplate
	if task == "" {
		task = string(node.ID)
	}
	prompt := fmt.Sprintf("Workflow node: %s (%s)\nTask:\n%s\nStructured inputs:\n%s", node.ID, node.Kind, task, canonicalInputs)
	if _, err = worker.RunWithContext(ctx, prompt, peer); err != nil {
		return nil, err
	}
	capture.mu.Lock()
	defer capture.mu.Unlock()
	if capture.submission == nil {
		return nil, &workflows.RunnerError{Kind: workflows.RunnerRuntimeError, Detail: fmt.Sprintf("workflow node %s did not call return_artifact", node.ID)}
	}
	return capture.submission, nil
}
