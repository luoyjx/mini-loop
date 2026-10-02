// Package protocol defines the typed portion of the model transcript that the
// Go port currently accepts. Wire decoding rejects unknown block and tool
// variants until they have explicit domain types and execution policies.
package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

type BlockKind string

const (
	BlockText       BlockKind = "text"
	BlockThinking   BlockKind = "thinking"
	BlockToolUse    BlockKind = "tool_use"
	BlockToolResult BlockKind = "tool_result"
)

type ToolName string

const ToolBash ToolName = "bash"

const MaxWireBytes = 512 * 1024

type TextBlock struct {
	Text string `json:"text"`
}

type ThinkingBlock struct {
	Thinking  string `json:"thinking"`
	Signature string `json:"signature"`
}

type ToolUseBlock struct {
	ID     string      `json:"id"`
	Name   ToolName    `json:"name"`
	Input  ToolInput   `json:"input"`
	Caller *ToolCaller `json:"caller"`
}

type ToolResultBlock struct {
	ToolUseID string `json:"tool_use_id"`
	Content   string `json:"content"`
	IsError   bool   `json:"is_error,omitempty"`
}

// Block is a closed tagged union. Exactly one variant is populated.
type Block struct {
	kind       BlockKind
	text       *TextBlock
	thinking   *ThinkingBlock
	toolUse    *ToolUseBlock
	toolResult *ToolResultBlock
}

func NewTextBlock(text string) Block {
	return Block{kind: BlockText, text: &TextBlock{Text: text}}
}

func NewThinkingBlock(thinking, signature string) Block {
	return Block{kind: BlockThinking, thinking: &ThinkingBlock{Thinking: thinking, Signature: signature}}
}

func NewBashUse(id, command string) Block {
	return NewToolUse(id, BashToolInput(BashInput{Command: command}))
}

func NewToolUse(id string, input ToolInput) Block {
	return NewToolUseWithCaller(id, input, nil)
}

func NewToolUseWithCaller(id string, input ToolInput, caller *ToolCaller) Block {
	var detached *ToolCaller
	if caller != nil {
		copyOf := *caller
		detached = &copyOf
	}
	return Block{kind: BlockToolUse, toolUse: &ToolUseBlock{ID: id, Name: input.Name(), Input: input.clone(), Caller: detached}}
}

func NewToolResult(id, content string, isError bool) Block {
	return Block{kind: BlockToolResult, toolResult: &ToolResultBlock{ToolUseID: id, Content: content, IsError: isError}}
}

func (b Block) Kind() BlockKind { return b.kind }

func (b Block) Text() (TextBlock, bool) {
	if b.text == nil || b.kind != BlockText {
		return TextBlock{}, false
	}
	return *b.text, true
}

func (b Block) ToolUse() (ToolUseBlock, bool) {
	if b.toolUse == nil || b.kind != BlockToolUse {
		return ToolUseBlock{}, false
	}
	copyOf := *b.toolUse
	copyOf.Input = copyOf.Input.clone()
	if copyOf.Caller != nil {
		caller := *copyOf.Caller
		copyOf.Caller = &caller
	}
	return copyOf, true
}

func (b Block) ToolResult() (ToolResultBlock, bool) {
	if b.toolResult == nil || b.kind != BlockToolResult {
		return ToolResultBlock{}, false
	}
	return *b.toolResult, true
}

func (b Block) Validate() error {
	switch b.kind {
	case BlockText:
		if b.text == nil || b.thinking != nil || b.toolUse != nil || b.toolResult != nil {
			return errors.New("text block has invalid variant fields")
		}
	case BlockThinking:
		if b.thinking == nil || b.text != nil || b.toolUse != nil || b.toolResult != nil || b.thinking.Signature == "" {
			return errors.New("thinking block requires a signature and no other variant")
		}
	case BlockToolUse:
		if b.toolUse == nil || b.text != nil || b.thinking != nil || b.toolResult != nil || b.toolUse.ID == "" || b.toolUse.Name != b.toolUse.Input.Name() || b.toolUse.Input.Validate() != nil || (b.toolUse.Caller != nil && b.toolUse.Caller.Validate() != nil) {
			return errors.New("tool_use requires a supported named tool and id")
		}
	case BlockToolResult:
		if b.toolResult == nil || b.text != nil || b.thinking != nil || b.toolUse != nil || b.toolResult.ToolUseID == "" {
			return errors.New("tool_result requires a tool_use_id")
		}
	default:
		return fmt.Errorf("unsupported block type %q", b.kind)
	}
	return nil
}

