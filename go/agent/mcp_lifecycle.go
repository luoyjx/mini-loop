package agent

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// ManagedMCPClient adds cleanup to the existing concrete discovery/call contract.
// Close must drain its client and must not recursively acquire the same handle.
type ManagedMCPClient interface {
	MCPClient
	Close() error
}

// MCPConnection gives a client stable identity without comparing interface values
// or conflating servers with the same raw name. Share one handle for one client.
// The manager closes it after its last acquired session drains. A later connection
// may reuse the handle after Close finishes, as with the reusable stdio client.
type MCPConnection struct {
	client  ManagedMCPClient
	mu      sync.Mutex
	holders int
	closing chan struct{}
}

func NewMCPConnection(client ManagedMCPClient) (*MCPConnection, error) {
	if client == nil {
		return nil, errors.New("MCP connection requires a client")
	}
	return &MCPConnection{client: client}, nil
}

type MCPConnectionFactory func(context.Context) (*MCPConnection, error)

type ManagedMCPServer struct {
	Alias      string
	Connection *MCPConnection
	Factory    MCPConnectionFactory
}

func normalizeManagerMCP(services *ManagerServices) error {
	servers := append([]ManagedMCPServer(nil), services.MCPServers...)
	aliases := make(map[string]bool)
	for _, server := range servers {
		if aliases[server.Alias] {
			return fmt.Errorf("duplicate MCP server alias %q", server.Alias)
		}
		aliases[server.Alias] = true
		if (server.Connection == nil) == (server.Factory == nil) {
			return errors.New("managed MCP server requires exactly one connection or factory")
		}
		if server.Connection != nil && server.Connection.client == nil {
			return errors.New("uninitialized MCP connection")
		}
	}
	services.MCPServers = servers
	return nil
}

func (connection *MCPConnection) acquire(ctx context.Context) error {
	for {
		connection.mu.Lock()
		closing := connection.closing
		if closing == nil {
			connection.holders++
			connection.mu.Unlock()
			return nil
		}
		connection.mu.Unlock()
		select {
		case <-closing:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (connection *MCPConnection) release() (err error) {
	connection.mu.Lock()
	connection.holders--
	if connection.holders != 0 {
		connection.mu.Unlock()
		return nil
	}
	done := make(chan struct{})
	connection.closing = done
	connection.mu.Unlock()
	defer func() {
		if fault := recover(); fault != nil {
			err = fmt.Errorf("MCP close panicked (%T)", fault)
		}
		connection.mu.Lock()
		connection.closing = nil
		close(done)
		connection.mu.Unlock()
	}()
	return connection.client.Close()
}

// mcpLifetime retains all acquisitions, including a factory's failed discovery
// and earlier raw-name replacements. It is released only after consumer drain.
type mcpLifetime struct {
	mu          sync.Mutex
	connections []*MCPConnection
	closed      bool
}

func (lifetime *mcpLifetime) retain(ctx context.Context, connection *MCPConnection) error {
	lifetime.mu.Lock()
	defer lifetime.mu.Unlock()
	if lifetime.closed {
		return errors.New("MCP session lifetime is closed")
	}
	for _, prior := range lifetime.connections {
		if prior == connection {
			return nil
		}
	}
	if connection == nil || connection.client == nil {
		return errors.New("MCP factory returned an uninitialized connection")
	}
	if err := connection.acquire(ctx); err != nil {
		return err
	}
	lifetime.connections = append(lifetime.connections, connection)
	return nil
}

func (lifetime *mcpLifetime) close() error {
	if lifetime == nil {
		return nil
	}
	lifetime.mu.Lock()
	if lifetime.closed {
		lifetime.mu.Unlock()
		return nil
	}
	lifetime.closed = true
	connections := lifetime.connections
	lifetime.connections = nil
	lifetime.mu.Unlock()
	var result error
	for _, connection := range connections {
		result = errors.Join(result, connection.release())
	}
	return result
}

func (manager *SessionManager) bindMCP(runtime *RuntimeConfig) {
	services := manager.config.Services
	if !services.MCPTools {
		return
	}
	lifetime := &mcpLifetime{}
	runtime.MCPTools, runtime.mcpLifetime = true, lifetime
	for _, server := range services.MCPServers {
		runtime.MCPServers = append(runtime.MCPServers, MCPServer{Alias: server.Alias, Factory: func(ctx context.Context) (MCPClient, error) {
			connection := server.Connection
			var factoryErr error
			if server.Factory != nil {
				connection, factoryErr = server.Factory(ctx)
			}
			// A non-nil connection accompanying an error is still a created resource.
			if connection != nil {
				if err := lifetime.retain(ctx, connection); err != nil {
					return nil, errors.Join(factoryErr, err)
				}
			}
			if factoryErr != nil {
				return nil, factoryErr
			}
			if connection == nil {
				return nil, errors.New("MCP factory returned no connection")
			}
			return connection.client, nil
		}})
	}
}
