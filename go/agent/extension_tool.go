package agent

import (
	"context"
	"errors"
	"fmt"

	"github.com/luoyjx/mini-loop/go/protocol"
)

// ExtensionInput is a concrete caller-defined argument structure. Its validation
// runs after the common tool gate; it never supplies principal or capabilities.
type ExtensionInput interface {
	Validate() error
}

// ExtensionInputCodec lowers an immutable model observation into one named Go
// input type. Decode is an execution-stage callback and must honor context.
type ExtensionInputCodec[T ExtensionInput] interface {
	DecodeExtensionInput(context.Context, protocol.ToolInput) (T, error)
}

type ExtensionToolHandler[T ExtensionInput] interface {
	ExecuteExtensionTool(context.Context, ToolAuthority, T) (string, error)
}

type extensionToolAdapter[T ExtensionInput] struct {
	name    protocol.ToolName
	codec   ExtensionInputCodec[T]
	handler ExtensionToolHandler[T]
}

func (adapter extensionToolAdapter[T]) ExecuteTool(ctx context.Context, authority ToolAuthority, input protocol.ToolInput) (output string, returnedErr error) {
	defer func() {
		if fault := recover(); fault != nil {
			output, returnedErr = "", fmt.Errorf("extension callback panic: %T", fault)
		}
	}()
	if input.Name() != adapter.name {
		return "", errors.New("extension input name does not match its bound codec")
	}
	if _, ok := input.UnknownModelToolObservation(); !ok {
		return "", errors.New("extension codec requires a model observation")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	value, err := adapter.codec.DecodeExtensionInput(ctx, input)
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := value.Validate(); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return adapter.handler.ExecuteExtensionTool(ctx, authority, value)
}

// NewTypedExtensionTool binds a schema to an explicit named-input codec and
// handler. Merely registering a custom schema still cannot execute observations.
// Native built-in/MCP variants keep their existing decoders and cannot use this seam.
func NewTypedExtensionTool[T ExtensionInput](schema protocol.ToolSchema, traits ToolTraits, codec ExtensionInputCodec[T], handler ExtensionToolHandler[T]) (ToolDefinition, error) {
	if codec == nil || handler == nil {
		return ToolDefinition{}, errors.New("typed extension requires input codec and handler")
	}
	probe, err := protocol.DecodeModelToolInput(schema.Name, []byte("{}"))
	if err != nil {
		return ToolDefinition{}, fmt.Errorf("extension name must have no native decoder: %w", err)
	}
	if _, ok := probe.UnknownModelToolObservation(); !ok {
		return ToolDefinition{}, errors.New("extension name must have no native decoder")
	}
	definition, err := NewToolDefinitionWithSchema(schema, traits, extensionToolAdapter[T]{schema.Name, codec, handler})
	if err != nil {
		return ToolDefinition{}, err
	}
	definition.extensionInput = true
	return definition, nil
}
