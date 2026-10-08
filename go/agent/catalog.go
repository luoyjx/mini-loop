package agent

import (
	"context"
	"errors"
	"fmt"

	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/shell"
)

type ToolRisk string

const (
	RiskRead         ToolRisk = "read"
	RiskWrite        ToolRisk = "write"
	RiskExec         ToolRisk = "exec"
	RiskExternal     ToolRisk = "external"
	RiskUnclassified ToolRisk = "unclassified"
)

func (risk ToolRisk) valid() bool {
	switch risk {
	case RiskRead, RiskWrite, RiskExec, RiskExternal, RiskUnclassified:
		return true
	default:
		return false
	}
}

type Capability string

const (
	CapabilityProcessExec         Capability = "process.exec"
	CapabilityRepoRead            Capability = "repo.read"
	CapabilityRepoSearch          Capability = "repo.search"
	CapabilityWorkspaceWrite      Capability = "workspace.write"
	CapabilityRepoSemanticOutline Capability = "repo.semantic_outline"
	CapabilityRepoSymbol          Capability = "repo.symbol"
	CapabilityRepoReferences      Capability = "repo.references"
	CapabilityObservationRecover  Capability = "observation.recover"
)

type ToolTraits struct {
	Risk         ToolRisk
	Readonly     bool
	ParallelSafe bool
	Capabilities []Capability
}

type ToolAuthority struct {
	SessionID  SessionID
	OwnerID    OwnerID
	Workspace  string
	Mode       PermissionMode
	RunContext RunContext
	ActionID   ActionID
	ToolUseID  string
}

func (authority ToolAuthority) Validate() error {
	if authority.SessionID == "" || authority.OwnerID == "" || !authority.Mode.Valid() {
		return errors.New("tool authority requires session, owner, and valid permission mode")
	}
	if authority.RunContext.MessageID() != "" {
		return authority.RunContext.Validate()
	}
	return nil
}

type ToolCall struct {
	ID    string
	Input protocol.ToolInput
}

func (call ToolCall) Name() protocol.ToolName { return call.Input.Name() }

func (call ToolCall) Validate() error {
	if call.ID == "" {
		return errors.New("tool call requires an ID")
	}
	return call.Input.Validate()
}

// ToolHandler receives one closed tool-input variant. Adapters for individual
// tools must assert their expected variant before performing an effect.
type ToolHandler interface {
	ExecuteTool(context.Context, ToolAuthority, protocol.ToolInput) (string, error)
}

type ToolDefinition struct {
	name         protocol.ToolName
	risk         ToolRisk
	readonly     bool
	parallelSafe bool
	capabilities []Capability
	handler      ToolHandler
	schema       protocol.ToolSchema
	verifier     ToolVerifier
	classifier   ExecutionClassifier
}

type EffectVerdict string

const (
	EffectUndetermined   EffectVerdict = "undetermined"
	EffectAlreadyApplied EffectVerdict = "already_applied"
	EffectNotApplied     EffectVerdict = "not_applied"
)

type ToolVerifier interface {
	VerifyTool(context.Context, ToolAuthority, ToolCall) (EffectVerdict, error)
}

// WithVerifier creates a new immutable definition. Failure or an invalid verdict
// means undetermined; neither can authorize retry of an unknown action.
func (definition ToolDefinition) WithVerifier(verifier ToolVerifier) ToolDefinition {
	definition.verifier = verifier
	return definition
}

func verifyEffect(ctx context.Context, definition ToolDefinition, authority ToolAuthority, call ToolCall) (verdict EffectVerdict) {
	verdict = EffectUndetermined
	defer func() {
		if recover() != nil {
			verdict = EffectUndetermined
		}
	}()
	if definition.verifier == nil {
		return
	}
	v, err := definition.verifier.VerifyTool(ctx, authority, call)
	if err == nil && (v == EffectAlreadyApplied || v == EffectNotApplied) {
		verdict = v
	}
	return
}

func NewToolDefinition(name protocol.ToolName, traits ToolTraits, handler ToolHandler) (ToolDefinition, error) {
	schema, ok := protocol.DefaultToolSchema(name)
	if !ok {
		return ToolDefinition{}, fmt.Errorf("tool %s requires an explicit schema", name)
	}
	return NewToolDefinitionWithSchema(schema, traits, handler)
}

