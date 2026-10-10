package protocol

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

type CallerKind string

const (
	CallerDirect            CallerKind = "direct"
	CallerCodeExecution2025 CallerKind = "code_execution_20250825"
	CallerCodeExecution2026 CallerKind = "code_execution_20260120"
)

// ToolCaller is the closed provider caller union for tool-use and server-result blocks.
// The fake provider leaves it absent; server-side tool callers carry a tool ID.
type ToolCaller struct {
	kind   CallerKind
	toolID string
	extra  jsonvalue.Value
}

func DirectToolCaller() ToolCaller { return ToolCaller{kind: CallerDirect} }

func CodeExecutionToolCaller(kind CallerKind, toolID string) (ToolCaller, error) {
	if kind != CallerCodeExecution2025 && kind != CallerCodeExecution2026 {
		return ToolCaller{}, fmt.Errorf("not a code execution caller kind: %q", kind)
	}
	caller := ToolCaller{kind: kind, toolID: toolID}
	if err := caller.Validate(); err != nil {
		return ToolCaller{}, err
	}
	return caller, nil
}

func (caller ToolCaller) Kind() CallerKind { return caller.kind }
func (caller ToolCaller) ToolID() string   { return caller.toolID }

func (caller ToolCaller) Validate() error {
	switch caller.kind {
	case CallerDirect:
		if caller.toolID != "" {
			return errors.New("direct caller cannot carry a tool ID")
		}
	case CallerCodeExecution2025, CallerCodeExecution2026:
		if caller.toolID == "" {
			return errors.New("code execution caller requires a tool ID")
		}
	default:
		return fmt.Errorf("unsupported caller kind %q", caller.kind)
	}
	return nil
}

func (caller ToolCaller) MarshalJSON() ([]byte, error) {
	if err := caller.Validate(); err != nil {
		return nil, err
	}
	if caller.kind == CallerDirect {
		return marshalMetadataObject(struct {
			Type CallerKind `json:"type"`
		}{Type: caller.kind}, caller.extra)
	}
	return marshalMetadataObject(struct {
		Type   CallerKind `json:"type"`
		ToolID string     `json:"tool_id"`
	}{Type: caller.kind, ToolID: caller.toolID}, caller.extra)
}

func (caller *ToolCaller) UnmarshalJSON(data []byte) error {
	next, err := decodeToolCaller(data, true)
	if err == nil {
		*caller = next
	}
	return err
}

func decodeToolCaller(data []byte, bounded bool) (ToolCaller, error) {
	var tag struct {
		Type CallerKind `json:"type"`
	}
	if err := json.Unmarshal(data, &tag); err != nil {
		return ToolCaller{}, err
	}
	var next ToolCaller
	switch tag.Type {
	case CallerDirect:
		var wire struct {
			Type *CallerKind `json:"type"`
		}
		_, extra, err := decodeMetadataObject(data, &wire, []string{"type"}, bounded)
		if err != nil {
			return ToolCaller{}, err
		}
		if wire.Type == nil {
			return ToolCaller{}, errors.New("direct caller requires type")
		}
		next = ToolCaller{kind: *wire.Type, extra: extra}
	case CallerCodeExecution2025, CallerCodeExecution2026:
		var wire struct {
			Type   *CallerKind `json:"type"`
			ToolID *string     `json:"tool_id"`
		}
		_, extra, err := decodeMetadataObject(data, &wire, []string{"type", "tool_id"}, bounded)
		if err != nil {
			return ToolCaller{}, err
		}
		if wire.Type == nil || wire.ToolID == nil {
			return ToolCaller{}, errors.New("code execution caller requires type and tool_id")
		}
		next = ToolCaller{kind: *wire.Type, toolID: *wire.ToolID, extra: extra}
	default:
		return ToolCaller{}, fmt.Errorf("unsupported caller kind %q", tag.Type)
	}
	if err := next.Validate(); err != nil {
		return ToolCaller{}, err
	}
	return next, nil
}
