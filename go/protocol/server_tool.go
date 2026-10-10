package protocol

import (
	"encoding/json"
	"errors"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

// ServerToolName describes provider-owned observations, never client registration.
type ServerToolName string

const (
	ServerWebSearch               ServerToolName = "web_search"
	ServerWebFetch                ServerToolName = "web_fetch"
	ServerCodeExecution           ServerToolName = "code_execution"
	ServerBashCodeExecution       ServerToolName = "bash_code_execution"
	ServerTextEditorCodeExecution ServerToolName = "text_editor_code_execution"
	ServerToolSearchRegex         ServerToolName = "tool_search_tool_regex"
	ServerToolSearchBM25          ServerToolName = "tool_search_tool_bm25"
)

// ServerToolInput retains finite SDK input observations, including unsupported roots. It has no
// declared input fields and carries no client ToolInput or execution authority.
// Its recursive value is immutable, with explicit scalar/array/object variants.
type ServerToolInput struct{ value jsonvalue.Value }

func DecodeServerToolInput(data []byte) (ServerToolInput, error) {
	return decodeServerToolInput(data, true)
}
func decodeServerToolInput(data []byte, bounded bool) (ServerToolInput, error) {
	if bounded && len(data) > MaxWireBytes {
		return ServerToolInput{}, errors.New("server tool input exceeds wire bound")
	}
	value, err := jsonvalue.Decode(string(data))
	if err != nil {
		return ServerToolInput{}, err
	}
	if _, err := value.MarshalJSON(); err != nil {
		return ServerToolInput{}, err
	}
	return ServerToolInput{value: value}, nil
}
func (input ServerToolInput) Validate() error {
	_, err := input.value.MarshalJSON()
	return err
}
func (input ServerToolInput) MarshalJSON() ([]byte, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(input.value)
}
func (input *ServerToolInput) UnmarshalJSON(data []byte) error {
	next, err := DecodeServerToolInput(data)
	if err == nil {
		*input = next
	}
	return err
}

// ServerToolUseBlock retains provider metadata without entering client dispatch.
type ServerToolUseBlock struct {
	ID            string
	Name          ServerToolName
	Input         ServerToolInput
	Caller        *ToolCaller
	callerPresent bool
	cache         *CacheControl
	extra         jsonvalue.Value
}

func (r ServerToolUseBlock) clone() ServerToolUseBlock {
	r.Caller = copyMetadataPointer(r.Caller)
	r.cache = copyMetadataPointer(r.cache)
	return r
}
func (r ServerToolUseBlock) Validate() error {
	if err := r.Input.Validate(); err != nil {
		return err
	}
	if r.Caller != nil {
		if err := r.Caller.Validate(); err != nil {
			return err
		}
	}
	if r.cache != nil {
		return r.cache.Validate()
	}
	return nil
}
func (r ServerToolUseBlock) WithSDKDefaults() ServerToolUseBlock {
	r = r.clone()
	r.callerPresent = true
	return r
}

// WithInput replaces only accumulated input, retaining caller and extensions.
func (r ServerToolUseBlock) WithInput(input ServerToolInput) ServerToolUseBlock {
	r = r.clone()
	r.Input = input
	return r
}
func (r ServerToolUseBlock) marshal(control *CacheControl) ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	if control == nil {
		control = r.cache
	}
	return marshalMetadataObject(struct {
		Type   BlockKind                 `json:"type"`
		ID     string                    `json:"id"`
		Name   ServerToolName            `json:"name"`
		Input  ServerToolInput           `json:"input"`
		Caller *wireOptional[ToolCaller] `json:"caller,omitempty"`
		Cache  *CacheControl             `json:"cache_control,omitempty"`
	}{BlockServerToolUse, r.ID, r.Name, r.Input, optionalWireValue(r.Caller, r.callerPresent), control}, r.extra)
}
func (r ServerToolUseBlock) MarshalJSON() ([]byte, error) { return r.marshal(nil) }
func decodeServerToolUseBlock(data []byte, bounded bool) (ServerToolUseBlock, error) {
	var wire struct {
		Type   *BlockKind
		ID     *string
		Name   *ServerToolName
		Input  json.RawMessage
		Caller json.RawMessage
		Cache  *CacheControl `json:"cache_control"`
	}
	_, extra, err := decodeMetadataObject(data, &wire, []string{"type", "id", "name", "input", "caller", "cache_control"}, bounded)
	if err != nil {
		return ServerToolUseBlock{}, err
	}
	if wire.Type == nil || *wire.Type != BlockServerToolUse || wire.ID == nil || wire.Name == nil || len(wire.Input) == 0 {
		return ServerToolUseBlock{}, errors.New("server tool use requires type, id, name and input")
	}
	input, err := decodeServerToolInput(wire.Input, bounded)
	if err != nil {
		return ServerToolUseBlock{}, err
	}
	var caller *ToolCaller
	if len(wire.Caller) > 0 && string(wire.Caller) != "null" {
		decoded, err := decodeToolCaller(wire.Caller, bounded)
		if err != nil {
			return ServerToolUseBlock{}, err
		}
		caller = &decoded
	}
	next := ServerToolUseBlock{ID: *wire.ID, Name: *wire.Name, Input: input, Caller: caller, callerPresent: len(wire.Caller) > 0, cache: wire.Cache, extra: extra}
	return next, next.Validate()
}
func (r *ServerToolUseBlock) UnmarshalJSON(data []byte) error {
	next, err := decodeServerToolUseBlock(data, true)
	if err == nil {
		*r = next
	}
	return err
}
func (r ServerToolUseBlock) Block() Block {
	r = r.clone()
	return Block{kind: BlockServerToolUse, serverTool: &r}
}
func NewServerToolUseBlock(id string, name ServerToolName, input ServerToolInput) Block {
	return (ServerToolUseBlock{ID: id, Name: name, Input: input}).Block()
}
func (b Block) ServerToolUse() (ServerToolUseBlock, bool) {
	if b.kind != BlockServerToolUse || b.serverTool == nil {
		return ServerToolUseBlock{}, false
	}
	return b.serverTool.clone(), true
}
