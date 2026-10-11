package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/luoyjx/mini-loop/go/mcp"
	"github.com/luoyjx/mini-loop/go/problems"
	"github.com/luoyjx/mini-loop/go/secrets"
)

// MCPFactory constructs a client at connection time. It must honor cancellation
// and must not reenter the active session. Lifecycle remains operator-owned.
type MCPFactory func(context.Context) (MCPClient, error)
type MCPServer struct {
	Alias   string
	Client  MCPClient
	Factory MCPFactory
}
type mcpState struct {
	registry  MCPRegistry
	servers   []MCPServer
	index     map[string]int
	connected map[string]string
}

func newMCPState(servers []MCPServer) (*mcpState, error) {
	state := &mcpState{servers: append([]MCPServer(nil), servers...), index: make(map[string]int), connected: make(map[string]string)}
	for i, server := range state.servers {
		if _, exists := state.index[server.Alias]; exists {
			return nil, fmt.Errorf("duplicate MCP server alias %q", server.Alias)
		}
		if (server.Client == nil) == (server.Factory == nil) {
			return nil, errors.New("MCP server requires exactly one client or factory")
		}
		state.index[server.Alias] = i
	}
	return state, nil
}
func (state *mcpState) aliases() []string {
	out := make([]string, 0, len(state.servers))
	for _, server := range state.servers {
		out = append(out, server.Alias)
	}
	return out
}
func mcpAvailable(aliases []string) string {
	joined := strings.Join(aliases, ", ")
	if joined == "" {
		return "(none)"
	}
	return joined
}

type mcpWithheld interface{ Withheld() []secrets.Name }

// connect executes only as an exclusive tool while its session owns the turn.
// Completed prior parallel groups cannot race the inventory replacement. The
// source registry exposes a successful registration to the remainder of this
// tool batch, and the next model request builds a fresh fitted schema snapshot.
func (state *mcpState) connect(ctx context.Context, session *Session, alias string) (string, error) {
	if raw, exists := state.connected[alias]; exists {
		return fmt.Sprintf("MCP server '%s' already connected as '%s'", alias, raw), nil
	}
	position, exists := state.index[alias]
	if !exists {
		return fmt.Sprintf("Error: unknown MCP server '%s'. Available: %s", alias, mcpAvailable(state.aliases())), nil
	}
	spec := state.servers[position]
	client := spec.Client
	if spec.Factory != nil {
		var err error
		client, err = spec.Factory(ctx)
		if err != nil {
			return "", err
		}
	}
	if client == nil {
		return "", errors.New("MCP factory returned no client")
	}
	publication, err := state.registry.Register(ctx, client, mcp.DefaultTimeout)
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	definitions := append([]ToolDefinition(nil), session.gate.catalog.ordered...)
	for _, added := range publication.Catalog.ordered {
		replaced := false
		for i, prior := range definitions {
			if prior.Name() == added.Name() {
				definitions[i] = added
				replaced = true
				break
			}
		}
		if !replaced {
			definitions = append(definitions, added)
		}
	}
	catalog, err := NewToolCatalog(definitions...)
	if err != nil {
		return "", err
	}
	session.gate.catalog = catalog
	if report, ok := client.(mcpWithheld); ok {
		withheld := report.Withheld()
		if len(withheld) > 0 {
			limit := len(withheld)
			if limit > 3 {
				limit = 3
			}
			names := make([]string, limit)
			for i, name := range withheld[:limit] {
				names[i] = string(name)
			}
			_ = state.registry.problems.Append(fmt.Sprintf("%s: %d credential(s) withheld from the server environment (%s); add them to env_passthrough if it needs them", client.Name(), len(withheld), strings.Join(names, ", ")))
		}
	}
	state.connected[alias] = client.Name()
	names := make([]string, len(publication.Added))
	for i, name := range publication.Added {
		names[i] = string(name)
	}
	return fmt.Sprintf("Connected '%s'. Added tools: %s", alias, mcpAvailable(names)), nil
}

// MCPProblems exposes the source-style bounded connection/registration ledger.
// It does not take the turn lock, so diagnostics are safe during a client callback.
func (session *Session) MCPProblems() problems.Snapshot {
	if session.mcp == nil {
		var empty problems.Log
		return empty.Snapshot()
	}
	return session.mcp.registry.Problems()
}
