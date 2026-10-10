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
	BlockText                    BlockKind = "text"
	BlockThinking                BlockKind = "thinking"
	BlockRedactedThinking        BlockKind = "redacted_thinking"
	BlockContainerUpload         BlockKind = "container_upload"
	BlockWebSearchToolResult     BlockKind = "web_search_tool_result"
	BlockToolSearchToolResult    BlockKind = "tool_search_tool_result"
	BlockToolUse                 BlockKind = "tool_use"
	BlockServerToolUse           BlockKind = "server_tool_use"
	BlockWebFetchToolResult      BlockKind = "web_fetch_tool_result"
	BlockCodeExecutionToolResult BlockKind = "code_execution_tool_result"
	BlockBashExecutionToolResult BlockKind = "bash_code_execution_tool_result"
	BlockTextEditorToolResult    BlockKind = "text_editor_code_execution_tool_result"
	BlockToolResult              BlockKind = "tool_result"
)

type ToolName string

const ToolBash ToolName = "bash"

const MaxWireBytes = 512 * 1024

type TextBlock struct {
	Text             string `json:"text"`
	citations        Citations
	parsedOutputNull bool
}

type ThinkingBlock struct {
	Thinking  string `json:"thinking"`
	Signature string `json:"signature"`
}
type ContainerUploadBlock struct {
	FileID                                    string `json:"file_id"`
	textNull, citationsNull, parsedOutputNull bool
}

type RedactedThinkingBlock struct {
	Data string `json:"data"`
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
	callerPresent    bool
	kind             BlockKind
	text             *TextBlock
	thinking         *ThinkingBlock
	redactedThinking *RedactedThinkingBlock
	containerUpload  *ContainerUploadBlock
	webSearch        *WebSearchToolResultBlock
	toolSearch       *ToolSearchToolResultBlock
	serverTool       *ServerToolUseBlock
	webFetch         *WebFetchToolResultBlock
	codeExecution    *CodeExecutionToolResultBlock
	bashExecution    *BashExecutionToolResultBlock
	textEditor       *TextEditorToolResultBlock
	toolUse          *ToolUseBlock
	toolResult       *ToolResultBlock
}

func NewTextBlock(text string) Block {
	return NewTextBlockWithCitations(text, Citations{})
}

func NewTextBlockWithCitations(text string, citations Citations) Block {
	return Block{kind: BlockText, text: &TextBlock{Text: text, citations: citations}}
}

// NewStreamedTextBlock retains the default SDK ParsedTextBlock's null output.
// Selecting a structured output format is a separate unsupported request profile.
func NewStreamedTextBlock(text string, citations Citations) Block {
	block := NewTextBlockWithCitations(text, citations)
	block.text.parsedOutputNull = true
	return block
}

func (text TextBlock) Citations() Citations   { return text.citations }
func (text TextBlock) ParsedOutputNull() bool { return text.parsedOutputNull }

// WithText replaces only text, retaining immutable provider metadata.
func (text TextBlock) WithText(value string) Block {
	text.Text = value
	return Block{kind: BlockText, text: &text}
}
func (text TextBlock) parsedOutputWire() *wireOptional[string] {
	if !text.parsedOutputNull {
		return nil
	}
	return &wireOptional[string]{}
}
func (text TextBlock) citationWire() *Citations {
	if !text.citations.Present() {
		return nil
	}
	value := text.citations
	return &value
}
func (text TextBlock) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Text         string                `json:"text"`
		Citations    *Citations            `json:"citations,omitempty"`
		ParsedOutput *wireOptional[string] `json:"parsed_output,omitempty"`
	}{text.Text, text.citationWire(), text.parsedOutputWire()})
}
func (text *TextBlock) UnmarshalJSON(data []byte) error {
	var wire struct {
		Text         string          `json:"text"`
		Citations    json.RawMessage `json:"citations"`
		ParsedOutput json.RawMessage `json:"parsed_output"`
	}
	if err := decodeStrict(data, &wire); err != nil {
		return err
	}
	citations, err := DecodeCitations(wire.Citations)
	if err != nil {
		return err
	}
	if len(wire.ParsedOutput) > 0 && string(bytes.TrimSpace(wire.ParsedOutput)) != "null" {
		return errors.New("structured parsed output is unsupported")
	}
	*text = TextBlock{Text: wire.Text, citations: citations, parsedOutputNull: len(wire.ParsedOutput) > 0}
	return nil
}

