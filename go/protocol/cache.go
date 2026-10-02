package protocol

import (
	"encoding/json"
	"errors"
)

type CacheTTL string
type CacheType string

const CacheEphemeral CacheType = "ephemeral"

type CacheControl struct {
	Type CacheType `json:"type"`
	TTL  CacheTTL  `json:"ttl,omitempty"`
}

func (control CacheControl) Validate() error {
	if control.Type != CacheEphemeral {
		return errors.New("unsupported cache control type")
	}
	return nil
}

type CacheBreakpoint struct {
	MessageIndex int
	BlockIndex   int
	Control      CacheControl
}

// Annotations belong to one detached request, never to the live transcript.
type CacheAnnotations struct {
	System   *CacheControl
	Messages []CacheBreakpoint
}

func (annotations CacheAnnotations) Clone() CacheAnnotations {
	if annotations.System != nil {
		control := *annotations.System
		annotations.System = &control
	}
	annotations.Messages = append([]CacheBreakpoint(nil), annotations.Messages...)
	return annotations
}
func (annotations CacheAnnotations) Validate(request ModelRequest) error {
	if annotations.System != nil {
		if request.System == nil || *request.System == "" {
			return errors.New("cache breakpoint requires nonempty system")
		}
		if err := annotations.System.Validate(); err != nil {
			return err
		}
	}
	seen := make(map[[2]int]bool)
	for _, target := range annotations.Messages {
		if err := target.Control.Validate(); err != nil {
			return err
		}
		if target.MessageIndex < 0 || target.MessageIndex >= len(request.Messages) {
			return errors.New("cache message index out of range")
		}
		message := request.Messages[target.MessageIndex]
		blocks, ok := message.Content.Blocks()
		if message.Role != RoleUser || !ok || target.BlockIndex < 0 || target.BlockIndex >= len(blocks) {
			return errors.New("cache breakpoint requires a user content block")
		}
		key := [2]int{target.MessageIndex, target.BlockIndex}
		if seen[key] {
			return errors.New("duplicate cache breakpoint")
		}
		seen[key] = true
	}
	return nil
}

type WireSystem struct {
	plain   string
	control *CacheControl
}

func (system WireSystem) MarshalJSON() ([]byte, error) {
	if system.control == nil {
		return json.Marshal(system.plain)
	}
	return json.Marshal([]struct {
		Type         BlockKind     `json:"type"`
		Text         string        `json:"text"`
		CacheControl *CacheControl `json:"cache_control"`
	}{{BlockText, system.plain, system.control}})
}

type WireBlock struct {
	block   Block
	control *CacheControl
}

func (block WireBlock) MarshalJSON() ([]byte, error) {
	return block.block.marshalWithCache(block.control)
}

type WireContent struct {
	content  Content
	controls map[int]CacheControl
}

func (content WireContent) MarshalJSON() ([]byte, error) {
	if text, ok := content.content.Plain(); ok {
		return json.Marshal(text)
	}
	blocks, ok := content.content.Blocks()
	if !ok {
		return nil, errors.New("invalid wire content")
	}
	wire := make([]WireBlock, len(blocks))
	for i, block := range blocks {
		wire[i].block = block
		if control, ok := content.controls[i]; ok {
			copy := control
			wire[i].control = &copy
		}
	}
	return json.Marshal(wire)
}

type WireMessage struct {
	Role    Role        `json:"role"`
	Content WireContent `json:"content"`
}

// WireRequest contains concrete transport variants; Purpose is local only.
type WireRequest struct {
	Model     string        `json:"model"`
	MaxTokens int           `json:"max_tokens"`
	Messages  []WireMessage `json:"messages"`
	System    *WireSystem   `json:"system,omitempty"`
	Tools     *[]ToolSchema `json:"tools,omitempty"`
}

func (request ModelRequest) Wire() (WireRequest, error) {
	if err := request.Cache.Validate(request); err != nil {
		return WireRequest{}, err
	}
	wire := WireRequest{Model: request.Model, MaxTokens: request.MaxTokens, Messages: make([]WireMessage, len(request.Messages))}
	if request.System != nil {
		wire.System = &WireSystem{plain: *request.System}
		if request.Cache.System != nil {
			control := *request.Cache.System
			wire.System.control = &control
		}
	}
	if request.Tools != nil {
		tools := make([]ToolSchema, len(request.Tools))
		for i, tool := range request.Tools {
			tools[i] = tool.Clone()
		}
		wire.Tools = &tools
	}
	for i, message := range request.Messages {
		wire.Messages[i] = WireMessage{message.Role, WireContent{content: message.Content, controls: make(map[int]CacheControl)}}
	}
	for _, target := range request.Cache.Messages {
		wire.Messages[target.MessageIndex].Content.controls[target.BlockIndex] = target.Control
	}
	return wire, nil
}