func (b Block) MarshalJSON() ([]byte, error) { return b.marshalWithCache(nil) }
func (b Block) marshalWithCache(control *CacheControl) ([]byte, error) {
	if control != nil {
		if err := control.Validate(); err != nil {
			return nil, err
		}
	}
	if err := b.Validate(); err != nil {
		return nil, err
	}
	switch b.kind {
	case BlockText:
		return json.Marshal(struct {
			Type BlockKind `json:"type"`
			TextBlock
			CacheControl *CacheControl `json:"cache_control,omitempty"`
		}{BlockText, *b.text, control})
	case BlockThinking:
		return json.Marshal(struct {
			Type BlockKind `json:"type"`
			ThinkingBlock
			CacheControl *CacheControl `json:"cache_control,omitempty"`
		}{BlockThinking, *b.thinking, control})
	case BlockToolUse:
		return json.Marshal(struct {
			Type BlockKind `json:"type"`
			ToolUseBlock
			CacheControl *CacheControl `json:"cache_control,omitempty"`
		}{BlockToolUse, *b.toolUse, control})
	case BlockToolResult:
		return json.Marshal(struct {
			Type BlockKind `json:"type"`
			ToolResultBlock
			CacheControl *CacheControl `json:"cache_control,omitempty"`
		}{BlockToolResult, *b.toolResult, control})
	default:
		return nil, fmt.Errorf("unsupported block type %q", b.kind)
	}
}

// decodeStrict is used only at a JSON boundary. Domain values never retain
// raw JSON or an open-ended map.
func decodeStrict[T any](data []byte, target *T) error {
	if len(data) > MaxWireBytes {
		return fmt.Errorf("JSON value exceeds %d bytes", MaxWireBytes)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(new(struct{})); err != io.EOF {
		if err == nil {
			return errors.New("trailing JSON value")
		}
		return err
	}
	return nil
}

func (b *Block) UnmarshalJSON(data []byte) error {
	if len(data) > MaxWireBytes {
		return fmt.Errorf("block exceeds %d bytes", MaxWireBytes)
	}
	var tag struct {
		Type BlockKind `json:"type"`
	}
	if err := json.Unmarshal(data, &tag); err != nil {
		return err
	}
	var next Block
	switch tag.Type {
	case BlockText:
		var wire struct {
			Type BlockKind `json:"type"`
			TextBlock
		}
		if err := decodeStrict(data, &wire); err != nil {
			return err
		}
		next = NewTextBlock(wire.Text)
	case BlockThinking:
		var wire struct {
			Type BlockKind `json:"type"`
			ThinkingBlock
		}
		if err := decodeStrict(data, &wire); err != nil {
			return err
		}
		next = NewThinkingBlock(wire.Thinking, wire.Signature)
	case BlockToolUse:
		var wire struct {
			Type   BlockKind       `json:"type"`
			ID     string          `json:"id"`
			Name   ToolName        `json:"name"`
			Input  json.RawMessage `json:"input"`
			Caller *ToolCaller     `json:"caller"`
		}
		if err := decodeStrict(data, &wire); err != nil {
			return err
		}
		if len(wire.Input) == 0 {
			return errors.New("tool_use requires input")
		}
		input, err := DecodeToolInput(wire.Name, wire.Input)
		if err != nil {
			return err
		}
		next = NewToolUseWithCaller(wire.ID, input, wire.Caller)
	case BlockToolResult:
		var wire struct {
			Type BlockKind `json:"type"`
			ToolResultBlock
		}
		if err := decodeStrict(data, &wire); err != nil {
			return err
		}
		next = NewToolResult(wire.ToolUseID, wire.Content, wire.IsError)
	default:
		return fmt.Errorf("unsupported block type %q", tag.Type)
	}
	if err := next.Validate(); err != nil {
		return err
	}
	*b = next
	return nil
}

// Content is the provider's string-or-block-array union. An empty string is
// allowed; an empty block array is not.
type Content struct {
	plain  *string
	blocks []Block
}

func PlainContent(text string) Content { return Content{plain: &text} }

func BlockContent(blocks ...Block) Content {
	copyOf := append([]Block(nil), blocks...)
	return Content{blocks: copyOf}
}

func (c Content) Plain() (string, bool) {
	if c.plain == nil {
		return "", false
	}
	return *c.plain, true
}

func (c Content) Blocks() ([]Block, bool) {
	if c.plain != nil || len(c.blocks) == 0 {
		return nil, false
	}
	return append([]Block(nil), c.blocks...), true
}

func (c Content) Validate() error {
	if c.plain != nil {
		if len(c.blocks) != 0 {
			return errors.New("content cannot hold both text and blocks")
		}
		return nil
	}
	if len(c.blocks) == 0 {
		return errors.New("block content must be non-empty")
	}
	for i, block := range c.blocks {
		if err := block.Validate(); err != nil {
			return fmt.Errorf("content.%d: %w", i, err)
		}
	}
	return nil
}

func (c Content) MarshalJSON() ([]byte, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	if c.plain != nil {
		return json.Marshal(*c.plain)
	}
	return json.Marshal(c.blocks)
}

func (c *Content) UnmarshalJSON(data []byte) error {
	if len(data) > MaxWireBytes {
		return fmt.Errorf("content exceeds %d bytes", MaxWireBytes)
	}
	var next Content
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return errors.New("missing content")
	}
	switch trimmed[0] {
	case '"':
		var plain string
		if err := decodeStrict(data, &plain); err != nil {
			return err
		}
		next = PlainContent(plain)
	case '[':
		var blocks []Block
		if err := decodeStrict(data, &blocks); err != nil {
			return err
		}
		next = BlockContent(blocks...)
	default:
		return errors.New("content must be a string or block array")
	}
	if err := next.Validate(); err != nil {
		return err
	}
	*c = next
	return nil
}