func NewThinkingBlock(thinking, signature string) Block {
	return Block{kind: BlockThinking, thinking: &ThinkingBlock{Thinking: thinking, Signature: signature}}
}
func NewContainerUploadBlock(fileID string) Block {
	return Block{kind: BlockContainerUpload, containerUpload: &ContainerUploadBlock{FileID: fileID}}
}

// NewStreamedContainerUploadBlock retains the SDK stream union's null text defaults.
func NewStreamedContainerUploadBlock(fileID string) Block {
	b := NewContainerUploadBlock(fileID)
	b.containerUpload.textNull, b.containerUpload.citationsNull, b.containerUpload.parsedOutputNull = true, true, true
	return b
}
func (b Block) ContainerUpload() (ContainerUploadBlock, bool) {
	if b.kind != BlockContainerUpload || b.containerUpload == nil {
		return ContainerUploadBlock{}, false
	}
	return *b.containerUpload, true
}

func NewRedactedThinkingBlock(data string) Block {
	return Block{kind: BlockRedactedThinking, redactedThinking: &RedactedThinkingBlock{Data: data}}
}
func (b Block) RedactedThinking() (RedactedThinkingBlock, bool) {
	if b.kind != BlockRedactedThinking || b.redactedThinking == nil {
		return RedactedThinkingBlock{}, false
	}
	return *b.redactedThinking, true
}

func NewBashUse(id, command string) Block {
	return NewToolUse(id, BashToolInput(BashInput{Command: command}))
}

func NewToolUse(id string, input ToolInput) Block {
	return NewToolUseWithCaller(id, input, nil)
}

// NewToolUseWithoutCaller preserves a provider block that omits caller.
// NewToolUseWithCaller(..., nil) retains an explicit JSON null.
func NewToolUseWithoutCaller(id string, input ToolInput) Block {
	block := NewToolUseWithCaller(id, input, nil)
	block.callerPresent = false
	return block
}

