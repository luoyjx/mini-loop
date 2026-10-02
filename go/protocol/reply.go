package protocol

import (
	"encoding/json"
	"errors"
	"fmt"
)

type ReplyKind string

const ReplyMessage ReplyKind = "message"

type StopReason string

const (
	StopEndTurn   StopReason = "end_turn"
	StopToolUse   StopReason = "tool_use"
	StopMaxTokens StopReason = "max_tokens"
	StopSequence  StopReason = "stop_sequence"
	StopPauseTurn StopReason = "pause_turn"
	StopRefusal   StopReason = "refusal"
)

func (reason StopReason) Known() bool {
	switch reason {
	case StopEndTurn, StopToolUse, StopMaxTokens, StopSequence, StopPauseTurn, StopRefusal:
		return true
	default:
		return false
	}
}

func (reason StopReason) Resumable() bool { return reason == StopPauseTurn }

type ServiceTier string

// TokenUsage preserves absent cache fields separately from explicit zeroes.
// The final provider response always reports input and output token counts.
type TokenUsage struct {
	InputTokens              int          `json:"input_tokens"`
	OutputTokens             int          `json:"output_tokens"`
	CacheReadInputTokens     *int         `json:"cache_read_input_tokens,omitempty"`
	CacheCreationInputTokens *int         `json:"cache_creation_input_tokens,omitempty"`
	ServiceTier              *ServiceTier `json:"service_tier,omitempty"`
}

func (usage TokenUsage) Validate() error {
	if usage.InputTokens < 0 || usage.OutputTokens < 0 ||
		(usage.CacheReadInputTokens != nil && *usage.CacheReadInputTokens < 0) ||
		(usage.CacheCreationInputTokens != nil && *usage.CacheCreationInputTokens < 0) {
		return errors.New("token counts must be non-negative")
	}
	return nil
}

func (usage TokenUsage) clone() TokenUsage {
	if usage.CacheReadInputTokens != nil {
		value := *usage.CacheReadInputTokens
		usage.CacheReadInputTokens = &value
	}
	if usage.CacheCreationInputTokens != nil {
		value := *usage.CacheCreationInputTokens
		usage.CacheCreationInputTokens = &value
	}
	if usage.ServiceTier != nil {
		value := *usage.ServiceTier
		usage.ServiceTier = &value
	}
	return usage
}

func (usage *TokenUsage) UnmarshalJSON(data []byte) error {
	var wire struct {
		InputTokens              *int         `json:"input_tokens"`
		OutputTokens             *int         `json:"output_tokens"`
		CacheReadInputTokens     *int         `json:"cache_read_input_tokens"`
		CacheCreationInputTokens *int         `json:"cache_creation_input_tokens"`
		ServiceTier              *ServiceTier `json:"service_tier"`
	}
	if err := decodeStrict(data, &wire); err != nil {
		return err
	}
	if wire.InputTokens == nil || wire.OutputTokens == nil {
		return errors.New("usage requires input_tokens and output_tokens")
	}
	next := TokenUsage{
		InputTokens: *wire.InputTokens, OutputTokens: *wire.OutputTokens,
		CacheReadInputTokens:     wire.CacheReadInputTokens,
		CacheCreationInputTokens: wire.CacheCreationInputTokens,
		ServiceTier:              wire.ServiceTier,
	}
	if err := next.Validate(); err != nil {
		return err
	}
	*usage = next
	return nil
}

// ModelReply is the completed provider response. An empty Content slice is
// valid for a refusal; request transcript messages retain their own rules.
type ModelReply struct {
	ID           string     `json:"id"`
	Type         ReplyKind  `json:"type"`
	Role         Role       `json:"role"`
	Model        string     `json:"model"`
	Content      []Block    `json:"content"`
	StopReason   StopReason `json:"stop_reason"`
	StopSequence *string    `json:"stop_sequence"`
	Usage        TokenUsage `json:"usage"`
}

func (reply ModelReply) Validate() error {
	if reply.ID == "" || reply.Model == "" || reply.Type != ReplyMessage || reply.Role != RoleAssistant || reply.StopReason == "" || reply.Content == nil {
		return errors.New("model reply requires id, message type, assistant role, model, content, and stop reason")
	}
	if err := reply.Usage.Validate(); err != nil {
		return fmt.Errorf("usage: %w", err)
	}
	for i, block := range reply.Content {
		if err := block.Validate(); err != nil {
			return fmt.Errorf("content.%d: %w", i, err)
		}
	}
	return nil
}

func (reply ModelReply) Clone() ModelReply {
	reply.Content = append([]Block{}, reply.Content...)
	if reply.StopSequence != nil {
		value := *reply.StopSequence
		reply.StopSequence = &value
	}
	reply.Usage = reply.Usage.clone()
	return reply
}

func (reply ModelReply) MarshalJSON() ([]byte, error) {
	if err := reply.Validate(); err != nil {
		return nil, err
	}
	type wire ModelReply
	return json.Marshal(wire(reply))
}

func (reply *ModelReply) UnmarshalJSON(data []byte) error {
	var wire struct {
		ID           *string     `json:"id"`
		Type         *ReplyKind  `json:"type"`
		Role         *Role       `json:"role"`
		Model        *string     `json:"model"`
		Content      *[]Block    `json:"content"`
		StopReason   *StopReason `json:"stop_reason"`
		StopSequence *string     `json:"stop_sequence"`
		Usage        *TokenUsage `json:"usage"`
	}
	if err := decodeStrict(data, &wire); err != nil {
		return err
	}
	if wire.ID == nil || wire.Type == nil || wire.Role == nil || wire.Model == nil || wire.Content == nil || wire.StopReason == nil || wire.Usage == nil {
		return errors.New("model reply is missing a required field")
	}
	next := ModelReply{
		ID: *wire.ID, Type: *wire.Type, Role: *wire.Role, Model: *wire.Model,
		Content: *wire.Content, StopReason: *wire.StopReason,
		StopSequence: wire.StopSequence, Usage: *wire.Usage,
	}
	if err := next.Validate(); err != nil {
		return err
	}
	*reply = next.Clone()
	return nil
}
