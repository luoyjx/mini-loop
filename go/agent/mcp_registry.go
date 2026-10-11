package agent

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/mcp"
	"github.com/luoyjx/mini-loop/go/problems"
	"github.com/luoyjx/mini-loop/go/protocol"
)

const MCPDescriptionLimit = 4000

// MCPClient is the concrete discovery/call seam. Operator code retains lifecycle
// ownership; registering a client does not transfer Close to a gate snapshot.
type MCPClient interface {
	Name() string
	ListTools(context.Context) ([]mcp.ToolDescription, error)
	CallTool(context.Context, string, jsonvalue.Value) (string, error)
}

// MCPRegistry serializes publication, not execution. Every publication returns a
// new immutable catalog; existing gates keep their original handler/risk binding.
// It holds raw server ownership because normalization cannot prevent collisions.
type MCPRegistry struct {
	mu          sync.Mutex
	owners      map[protocol.ToolName]string
	definitions []ToolDefinition
	problems    problems.Log
}

type MCPRegistration struct {
	Catalog *ToolCatalog
	Added   []protocol.ToolName
}

func (registry *MCPRegistry) Problems() problems.Snapshot { return registry.problems.Snapshot() }
func (registry *MCPRegistry) Snapshot() (*ToolCatalog, error) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	return NewToolCatalog(registry.definitions...)
}
func (registry *MCPRegistry) Register(ctx context.Context, client MCPClient, timeout time.Duration) (MCPRegistration, error) {
	if client == nil {
		return MCPRegistration{}, errors.New("MCP client is required")
	}
	if timeout == 0 {
		timeout = mcp.DefaultTimeout
	}
	if timeout < 0 {
		return MCPRegistration{}, errors.New("MCP tool timeout must be positive")
	}
	tools, err := client.ListTools(ctx)
	if err != nil {
		return MCPRegistration{}, err
	}
	if err := ctx.Err(); err != nil {
		return MCPRegistration{}, err
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if registry.owners == nil {
		registry.owners = make(map[protocol.ToolName]string)
	}
	server := client.Name()
	added := make([]protocol.ToolName, 0, len(tools))
	for _, tool := range tools {
		name := protocol.MCPToolName(server, tool.Name)
		if owner, exists := registry.owners[name]; exists && owner != server {
			quoted := mcpRepr(owner)
			_ = registry.problems.Append(fmt.Sprintf("%s: refused, already provided by server %s", name, quoted))
			continue
		}
		description := tool.Description.PythonString()
		if count := jsonvalue.RuneCount(description); count > MCPDescriptionLimit {
			_ = registry.problems.Append(fmt.Sprintf("%s: description truncated from %s to 4,000 characters", name, commaMCP(count)))
			description = jsonvalue.TextPrefix(description, MCPDescriptionLimit) + " [truncated]"
		}
		annotations := tool.Annotations
		if annotations.Truth() && annotations.Kind() != jsonvalue.Object {
			return MCPRegistration{}, errors.New("MCP annotations must be an object when nonempty")
		}
		hint, _ := annotations.Lookup("readOnlyHint")
		schema, err := protocol.MCPToolSchema(name, "[mcp:"+server+"] "+description, tool.InputSchema)
		if err != nil {
			return MCPRegistration{}, err
		}
		handler := mcpToolHandler{client: client, original: tool.Name, timeout: timeout, name: name}
		definition, err := NewToolDefinitionWithSchema(schema, ToolTraits{Risk: RiskExternal, Readonly: hint.Truth()}, handler)
		if err != nil {
			return MCPRegistration{}, err
		}
		registry.owners[name] = server
		replaced := false
		for i, prior := range registry.definitions {
			if prior.Name() == name {
				registry.definitions[i] = definition
				replaced = true
				break
			}
		}
		if !replaced {
			registry.definitions = append(registry.definitions, definition)
		}
		added = append(added, name)
	}
	catalog, err := NewToolCatalog(registry.definitions...)
	return MCPRegistration{Catalog: catalog, Added: added}, err
}

func commaMCP(n int) string {
	s := strconv.Itoa(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

type mcpToolHandler struct {
	client   MCPClient
	original string
	timeout  time.Duration
	name     protocol.ToolName
}

func (handler mcpToolHandler) ExecuteTool(ctx context.Context, authority ToolAuthority, input protocol.ToolInput) (string, error) {
	if err := authority.Validate(); err != nil {
		return "", err
	}
	arguments, ok := input.MCP()
	if !ok || input.Name() != handler.name {
		return "", errors.New("MCP handler input does not match its registered tool")
	}
	bounded, cancel := context.WithTimeout(ctx, handler.timeout)
	defer cancel()
	output, err := handler.client.CallTool(bounded, handler.original, arguments.Arguments)
	if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
		seconds := strconv.FormatFloat(handler.timeout.Seconds(), 'f', -1, 64)
		if !containsDecimal(seconds) {
			seconds += ".0"
		}
		return fmt.Sprintf("Error: MCP tool %s timed out after %ss", mcpRepr(handler.original), seconds), nil
	}
	return output, err
}
func containsDecimal(s string) bool {
	for _, r := range s {
		if r == '.' {
			return true
		}
	}
	return false
}

// Python repr chooses quote style and escaping; a one-element closed array
// supplies the existing shared text repr without creating a second encoder.
func mcpRepr(text string) string {
	rendered := jsonvalue.ArrayValue([]jsonvalue.Value{jsonvalue.TextValue(text)}).PythonString()
	return rendered[1 : len(rendered)-1]
}