type Message struct {
	Role    Role    `json:"role"`
	Content Content `json:"content"`
}

func (m Message) Validate() error {
	if m.Role != RoleUser && m.Role != RoleAssistant {
		return fmt.Errorf("unsupported role %q", m.Role)
	}
	return m.Content.Validate()
}

func (m Message) MarshalJSON() ([]byte, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		Role    Role    `json:"role"`
		Content Content `json:"content"`
	}{Role: m.Role, Content: m.Content})
}

func (m *Message) UnmarshalJSON(data []byte) error {
	if len(data) > MaxWireBytes {
		return fmt.Errorf("message exceeds %d bytes", MaxWireBytes)
	}
	var wire struct {
		Role    Role    `json:"role"`
		Content Content `json:"content"`
	}
	if err := decodeStrict(data, &wire); err != nil {
		return err
	}
	next := Message{Role: wire.Role, Content: wire.Content}
	if err := next.Validate(); err != nil {
		return err
	}
	*m = next
	return nil
}

// ValidateTranscript enforces the immediate tool_use/tool_result pairing
// observed by the Python fake provider and the compatible live endpoint.
func ValidateTranscript(messages []Message) error {
	if len(messages) == 0 {
		return errors.New("messages: at least one message is required")
	}
	for i, message := range messages {
		if err := message.Validate(); err != nil {
			return fmt.Errorf("messages.%d: %w", i, err)
		}
		uses := idsOfKind(message, BlockToolUse)
		var following []string
		if i+1 < len(messages) {
			following = idsOfKind(messages[i+1], BlockToolResult)
		}
		for _, id := range uses {
			if !contains(following, id) {
				return fmt.Errorf("messages.%d: tool_use %q has no immediately following tool_result", i, id)
			}
		}
		var offered []string
		if i > 0 {
			offered = idsOfKind(messages[i-1], BlockToolUse)
		}
		for _, id := range idsOfKind(message, BlockToolResult) {
			if !contains(offered, id) {
				return fmt.Errorf("messages.%d: unexpected tool_result %q", i, id)
			}
		}
	}
	return nil
}

func idsOfKind(message Message, kind BlockKind) []string {
	blocks, ok := message.Content.Blocks()
	if !ok {
		return nil
	}
	ids := make([]string, 0)
	for _, block := range blocks {
		switch kind {
		case BlockToolUse:
			if use, ok := block.ToolUse(); ok {
				ids = append(ids, use.ID)
			}
		case BlockToolResult:
			if result, ok := block.ToolResult(); ok {
				ids = append(ids, result.ToolUseID)
			}
		}
	}
	return ids
}

func contains(ids []string, id string) bool {
	for _, candidate := range ids {
		if candidate == id {
			return true
		}
	}
	return false
}