func NewToolUseWithCaller(id string, input ToolInput, caller *ToolCaller) Block {
	var detached *ToolCaller
	if caller != nil {
		copyOf := *caller
		detached = &copyOf
	}
	return Block{callerPresent: true, kind: BlockToolUse, toolUse: &ToolUseBlock{ID: id, Name: input.Name(), Input: input.clone(), Caller: detached}}
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

func (b Block) Thinking() (ThinkingBlock, bool) {
	if b.thinking == nil {
		return ThinkingBlock{}, false
	}
	return *b.thinking, true
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
	if b.kind != BlockTextEditorToolResult && b.textEditor != nil {
		return errors.New("block has an unexpected text editor variant")
	}
	if b.kind == BlockTextEditorToolResult {
		if b.textEditor == nil || b.bashExecution != nil || b.codeExecution != nil || b.webFetch != nil || b.serverTool != nil || b.toolSearch != nil || b.webSearch != nil || b.containerUpload != nil || b.redactedThinking != nil || b.text != nil || b.thinking != nil || b.toolUse != nil || b.toolResult != nil {
			return errors.New("text editor block requires one variant")
		}
		return b.textEditor.Validate()
	}

	if b.kind != BlockBashExecutionToolResult && b.bashExecution != nil {
		return errors.New("block has an unexpected bash execution variant")
	}
	if b.kind == BlockBashExecutionToolResult {
		if b.bashExecution == nil || b.codeExecution != nil || b.webFetch != nil || b.serverTool != nil || b.toolSearch != nil || b.webSearch != nil || b.containerUpload != nil || b.redactedThinking != nil || b.text != nil || b.thinking != nil || b.toolUse != nil || b.toolResult != nil {
			return errors.New("bash execution block requires one variant")
		}
		return b.bashExecution.Validate()
	}

	if b.kind != BlockCodeExecutionToolResult && b.codeExecution != nil {
		return errors.New("block has an unexpected code execution variant")
	}
	if b.kind == BlockCodeExecutionToolResult {
		if b.codeExecution == nil || b.webFetch != nil || b.serverTool != nil || b.toolSearch != nil || b.webSearch != nil || b.containerUpload != nil || b.redactedThinking != nil || b.text != nil || b.thinking != nil || b.toolUse != nil || b.toolResult != nil {
			return errors.New("code execution block requires one variant")
		}
		return b.codeExecution.Validate()
	}

	if b.kind != BlockWebFetchToolResult && b.webFetch != nil {
		return errors.New("block has an unexpected web fetch variant")
	}
	if b.kind == BlockWebFetchToolResult {
		if b.webFetch == nil || b.serverTool != nil || b.toolSearch != nil || b.webSearch != nil || b.containerUpload != nil || b.redactedThinking != nil || b.text != nil || b.thinking != nil || b.toolUse != nil || b.toolResult != nil {
			return errors.New("web fetch block requires one variant")
		}
		return b.webFetch.Validate()
	}

	if b.kind != BlockServerToolUse && b.serverTool != nil {
		return errors.New("block has an unexpected server tool use variant")
	}
	if b.kind == BlockServerToolUse {
		if b.serverTool == nil || b.toolSearch != nil || b.webSearch != nil || b.containerUpload != nil || b.redactedThinking != nil || b.text != nil || b.thinking != nil || b.toolUse != nil || b.toolResult != nil {
			return errors.New("server tool use requires one variant")
		}
		return b.serverTool.Validate()
	}

	if b.kind != BlockToolSearchToolResult && b.toolSearch != nil {
		return errors.New("block has an unexpected tool-search-result variant")
	}
	if b.kind == BlockToolSearchToolResult {
		if b.toolSearch == nil || b.webSearch != nil || b.containerUpload != nil || b.redactedThinking != nil || b.text != nil || b.thinking != nil || b.toolUse != nil || b.toolResult != nil {
			return errors.New("tool search result requires one variant")
		}
		return b.toolSearch.Validate()
	}
	if b.kind != BlockWebSearchToolResult && b.webSearch != nil {
		return errors.New("block has an unexpected web-search-result variant")
	}
	if b.kind == BlockWebSearchToolResult {
		if b.webSearch == nil || b.containerUpload != nil || b.redactedThinking != nil || b.text != nil || b.thinking != nil || b.toolUse != nil || b.toolResult != nil {
			return errors.New("web search result requires one variant")
		}
		return b.webSearch.Validate()
	}
	if b.kind != BlockContainerUpload && b.containerUpload != nil {
		return errors.New("block has an unexpected container-upload variant")
	}
	if b.kind == BlockContainerUpload {
		if b.containerUpload == nil || b.redactedThinking != nil || b.text != nil || b.thinking != nil || b.toolUse != nil || b.toolResult != nil {
			return errors.New("container upload requires file_id and no other variant")
		}
		return nil
	}
	if b.kind != BlockRedactedThinking && b.redactedThinking != nil {
		return errors.New("block has an unexpected redacted-thinking variant")
	}
	switch b.kind {
	case BlockRedactedThinking:
		if b.redactedThinking == nil || b.text != nil || b.thinking != nil || b.toolUse != nil || b.toolResult != nil {
			return errors.New("redacted thinking requires data and no other variant")
		}
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
	case BlockTextEditorToolResult:
		return b.textEditor.marshal(control)
	case BlockBashExecutionToolResult:
		return b.bashExecution.marshal(control)
	case BlockCodeExecutionToolResult:
		return b.codeExecution.marshal(control)
	case BlockWebFetchToolResult:
		return b.webFetch.marshal(control)
	case BlockServerToolUse:
		return b.serverTool.marshal(control)
	case BlockToolSearchToolResult:
		return b.toolSearch.marshal(control)
	case BlockWebSearchToolResult:
		return b.webSearch.marshal(control)
	case BlockContainerUpload:
		v := b.containerUpload
		return json.Marshal(struct {
			Type         BlockKind                `json:"type"`
			FileID       string                   `json:"file_id"`
			Text         *wireOptional[string]    `json:"text,omitempty"`
			Citations    *wireOptional[Citations] `json:"citations,omitempty"`
			ParsedOutput *wireOptional[string]    `json:"parsed_output,omitempty"`
			CacheControl *CacheControl            `json:"cache_control,omitempty"`
		}{BlockContainerUpload, v.FileID, optionalWireValue[string](nil, v.textNull), optionalWireValue[Citations](nil, v.citationsNull), optionalWireValue[string](nil, v.parsedOutputNull), control})
	case BlockRedactedThinking:
		return json.Marshal(struct {
			Type BlockKind `json:"type"`
			RedactedThinkingBlock
			CacheControl *CacheControl `json:"cache_control,omitempty"`
		}{BlockRedactedThinking, *b.redactedThinking, control})
	case BlockText:
		return json.Marshal(struct {
			Type         BlockKind             `json:"type"`
			Text         string                `json:"text"`
			Citations    *Citations            `json:"citations,omitempty"`
			ParsedOutput *wireOptional[string] `json:"parsed_output,omitempty"`
			CacheControl *CacheControl         `json:"cache_control,omitempty"`
		}{BlockText, b.text.Text, b.text.citationWire(), b.text.parsedOutputWire(), control})
	case BlockThinking:
		return json.Marshal(struct {
			Type BlockKind `json:"type"`
			ThinkingBlock
			CacheControl *CacheControl `json:"cache_control,omitempty"`
		}{BlockThinking, *b.thinking, control})
	case BlockToolUse:
		var caller *wireOptional[ToolCaller]
		if b.callerPresent {
			caller = &wireOptional[ToolCaller]{value: b.toolUse.Caller}
		}
		return json.Marshal(struct {
			Type         BlockKind                 `json:"type"`
			ID           string                    `json:"id"`
			Name         ToolName                  `json:"name"`
			Input        ToolInput                 `json:"input"`
			Caller       *wireOptional[ToolCaller] `json:"caller,omitempty"`
			CacheControl *CacheControl             `json:"cache_control,omitempty"`
		}{BlockToolUse, b.toolUse.ID, b.toolUse.Name, b.toolUse.Input, caller, control})
	case BlockToolResult:
		return json.Marshal(struct {
			Type         BlockKind      `json:"type"`
			ToolUseID    string         `json:"tool_use_id"`
			Content      wireResultText `json:"content"`
			IsError      bool           `json:"is_error,omitempty"`
			CacheControl *CacheControl  `json:"cache_control,omitempty"`
		}{BlockToolResult, b.toolResult.ToolUseID, wireResultText(b.toolResult.Content), b.toolResult.IsError, control})
	default:
		return nil, fmt.Errorf("unsupported block type %q", b.kind)
	}
}

// decodeStrict is used only at a JSON boundary. Domain values never retain
// raw JSON or an open-ended map.
func decodeStrict[T any](data []byte, target *T) error { return decodeStrictMode(data, target, true) }

func decodeStrictMode[T any](data []byte, target *T, bounded bool) error {
	if bounded && len(data) > MaxWireBytes {
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
	next, err := decodeBlock(data, true)
	if err == nil {
		*b = next
	}
	return err
}

func decodeBlock(data []byte, bounded bool) (Block, error) {
	if bounded && len(data) > MaxWireBytes {
		return Block{}, fmt.Errorf("block exceeds %d bytes", MaxWireBytes)
	}
	var tag struct {
		Type BlockKind `json:"type"`
	}
	if err := json.Unmarshal(data, &tag); err != nil {
		return Block{}, err
	}
	var next Block
	switch tag.Type {
	case BlockTextEditorToolResult:
		result, err := decodeTextEditorBlock(data, bounded)
		if err != nil {
			return Block{}, err
		}
		next = Block{kind: BlockTextEditorToolResult, textEditor: &result}
	case BlockBashExecutionToolResult:
		result, err := decodeBashExecutionBlock(data, bounded)
		if err != nil {
			return Block{}, err
		}
		next = Block{kind: BlockBashExecutionToolResult, bashExecution: &result}
	case BlockCodeExecutionToolResult:
		result, err := decodeCodeExecutionBlock(data, bounded)
		if err != nil {
			return Block{}, err
		}
		next = Block{kind: BlockCodeExecutionToolResult, codeExecution: &result}
	case BlockWebFetchToolResult:
		result, err := decodeWebFetchBlock(data, bounded)
		if err != nil {
			return Block{}, err
		}
		next = Block{kind: BlockWebFetchToolResult, webFetch: &result}
	case BlockServerToolUse:
		result, err := decodeServerToolUseBlock(data, bounded)
		if err != nil {
			return Block{}, err
		}
		next = Block{kind: BlockServerToolUse, serverTool: &result}
	case BlockToolSearchToolResult:
		result, err := decodeToolSearchBlock(data, bounded)
		if err != nil {
			return Block{}, err
		}
		next = Block{kind: BlockToolSearchToolResult, toolSearch: &result}
	case BlockWebSearchToolResult:
		result, err := decodeWebSearchBlock(data, bounded)
		if err != nil {
			return Block{}, err
		}
		next = Block{kind: BlockWebSearchToolResult, webSearch: &result}
	case BlockContainerUpload:
		var wire struct {
			Type         BlockKind       `json:"type"`
			FileID       *string         `json:"file_id"`
			Text         json.RawMessage `json:"text"`
			Citations    json.RawMessage `json:"citations"`
			ParsedOutput json.RawMessage `json:"parsed_output"`
		}
		if err := decodeStrictMode(data, &wire, bounded); err != nil {
			return Block{}, err
		}
		if wire.FileID == nil {
			return Block{}, errors.New("container upload requires file_id")
		}
		next = NewContainerUploadBlock(*wire.FileID)
		for _, raw := range []json.RawMessage{wire.Text, wire.Citations, wire.ParsedOutput} {
			if len(raw) > 0 && string(bytes.TrimSpace(raw)) != "null" {
				return Block{}, errors.New("container stream metadata must be null")
			}
		}
		next.containerUpload.textNull = len(wire.Text) > 0
		next.containerUpload.citationsNull = len(wire.Citations) > 0
		next.containerUpload.parsedOutputNull = len(wire.ParsedOutput) > 0
	case BlockRedactedThinking:
		var wire struct {
			Type BlockKind `json:"type"`
			Data *string   `json:"data"`
		}
		if err := decodeStrictMode(data, &wire, bounded); err != nil {
			return Block{}, err
		}
		if wire.Data == nil {
			return Block{}, errors.New("redacted thinking requires data")
		}
		next = NewRedactedThinkingBlock(*wire.Data)
	case BlockText:
		var wire struct {
			Type         BlockKind       `json:"type"`
			Text         string          `json:"text"`
			Citations    json.RawMessage `json:"citations"`
			ParsedOutput json.RawMessage `json:"parsed_output"`
		}
		if err := decodeStrictMode(data, &wire, bounded); err != nil {
			return Block{}, err
		}
		citations, err := DecodeCitations(wire.Citations)
		if err != nil {
			return Block{}, err
		}
		next = NewTextBlockWithCitations(wire.Text, citations)
		if len(wire.ParsedOutput) > 0 {
			if string(bytes.TrimSpace(wire.ParsedOutput)) != "null" {
				return Block{}, errors.New("structured parsed output is unsupported")
			}
			next.text.parsedOutputNull = true
		}
	case BlockThinking:
		var wire struct {
			Type BlockKind `json:"type"`
			ThinkingBlock
		}
		if err := decodeStrictMode(data, &wire, bounded); err != nil {
			return Block{}, err
		}
		next = NewThinkingBlock(wire.Thinking, wire.Signature)
	case BlockToolUse:
		var wire struct {
			Type   BlockKind       `json:"type"`
			ID     string          `json:"id"`
			Name   ToolName        `json:"name"`
			Input  json.RawMessage `json:"input"`
			Caller json.RawMessage `json:"caller"`
		}
		if err := decodeStrictMode(data, &wire, bounded); err != nil {
			return Block{}, err
		}
		if len(wire.Input) == 0 {
			return Block{}, errors.New("tool_use requires input")
		}
		input, err := DecodeToolInput(wire.Name, wire.Input)
		if err != nil {
			return Block{}, err
		}
		var caller *ToolCaller
		if len(wire.Caller) > 0 && string(wire.Caller) != "null" {
			caller = &ToolCaller{}
			if err := json.Unmarshal(wire.Caller, caller); err != nil {
				return Block{}, err
			}
		}
		next = NewToolUseWithCaller(wire.ID, input, caller)
		next.callerPresent = len(wire.Caller) > 0
	case BlockToolResult:
		var wire struct {
			Type BlockKind `json:"type"`
			ToolResultBlock
		}
		if err := decodeStrictMode(data, &wire, bounded); err != nil {
			return Block{}, err
		}
		next = NewToolResult(wire.ToolUseID, wire.Content, wire.IsError)
	default:
		return Block{}, fmt.Errorf("unsupported block type %q", tag.Type)
	}
	if err := next.Validate(); err != nil {
		return Block{}, err
	}
	return next, nil
}

// Content is the provider's string-or-block-array union. An empty string is
// allowed; an empty block array is not.
type Content struct {
	plain  *string
	blocks []Block
}

func PlainContent(text string) Content { return Content{plain: &text} }

// Clone detaches every row, block and tool input without a wire-size limit.
// Forks copy valid accumulated histories which can exceed a single JSON frame.
func (c Content) Clone() Content {
	if c.plain != nil {
		return PlainContent(*c.plain)
	}
	blocks := make([]Block, len(c.blocks))
	for i, b := range c.blocks {
		blocks[i] = b
		if b.text != nil {
			v := *b.text
			blocks[i].text = &v
		}
		if b.thinking != nil {
			v := *b.thinking
			blocks[i].thinking = &v
		}
		if b.textEditor != nil {
			v := b.textEditor.clone()
			blocks[i].textEditor = &v
		}
		if b.bashExecution != nil {
			v := b.bashExecution.clone()
			blocks[i].bashExecution = &v
		}
		if b.codeExecution != nil {
			v := b.codeExecution.clone()
			blocks[i].codeExecution = &v
		}
		if b.webFetch != nil {
			v := b.webFetch.clone()
			blocks[i].webFetch = &v
		}
		if b.serverTool != nil {
			v := b.serverTool.clone()
			blocks[i].serverTool = &v
		}
		if b.toolSearch != nil {
			v := b.toolSearch.clone()
			blocks[i].toolSearch = &v
		}
		if b.webSearch != nil {
			v := b.webSearch.clone()
			blocks[i].webSearch = &v
		}
		if b.containerUpload != nil {
			v := *b.containerUpload
			blocks[i].containerUpload = &v
		}
		if b.redactedThinking != nil {
			v := *b.redactedThinking
			blocks[i].redactedThinking = &v
		}
		if b.toolResult != nil {
			v := *b.toolResult
			blocks[i].toolResult = &v
		}
		if b.toolUse != nil {
			v := *b.toolUse
			v.Input = v.Input.clone()
			if v.Caller != nil {
				caller := *v.Caller
				v.Caller = &caller
			}
			blocks[i].toolUse = &v
		}
	}
	return Content{blocks: blocks}
}

// SameStorage detects replacement of an immutable transcript row without
// re-encoding its entire payload. Copies share storage; new content does not.
func (c Content) SameStorage(other Content) bool {
	if c.plain != nil || other.plain != nil {
		return c.plain == other.plain
	}
	if len(c.blocks) != len(other.blocks) {
		return false
	}
	if len(c.blocks) == 0 {
		return true
	}
	return &c.blocks[0] == &other.blocks[0]
}

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
	next, err := decodeContent(data, true)
	if err == nil {
		*c = next
	}
	return err
}

func decodeContent(data []byte, bounded bool) (Content, error) {
	if bounded && len(data) > MaxWireBytes {
		return Content{}, fmt.Errorf("content exceeds %d bytes", MaxWireBytes)
	}
	var next Content
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return Content{}, errors.New("missing content")
	}
	switch trimmed[0] {
	case '"':
		var plain string
		if err := decodeStrictMode(data, &plain, bounded); err != nil {
			return Content{}, err
		}
		next = PlainContent(plain)
	case '[':
		var rows []json.RawMessage
		if err := decodeStrictMode(data, &rows, bounded); err != nil {
			return Content{}, err
		}
		blocks := make([]Block, 0, len(rows))
		for _, row := range rows {
			block, err := decodeBlock(row, bounded)
			if err != nil {
				return Content{}, err
			}
			blocks = append(blocks, block)
		}
		next = BlockContent(blocks...)
	default:
		return Content{}, errors.New("content must be a string or block array")
	}
	if err := next.Validate(); err != nil {
		return Content{}, err
	}
	return next, nil
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
	next, err := decodeMessage(data, true)
	if err == nil {
		*m = next
	}
	return err
}

// DecodeStoredMessage decodes a known historical message without the network-frame
// size cap. Tool input decoding retains its separate admission contract. Reading
// this value grants no actor, permissions or live execution authority.
func DecodeStoredMessage(data []byte) (Message, error) { return decodeMessage(data, false) }

func decodeMessage(data []byte, bounded bool) (Message, error) {
	if bounded && len(data) > MaxWireBytes {
		return Message{}, fmt.Errorf("message exceeds %d bytes", MaxWireBytes)
	}
	var wire struct {
		Role    Role            `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	if err := decodeStrictMode(data, &wire, bounded); err != nil {
		return Message{}, err
	}
	content, err := decodeContent(wire.Content, bounded)
	if err != nil {
		return Message{}, err
	}
	next := Message{Role: wire.Role, Content: content}
	if err := next.Validate(); err != nil {
		return Message{}, err
	}
	return next, nil
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
