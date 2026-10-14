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
	CallerObserved          CallerKind = "sdk_observation"
)

// ToolCaller is the closed provider caller union for tool-use and server-result blocks.
// The fake provider leaves it absent. SDK caller IDs are inert observations;
// the enclosing tool-use ID remains the execution identity.
type ToolCaller struct {
	kind           CallerKind
	toolID         SDKMetadataText
	extra          jsonvalue.Value
	sdkObservation *SDKCallerObservation
}

func DirectToolCaller() ToolCaller { return ToolCaller{kind: CallerDirect} }

func CodeExecutionToolCaller(kind CallerKind, toolID string) (ToolCaller, error) {
	if kind != CallerCodeExecution2025 && kind != CallerCodeExecution2026 {
		return ToolCaller{}, fmt.Errorf("not a code execution caller kind: %q", kind)
	}
	if toolID == "" {
		return ToolCaller{}, errors.New("code execution caller requires a tool ID")
	}
	caller := ToolCaller{kind: kind, toolID: MetadataText(toolID)}
	if err := caller.Validate(); err != nil {
		return ToolCaller{}, err
	}
	return caller, nil
}

func (caller ToolCaller) Kind() CallerKind {
	if caller.sdkObservation != nil {
		typeValue, _ := caller.sdkObservation.value.Lookup("type")
		tag, isText := typeValue.Text()
		if isText {
			switch CallerKind(tag) {
			case CallerDirect, CallerCodeExecution2025, CallerCodeExecution2026:
				return CallerKind(tag)
			}
		}
		return CallerObserved
	}
	return caller.kind
}

// ToolID returns the text projection, or an empty string for non-text metadata.
func (caller ToolCaller) ToolID() string {
	if text, ok := caller.ToolIDMetadata().Text(); ok {
		return text
	}
	return ""
}

// ToolIDMetadata retains nullable, non-text and legacy numeric SDK observations.
func (caller ToolCaller) ToolIDMetadata() SDKMetadataText {
	if caller.sdkObservation != nil {
		if caller.Kind() == CallerCodeExecution2025 || caller.Kind() == CallerCodeExecution2026 {
			value, _ := caller.sdkObservation.value.Lookup("tool_id")
			return SDKMetadataText{value: value}
		}
		return SDKMetadataText{}
	}
	return caller.toolID
}

func (caller ToolCaller) SDKObservation() (SDKCallerObservation, bool) {
	if caller.sdkObservation == nil {
		return SDKCallerObservation{}, false
	}
	return *caller.sdkObservation, true
}

func (caller ToolCaller) TypeMetadata() SDKMetadataText {
	if caller.sdkObservation != nil {
		value, _ := caller.sdkObservation.value.Lookup("type")
		return SDKMetadataText{value: value}
	}
	return MetadataText(string(caller.kind))
}

func (caller ToolCaller) Validate() error {
	if caller.sdkObservation != nil {
		_, err := jsonvalue.AppendLegacy(nil, caller.sdkObservation.value)
		return err
	}
	switch caller.kind {
	case CallerDirect:
		if caller.toolID.Observation().Kind() != jsonvalue.Null {
			return errors.New("direct caller cannot carry a tool ID")
		}
	case CallerCodeExecution2025, CallerCodeExecution2026:
		if _, err := caller.toolID.MarshalJSON(); err != nil {
			return fmt.Errorf("invalid code execution caller tool ID: %w", err)
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
	if caller.sdkObservation != nil {
		return caller.sdkObservation.MarshalJSON()
	}
	if caller.kind == CallerDirect {
		return marshalMetadataObject(struct {
			Type CallerKind `json:"type"`
		}{Type: caller.kind}, caller.extra)
	}
	return marshalMetadataObject(struct {
		Type   CallerKind      `json:"type"`
		ToolID SDKMetadataText `json:"tool_id"`
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
	if !bounded {
		return decodeObservedToolCaller(data, false, false)
	}
	if len(data) > MaxWireBytes {
		return ToolCaller{}, errors.New("metadata JSON exceeds wire bound")
	}
	// Explicit JSON admission remains finite, including unknown extension fields.
	value, err := jsonvalue.Decode(string(data))
	if err != nil {
		return ToolCaller{}, err
	}
	if _, err := value.MarshalJSON(); err != nil {
		return ToolCaller{}, err
	}
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
			Type *CallerKind `json:"type"`
		}
		value, extra, err := decodeMetadataObject(data, &wire, []string{"type", "tool_id"}, bounded)
		if err != nil {
			return ToolCaller{}, err
		}
		if wire.Type == nil {
			return ToolCaller{}, errors.New("code execution caller requires type")
		}
		// Missing IDs normalize to null in the fixed SDK. Read the immutable
		// object directly so archival decoding does not reapply the wire cap.
		toolID, _ := value.Lookup("tool_id")
		next = ToolCaller{kind: *wire.Type, toolID: SDKMetadataText{value: toolID}, extra: extra}
	default:
		return ToolCaller{}, fmt.Errorf("unsupported caller kind %q", tag.Type)
	}
	if err := next.Validate(); err != nil {
		return ToolCaller{}, err
	}
	return next, nil
}
