package mcp

import (
	"context"
	"errors"
	"fmt"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

// InProcessValue exposes the immutable closed JSON value contract to embedding
// callers without requiring an import of an internal package.
type InProcessValue = jsonvalue.Value

type InProcessHandler func(context.Context, InProcessValue) (InProcessValue, error)

type InProcessTool struct {
	Definition ToolDescription
	Handler    InProcessHandler
}

// InProcessClient preserves discovery order, including duplicate definitions;
// dispatch uses the last handler for each raw name, as Source does. Handler code
// must be safe for shared use and honor cancellation. Returned values use Python
// str semantics and are not capped by the stdio result limit.
type InProcessClient struct {
	name        string
	definitions []ToolDescription
	handlers    map[string]InProcessHandler
}

func NewInProcess(name string, tools []InProcessTool) *InProcessClient {
	client := &InProcessClient{name: name, definitions: make([]ToolDescription, 0, len(tools)), handlers: make(map[string]InProcessHandler)}
	for _, tool := range tools {
		client.definitions = append(client.definitions, tool.Definition)
		client.handlers[tool.Definition.Name] = tool.Handler
	}
	return client
}
func (client *InProcessClient) Name() string { return client.name }
func (client *InProcessClient) ListTools(ctx context.Context) ([]ToolDescription, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return append([]ToolDescription{}, client.definitions...), nil
}
func (client *InProcessClient) CallTool(ctx context.Context, name string, args InProcessValue) (result string, err error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	handler := client.handlers[name]
	if handler == nil {
		return fmt.Sprintf("Error: unknown MCP tool %s", name), nil
	}
	if args.Kind() != jsonvalue.Object {
		return "", errors.New("in-process MCP arguments must be an object")
	}
	defer func() {
		if fault := recover(); fault != nil {
			result = fmt.Sprintf("Error: handler panicked (%T)", fault)
			err = nil
		}
	}()
	value, err := handler(ctx, args)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return "", err
		}
		return "Error: " + err.Error(), nil
	}
	return value.PythonString(), nil
}

// Close has no process or remote resource to release and leaves handlers reusable.
func (client *InProcessClient) Close() error { return nil }
