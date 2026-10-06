package provider

import (
	"encoding/json"
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/luoyjx/mini-loop/go/protocol"
)

// Local ingress DTOs discard SDK-only response metadata, then normalize every
// consumed field into a closed domain variant. Raw bytes never reach a session.
func decodeReply(data []byte) (protocol.ModelReply, error) {
	if !utf8.Valid(data) {
		return protocol.ModelReply{}, errors.New("invalid UTF-8 response")
	}
	var wire struct {
		ID           *string              `json:"id"`
		Type         *protocol.ReplyKind  `json:"type"`
		Role         *protocol.Role       `json:"role"`
		Model        *string              `json:"model"`
		Content      *[]json.RawMessage   `json:"content"`
		StopReason   *protocol.StopReason `json:"stop_reason"`
		StopSequence *string              `json:"stop_sequence"`
		Usage        *struct {
			Input         *int                  `json:"input_tokens"`
			Output        *int                  `json:"output_tokens"`
			CacheRead     *int                  `json:"cache_read_input_tokens"`
			CacheCreation *int                  `json:"cache_creation_input_tokens"`
			Tier          *protocol.ServiceTier `json:"service_tier"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return protocol.ModelReply{}, errors.New("malformed JSON")
	}
	if wire.ID == nil || wire.Type == nil || wire.Role == nil || wire.Model == nil || wire.Content == nil || wire.StopReason == nil || wire.Usage == nil || wire.Usage.Input == nil || wire.Usage.Output == nil {
		return protocol.ModelReply{}, errors.New("missing completed-message fields")
	}
	reply := protocol.ModelReply{ID: *wire.ID, Type: *wire.Type, Role: *wire.Role, Model: *wire.Model, Content: []protocol.Block{}, StopReason: *wire.StopReason, StopSequence: wire.StopSequence, Usage: protocol.TokenUsage{InputTokens: *wire.Usage.Input, OutputTokens: *wire.Usage.Output, CacheReadInputTokens: wire.Usage.CacheRead, CacheCreationInputTokens: wire.Usage.CacheCreation, ServiceTier: wire.Usage.Tier}}
	for i, raw := range *wire.Content {
		block, err := decodeBlock(raw)
		if err != nil {
			return protocol.ModelReply{}, fmt.Errorf("content %d: %w", i, err)
		}
		reply.Content = append(reply.Content, block)
	}
	if err := reply.Validate(); err != nil {
		return protocol.ModelReply{}, err
	}
	return reply.Clone(), nil
}
func decodeBlock(raw []byte) (protocol.Block, error) {
	var wire struct {
		Type      protocol.BlockKind `json:"type"`
		Text      *string            `json:"text"`
		Thinking  *string            `json:"thinking"`
		Signature *string            `json:"signature"`
		Data      *string            `json:"data"`
		ID        *string            `json:"id"`
		Name      *protocol.ToolName `json:"name"`
		Input     json.RawMessage    `json:"input"`
		Caller    json.RawMessage    `json:"caller"`
		Citations json.RawMessage    `json:"citations"`
	}
	if len(raw) > protocol.MaxWireBytes {
		return protocol.Block{}, errors.New("block exceeds protocol limit")
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return protocol.Block{}, errors.New("malformed block")
	}
	switch wire.Type {
	case protocol.BlockRedactedThinking:
		if wire.Data == nil {
			return protocol.Block{}, errors.New("redacted thinking has no data")
		}
		return protocol.NewRedactedThinkingBlock(*wire.Data), nil
	case protocol.BlockText:
		if wire.Text == nil {
			return protocol.Block{}, errors.New("text block has no text")
		}
		if len(wire.Citations) > 0 {
			var citations []struct{}
			if json.Unmarshal(wire.Citations, &citations) != nil || len(citations) > 0 {
				return protocol.Block{}, errors.New("citation blocks require a typed citation adapter")
			}
		}
		return protocol.NewTextBlock(*wire.Text), nil
	case protocol.BlockThinking:
		if wire.Thinking == nil || wire.Signature == nil {
			return protocol.Block{}, errors.New("thinking block requires a signature")
		}
		b := protocol.NewThinkingBlock(*wire.Thinking, *wire.Signature)
		return b, b.Validate()
	case protocol.BlockToolUse:
		if wire.ID == nil || wire.Name == nil || len(wire.Input) == 0 {
			return protocol.Block{}, errors.New("tool_use fields missing")
		}
		input, err := protocol.DecodeToolInput(*wire.Name, wire.Input)
		if err != nil {
			return protocol.Block{}, err
		}
		var caller *protocol.ToolCaller
		if len(wire.Caller) > 0 && string(wire.Caller) != "null" {
			caller = &protocol.ToolCaller{}
			if err := json.Unmarshal(wire.Caller, caller); err != nil {
				return protocol.Block{}, err
			}
		}
		b := protocol.NewToolUseWithCaller(*wire.ID, input, caller)
		return b, b.Validate()
	default:
		return protocol.Block{}, fmt.Errorf("unsupported provider content type %q", wire.Type)
	}
}