func NewToolDefinitionWithSchema(schema protocol.ToolSchema, traits ToolTraits, handler ToolHandler) (ToolDefinition, error) {
	name := schema.Name
	if err := schema.Validate(); err != nil {
		return ToolDefinition{}, err
	}
	if name == "" || !traits.Risk.valid() || handler == nil || (traits.Readonly && traits.Risk != RiskRead && !(protocol.IsMCPToolName(name) && traits.Risk == RiskExternal)) {
		return ToolDefinition{}, errors.New("tool definition requires name, valid risk, matching readonly claim, and handler")
	}
	seen := make(map[Capability]bool, len(traits.Capabilities))
	for _, capability := range traits.Capabilities {
		if capability == "" || seen[capability] {
			return ToolDefinition{}, errors.New("tool capabilities must be non-empty and unique")
		}
		seen[capability] = true
	}
	return ToolDefinition{
		name: name, risk: traits.Risk, readonly: traits.Readonly,
		parallelSafe: traits.ParallelSafe,
		capabilities: append([]Capability(nil), traits.Capabilities...), handler: handler, schema: schema.Clone(),
	}, nil
}

func (definition ToolDefinition) Name() protocol.ToolName { return definition.name }
func (definition ToolDefinition) Risk() ToolRisk          { return definition.risk }
func (definition ToolDefinition) Readonly() bool          { return definition.readonly }
func (definition ToolDefinition) ParallelSafe() bool      { return definition.parallelSafe }
func (definition ToolDefinition) Capabilities() []Capability {
	return append([]Capability(nil), definition.capabilities...)
}

// ToolCatalog copies the ordered definitions at construction. No runtime
// mutation can change which handler and risk a model turn was bound to.
type ToolCatalog struct {
	ordered []ToolDefinition
	index   map[protocol.ToolName]int
}

func NewToolCatalog(definitions ...ToolDefinition) (*ToolCatalog, error) {
	index := make(map[protocol.ToolName]int, len(definitions))
	ordered := make([]ToolDefinition, len(definitions))
	for i, definition := range definitions {
		if err := definition.schema.Validate(); err != nil || definition.schema.Name != definition.name {
			return nil, fmt.Errorf("tool %d has an invalid schema", i)
		}
		if definition.name == "" || !definition.risk.valid() || definition.handler == nil || (definition.readonly && definition.risk != RiskRead && !(protocol.IsMCPToolName(definition.name) && definition.risk == RiskExternal)) {
			return nil, fmt.Errorf("tool %d has an invalid definition", i)
		}
		if _, exists := index[definition.name]; exists {
			return nil, fmt.Errorf("duplicate tool %q", definition.name)
		}
		index[definition.name] = i
		definition.capabilities = append([]Capability(nil), definition.capabilities...)
		definition.schema = definition.schema.Clone()
		ordered[i] = definition
	}
	return &ToolCatalog{ordered: ordered, index: index}, nil
}

func (catalog *ToolCatalog) Names() []protocol.ToolName {
	names := make([]protocol.ToolName, len(catalog.ordered))
	for i, definition := range catalog.ordered {
		names[i] = definition.name
	}
	return names
}

func (catalog *ToolCatalog) Lookup(name protocol.ToolName) (ToolDefinition, bool) {
	i, exists := catalog.index[name]
	if !exists {
		return ToolDefinition{}, false
	}
	definition := catalog.ordered[i]
	definition.capabilities = append([]Capability(nil), definition.capabilities...)
	definition.schema = definition.schema.Clone()
	return definition, true
}

type bashHandler struct{ executor BashExecutor }

// BashResultExecutor is an optional extension of the original string seam.
// The built-in shell.Executor supplies masked streams and typed status metadata.
type BashResultExecutor interface {
	ExecuteBashResult(context.Context, protocol.BashInput) (shell.Result, error)
}
type detailedToolHandler interface {
	executeDetailedTool(context.Context, ToolAuthority, protocol.ToolInput) (string, *shell.Result, error)
}

func (handler bashHandler) ExecuteTool(ctx context.Context, authority ToolAuthority, input protocol.ToolInput) (string, error) {
	output, _, err := handler.executeDetailedTool(ctx, authority, input)
	return output, err
}
func (handler bashHandler) executeDetailedTool(ctx context.Context, authority ToolAuthority, input protocol.ToolInput) (string, *shell.Result, error) {
	bash, ok := input.Bash()
	if !ok {
		return "", nil, errors.New("bash handler received a non-bash input")
	}
	if bound, ok := handler.executor.(interface{ Workspace() string }); ok && bound.Workspace() != authority.Workspace {
		return "", nil, errors.New("bash executor is bound to a different workspace")
	}
	if executor, ok := handler.executor.(BashResultExecutor); ok {
		result, err := executor.ExecuteBashResult(ctx, bash)
		return result.Render(), &result, err
	}
	output, err := handler.executor.ExecuteBash(ctx, bash)
	return output, nil, err
}

func NewBashToolCatalog(executor BashExecutor) (*ToolCatalog, error) {
	if executor == nil {
		return nil, errors.New("bash executor is required")
	}
	definition, err := NewToolDefinition(protocol.ToolBash, ToolTraits{
		Risk: RiskExec, Capabilities: []Capability{CapabilityProcessExec},
	}, bashHandler{executor})
	if err != nil {
		return nil, err
	}
	return NewToolCatalog(definition)
}
