package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/luoyjx/mini-loop/go/memory"
	"github.com/luoyjx/mini-loop/go/protocol"
)

// StaticSystemSection adapts a fixed string to the system-builder seam.
// Empty sections are omitted by SectionsBuilder; whitespace is preserved.
type StaticSystemSection string

func (section StaticSystemSection) BuildSystem(SystemContext) (string, error) {
	return string(section), nil
}

// SystemBuilderFunc adapts a named, typed callback without retaining an Agent.
type SystemBuilderFunc func(SystemContext) (string, error)

func (build SystemBuilderFunc) BuildSystem(value SystemContext) (string, error) {
	if build == nil {
		return "", PromptCompositionError("system builder callback is required")
	}
	return build(value)
}

// PromptCompositionError identifies a missing native composition dependency.
// Source callback and store errors otherwise pass through unchanged.
type PromptCompositionError string

func (err PromptCompositionError) Error() string { return string(err) }

type sectionSystemBuilder struct{ sections []SystemBuilder }

// SectionsBuilder captures the section order. Each BuildSystem evaluates every
// section in order, omits empty results and joins the rest with blank lines.
// Callback failures stop evaluation immediately. Callers retain ownership of
// callbacks; construction detaches the section slice, not callback state.
func SectionsBuilder(sections ...SystemBuilder) SystemBuilder {
	return sectionSystemBuilder{sections: append([]SystemBuilder(nil), sections...)}
}

func (builder sectionSystemBuilder) BuildSystem(value SystemContext) (string, error) {
	parts := make([]string, 0, len(builder.sections))
	for _, section := range builder.sections {
		if section == nil {
			return "", PromptCompositionError("system section is required")
		}
		part, err := section.BuildSystem(value)
		if err != nil {
			return "", err
		}
		if part != "" {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, "\n\n"), nil
}

// RuntimeFactsContext is a caller-owned observation, never session authority.
// UsedTokens is the caller's context_used observation, after its chosen meter
// and request-envelope policy. Memory must already be bound to its owner.
type RuntimeFactsContext struct {
	Todos           []protocol.TodoItem
	UsedTokens      int
	TokenThreshold  int
	Memory          *memory.ScopedStore
	RecallAvailable bool
}

// RuntimeFacts renders volatile facts without updating a transcript, meter or
// default system builder. A bound store is listed before recall availability is
// tested, preserving the public Source helper's evaluation order.
func RuntimeFacts(ctx context.Context, value RuntimeFactsContext) (string, error) {
	if ctx == nil {
		return "", PromptCompositionError("runtime facts context is required")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	var parts []string
	if len(value.Todos) != 0 {
		parts = append(parts, "Current TodoWrite state:\n"+renderTodos(value.Todos))
	}
	if value.TokenThreshold > 0 {
		for _, band := range []struct {
			fraction float64
			label    string
		}{{.90, "over 90%"}, {.75, "over 75%"}} {
			if float64(value.UsedTokens) >= float64(value.TokenThreshold)*band.fraction {
				parts = append(parts, fmt.Sprintf("Context is %s full; older tool output may be cleared automatically to make room. The `compress` tool frees space deliberately.", band.label))
				break
			}
		}
	}
	if value.Memory != nil {
		records, err := value.Memory.List(ctx)
		if err != nil {
			return "", err
		}
		if len(records) != 0 && value.RecallAvailable {
			index, err := value.Memory.Index(ctx)
			if err != nil {
				return "", err
			}
			parts = append(parts, "Known user memories (use `recall` for full text):\n"+index)
		}
	}
	return strings.Join(parts, "\n\n"), nil
}

type memorySystemBuilder struct {
	ctx   context.Context
	base  SystemBuilder
	store *memory.ScopedStore
}

// MemorySystemBuilder explicitly selects the legacy memory-in-system shape.
// It is never installed by default. The caller owns ctx and the bound store's
// lifetime; each build calls base first, then reads the current scoped index,
// including the empty-store placeholder. No owner override is accepted.
func MemorySystemBuilder(ctx context.Context, base SystemBuilder, store *memory.ScopedStore) SystemBuilder {
	return memorySystemBuilder{ctx: ctx, base: base, store: store}
}

func (builder memorySystemBuilder) BuildSystem(value SystemContext) (string, error) {
	if builder.base == nil {
		return "", PromptCompositionError("base system builder is required")
	}
	base, err := builder.base.BuildSystem(value)
	if err != nil {
		return "", err
	}
	if builder.ctx == nil || builder.store == nil {
		return "", PromptCompositionError("memory builder context and scoped store are required")
	}
	index, err := builder.store.Index(builder.ctx)
	if err != nil {
		return "", err
	}
	return base + "\n\nKnown user memories (use `recall` to load full text):\n" + index, nil
}
