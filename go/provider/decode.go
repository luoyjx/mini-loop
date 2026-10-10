package provider

import (
	"encoding/json"
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/luoyjx/mini-loop/go/protocol"
)

// Local ingress DTOs preserve named usage and discard unconsumed message metadata.
// Normalize every
// consumed field into a closed domain variant. Raw bytes never reach a session.
func decodeReply(data []byte) (protocol.ModelReply, error) {
	if !json.Valid(data) && utf8.Valid(data) {
		return decodeLegacySDKReply(data)
	}
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
		Usage        *protocol.TokenUsage `json:"usage"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return protocol.ModelReply{}, errors.New("malformed JSON")
	}
	if wire.ID == nil || wire.Type == nil || wire.Role == nil || wire.Model == nil || wire.Content == nil || wire.StopReason == nil || wire.Usage == nil {
		return protocol.ModelReply{}, errors.New("missing completed-message fields")
	}
	reply := protocol.ModelReply{ID: *wire.ID, Type: *wire.Type, Role: *wire.Role, Model: *wire.Model, Content: []protocol.Block{}, StopReason: *wire.StopReason, StopSequence: wire.StopSequence, Usage: wire.Usage.WithSDKDirectCounts()}
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
		FileID    *string            `json:"file_id"`
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
	case protocol.BlockTextEditorToolResult:
		var result protocol.TextEditorToolResultBlock
		if err := json.Unmarshal(raw, &result); err != nil {
			return protocol.Block{}, err
		}
		result = result.WithSDKDefaults(false)
		encoded, err := json.Marshal(result)
		if err != nil {
			return protocol.Block{}, err
		}
		if len(encoded) > protocol.MaxWireBytes {
			return protocol.Block{}, errors.New("normalized text editor block exceeds protocol limit")
		}
		return result.Block(), nil
	case protocol.BlockBashExecutionToolResult:
		var result protocol.BashExecutionToolResultBlock
		if err := json.Unmarshal(raw, &result); err != nil {
			return protocol.Block{}, err
		}
		result = result.WithSDKDefaults(false)
		encoded, err := json.Marshal(result)
		if err != nil {
			return protocol.Block{}, err
		}
		if len(encoded) > protocol.MaxWireBytes {
			return protocol.Block{}, errors.New("normalized bash execution block exceeds protocol limit")
		}
		return result.Block(), nil
	case protocol.BlockCodeExecutionToolResult:
		var result protocol.CodeExecutionToolResultBlock
		if err := json.Unmarshal(raw, &result); err != nil {
			return protocol.Block{}, err
		}
		result = result.WithSDKDefaults(false)
		encoded, err := json.Marshal(result)
		if err != nil {
			return protocol.Block{}, err
		}
		if len(encoded) > protocol.MaxWireBytes {
			return protocol.Block{}, errors.New("normalized code execution block exceeds protocol limit")
		}
		return result.Block(), nil
	case protocol.BlockWebFetchToolResult:
		var result protocol.WebFetchToolResultBlock
		if err := json.Unmarshal(raw, &result); err != nil {
			return protocol.Block{}, err
		}
		result = result.WithSDKDefaults(false)
		encoded, err := json.Marshal(result)
		if err != nil {
			return protocol.Block{}, err
		}
		if len(encoded) > protocol.MaxWireBytes {
			return protocol.Block{}, errors.New("normalized web fetch block exceeds protocol limit")
		}
		return result.Block(), nil
	case protocol.BlockServerToolUse:
		var result protocol.ServerToolUseBlock
		if err := json.Unmarshal(raw, &result); err != nil {
			return protocol.Block{}, err
		}
		result = result.WithSDKDefaults()
		encoded, err := json.Marshal(result)
		if err != nil {
			return protocol.Block{}, err
		}
		if len(encoded) > protocol.MaxWireBytes {
			return protocol.Block{}, errors.New("normalized server tool use exceeds protocol limit")
		}
		return result.Block(), nil
	case protocol.BlockToolSearchToolResult:
		var result protocol.ToolSearchToolResultBlock
		if err := json.Unmarshal(raw, &result); err != nil {
			return protocol.Block{}, err
		}
		result = result.WithSDKDefaults(false)
		encoded, err := json.Marshal(result)
		if err != nil {
			return protocol.Block{}, err
		}
		if len(encoded) > protocol.MaxWireBytes {
			return protocol.Block{}, errors.New("normalized tool search block exceeds protocol limit")
		}
		return result.Block(), nil
	case protocol.BlockWebSearchToolResult:
		var result protocol.WebSearchToolResultBlock
		if err := json.Unmarshal(raw, &result); err != nil {
			return protocol.Block{}, err
		}
		result = result.WithSDKDefaults()
		encoded, err := json.Marshal(result)
		if err != nil {
			return protocol.Block{}, err
		}
		if len(encoded) > protocol.MaxWireBytes {
			return protocol.Block{}, errors.New("normalized web search block exceeds protocol limit")
		}
		return result.Block(), nil
	case protocol.BlockContainerUpload:
		if wire.FileID == nil {
			return protocol.Block{}, errors.New("container upload has no file_id")
		}
		return protocol.NewContainerUploadBlock(*wire.FileID), nil
	case protocol.BlockRedactedThinking:
		if wire.Data == nil {
			return protocol.Block{}, errors.New("redacted thinking has no data")
		}
		return protocol.NewRedactedThinkingBlock(*wire.Data), nil
	case protocol.BlockText:
		if wire.Text == nil {
			return protocol.Block{}, errors.New("text block has no text")
		}
		citations, err := providerCitations(wire.Citations)
		if err != nil {
			return protocol.Block{}, err
		}
		block := protocol.NewTextBlockWithCitations(*wire.Text, citations)
		encoded, err := json.Marshal(block)
		if err != nil {
			return protocol.Block{}, err
		}
		if len(encoded) > protocol.MaxWireBytes {
			return protocol.Block{}, errors.New("normalized citation block exceeds protocol limit")
		}
		return block, nil
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
		input, err := protocol.DecodeModelToolInput(*wire.Name, wire.Input)
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
