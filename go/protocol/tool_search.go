package protocol

import (
	"encoding/json"
	"errors"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

type ToolSearchErrorCode string

const (
	ToolSearchInvalidInput    ToolSearchErrorCode = "invalid_tool_input"
	ToolSearchUnavailable     ToolSearchErrorCode = "unavailable"
	ToolSearchTooManyRequests ToolSearchErrorCode = "too_many_requests"
	ToolSearchTimeExceeded    ToolSearchErrorCode = "execution_time_exceeded"
)

// Tool references describe provider observations; they do not register tools.
type ToolReference struct {
	Name  ToolName
	extra jsonvalue.Value
}

func (r ToolReference) MarshalJSON() ([]byte, error) {
	return marshalMetadataObject(struct {
		Type string   `json:"type"`
		Name ToolName `json:"tool_name"`
	}{"tool_reference", r.Name}, r.extra)
}
func decodeToolReference(data []byte, bounded bool) (ToolReference, error) {
	var wire struct {
		Type *string
		Name *ToolName `json:"tool_name"`
	}
	_, extra, err := decodeMetadataObject(data, &wire, []string{"type", "tool_name"}, bounded)
	if err != nil {
		return ToolReference{}, err
	}
	if wire.Type == nil || *wire.Type != "tool_reference" || wire.Name == nil {
		return ToolReference{}, errors.New("tool reference requires type and tool_name")
	}
	return ToolReference{Name: *wire.Name, extra: extra}, nil
}
func (r *ToolReference) UnmarshalJSON(data []byte) error {
	next, err := decodeToolReference(data, true)
	if err == nil {
		*r = next
	}
	return err
}

type ToolSearchResults struct {
	References []ToolReference
	extra      jsonvalue.Value
}

func (r ToolSearchResults) clone() ToolSearchResults {
	r.References = append([]ToolReference{}, r.References...)
	return r
}
func (r ToolSearchResults) MarshalJSON() ([]byte, error) {
	refs := r.clone().References
	return marshalMetadataObject(struct {
		Type       string          `json:"type"`
		References []ToolReference `json:"tool_references"`
	}{"tool_search_tool_search_result", refs}, r.extra)
}
func decodeToolSearchResults(data []byte, bounded bool) (ToolSearchResults, error) {
	var wire struct {
		Type       *string
		References *[]json.RawMessage `json:"tool_references"`
	}
	_, extra, err := decodeMetadataObject(data, &wire, []string{"type", "tool_references"}, bounded)
	if err != nil {
		return ToolSearchResults{}, err
	}
	if wire.Type == nil || *wire.Type != "tool_search_tool_search_result" || wire.References == nil {
		return ToolSearchResults{}, errors.New("tool search success requires type and tool_references")
	}
	refs := []ToolReference{}
	for _, raw := range *wire.References {
		ref, err := decodeToolReference(raw, bounded)
		if err != nil {
			return ToolSearchResults{}, err
		}
		refs = append(refs, ref)
	}
	return ToolSearchResults{References: refs, extra: extra}, nil
}
func (r *ToolSearchResults) UnmarshalJSON(data []byte) error {
	next, err := decodeToolSearchResults(data, true)
	if err == nil {
		*r = next
	}
	return err
}

type ToolSearchError struct {
	Code        ToolSearchErrorCode
	Message     *string
	messageNull bool
	extra       jsonvalue.Value
}

func (e ToolSearchError) clone() ToolSearchError {
	e.Message = copyMetadataPointer(e.Message)
	return e
}
func (e ToolSearchError) MarshalJSON() ([]byte, error) {
	return marshalMetadataObject(struct {
		Type    string                `json:"type"`
		Code    ToolSearchErrorCode   `json:"error_code"`
		Message *wireOptional[string] `json:"error_message,omitempty"`
	}{"tool_search_tool_result_error", e.Code, optionalWireValue(e.Message, e.messageNull)}, e.extra)
}
func decodeToolSearchError(data []byte, bounded bool) (ToolSearchError, error) {
	var wire struct {
		Type    *string
		Code    *ToolSearchErrorCode `json:"error_code"`
		Message *string              `json:"error_message"`
	}
	value, extra, err := decodeMetadataObject(data, &wire, []string{"type", "error_code", "error_message"}, bounded)
	if err != nil {
		return ToolSearchError{}, err
	}
	if wire.Type == nil || *wire.Type != "tool_search_tool_result_error" || wire.Code == nil {
		return ToolSearchError{}, errors.New("tool search error requires type and error_code")
	}
	message, present := value.Lookup("error_message")
	return ToolSearchError{Code: *wire.Code, Message: wire.Message, messageNull: present && message.Kind() == jsonvalue.Null, extra: extra}, nil
}
func (e *ToolSearchError) UnmarshalJSON(data []byte) error {
	next, err := decodeToolSearchError(data, true)
	if err == nil {
		*e = next
	}
	return err
}

type ToolSearchContentKind string

const (
	ToolSearchSuccess ToolSearchContentKind = "results"
	ToolSearchFailure ToolSearchContentKind = "error"
)

type ToolSearchContent struct {
	kind    ToolSearchContentKind
	results *ToolSearchResults
	failure *ToolSearchError
}

func NewToolSearchResults(refs []ToolReference) ToolSearchContent {
	result := (ToolSearchResults{References: refs}).clone()
	return ToolSearchContent{kind: ToolSearchSuccess, results: &result}
}
func NewToolSearchFailure(code ToolSearchErrorCode, message *string) ToolSearchContent {
	failure := (ToolSearchError{Code: code, Message: message, messageNull: message == nil}).clone()
	return ToolSearchContent{kind: ToolSearchFailure, failure: &failure}
}
func (c ToolSearchContent) Kind() ToolSearchContentKind { return c.kind }
func (c ToolSearchContent) Results() (ToolSearchResults, bool) {
	if c.kind != ToolSearchSuccess || c.results == nil {
		return ToolSearchResults{}, false
	}
	return c.results.clone(), true
}
func (c ToolSearchContent) Error() (ToolSearchError, bool) {
	if c.kind != ToolSearchFailure || c.failure == nil {
		return ToolSearchError{}, false
	}
	return c.failure.clone(), true
}
func (c ToolSearchContent) clone() ToolSearchContent {
	if c.results != nil {
		result := c.results.clone()
		c.results = &result
	}
	if c.failure != nil {
		failure := c.failure.clone()
		c.failure = &failure
	}
	return c
}
func (c ToolSearchContent) Validate() error {
	if c.kind == ToolSearchSuccess && c.results != nil && c.failure == nil {
		return nil
	}
	if c.kind == ToolSearchFailure && c.failure != nil && c.results == nil {
		return nil
	}
	return errors.New("tool search content requires exactly one results/error variant")
}
func (c ToolSearchContent) MarshalJSON() ([]byte, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	if c.kind == ToolSearchSuccess {
		return json.Marshal(c.results)
	}
	return json.Marshal(c.failure)
}
func decodeToolSearchContent(data []byte, bounded bool) (ToolSearchContent, error) {
	var tag struct{ Type string }
	if err := json.Unmarshal(data, &tag); err != nil {
		return ToolSearchContent{}, err
	}
	switch tag.Type {
	case "tool_search_tool_search_result":
		result, err := decodeToolSearchResults(data, bounded)
		if err != nil {
			return ToolSearchContent{}, err
		}
		return ToolSearchContent{kind: ToolSearchSuccess, results: &result}, nil
	case "tool_search_tool_result_error":
		failure, err := decodeToolSearchError(data, bounded)
		if err != nil {
			return ToolSearchContent{}, err
		}
		return ToolSearchContent{kind: ToolSearchFailure, failure: &failure}, nil
	default:
		return ToolSearchContent{}, errors.New("unsupported tool search content type")
	}
}
func (c *ToolSearchContent) UnmarshalJSON(data []byte) error {
	next, err := decodeToolSearchContent(data, true)
	if err == nil {
		*c = next
	}
	return err
}

type ToolSearchToolResultBlock struct {
	ToolUseID                                 string
	Content                                   ToolSearchContent
	textNull, citationsNull, parsedOutputNull bool
	cache                                     *CacheControl
	extra                                     jsonvalue.Value
}

func (r ToolSearchToolResultBlock) clone() ToolSearchToolResultBlock {
	r.Content = r.Content.clone()
	r.cache = copyMetadataPointer(r.cache)
	return r
}
func (r ToolSearchToolResultBlock) Validate() error {
	if err := r.Content.Validate(); err != nil {
		return err
	}
	if r.cache != nil {
		return r.cache.Validate()
	}
	return nil
}
func (r ToolSearchToolResultBlock) WithSDKDefaults(streamed bool) ToolSearchToolResultBlock {
	r = r.clone()
	if r.Content.failure != nil {
		r.Content.failure.messageNull = r.Content.failure.Message == nil
	}
	if streamed {
		r.textNull, r.citationsNull, r.parsedOutputNull = true, true, true
	}
	return r
}
func (r ToolSearchToolResultBlock) marshal(control *CacheControl) ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	if control == nil {
		control = r.cache
	}
	return marshalMetadataObject(struct {
		Type         BlockKind                `json:"type"`
		ToolUseID    string                   `json:"tool_use_id"`
		Content      ToolSearchContent        `json:"content"`
		Text         *wireOptional[string]    `json:"text,omitempty"`
		Citations    *wireOptional[Citations] `json:"citations,omitempty"`
		ParsedOutput *wireOptional[string]    `json:"parsed_output,omitempty"`
		Cache        *CacheControl            `json:"cache_control,omitempty"`
	}{BlockToolSearchToolResult, r.ToolUseID, r.Content, optionalWireValue[string](nil, r.textNull), optionalWireValue[Citations](nil, r.citationsNull), optionalWireValue[string](nil, r.parsedOutputNull), control}, r.extra)
}
func (r ToolSearchToolResultBlock) MarshalJSON() ([]byte, error) { return r.marshal(nil) }
func decodeToolSearchBlock(data []byte, bounded bool) (ToolSearchToolResultBlock, error) {
	var wire struct {
		Type            *BlockKind
		ToolUseID       *string `json:"tool_use_id"`
		Content         json.RawMessage
		Text, Citations json.RawMessage
		ParsedOutput    json.RawMessage `json:"parsed_output"`
		Cache           *CacheControl   `json:"cache_control"`
	}
	_, extra, err := decodeMetadataObject(data, &wire, []string{"type", "tool_use_id", "content", "text", "citations", "parsed_output", "cache_control"}, bounded)
	if err != nil {
		return ToolSearchToolResultBlock{}, err
	}
	if wire.Type == nil || *wire.Type != BlockToolSearchToolResult || wire.ToolUseID == nil || len(wire.Content) == 0 {
		return ToolSearchToolResultBlock{}, errors.New("tool search result requires type, tool_use_id and content")
	}
	content, err := decodeToolSearchContent(wire.Content, bounded)
	if err != nil {
		return ToolSearchToolResultBlock{}, err
	}
	for _, raw := range []json.RawMessage{wire.Text, wire.Citations, wire.ParsedOutput} {
		if len(raw) > 0 && string(raw) != "null" {
			return ToolSearchToolResultBlock{}, errors.New("tool search stream metadata must be null")
		}
	}
	next := ToolSearchToolResultBlock{ToolUseID: *wire.ToolUseID, Content: content, textNull: len(wire.Text) > 0, citationsNull: len(wire.Citations) > 0, parsedOutputNull: len(wire.ParsedOutput) > 0, cache: wire.Cache, extra: extra}
	return next, next.Validate()
}
func (r *ToolSearchToolResultBlock) UnmarshalJSON(data []byte) error {
	next, err := decodeToolSearchBlock(data, true)
	if err == nil {
		*r = next
	}
	return err
}
func (r ToolSearchToolResultBlock) Block() Block {
	result := r.clone()
	return Block{kind: BlockToolSearchToolResult, toolSearch: &result}
}
func NewToolSearchToolResultBlock(toolUseID string, content ToolSearchContent) Block {
	return (ToolSearchToolResultBlock{ToolUseID: toolUseID, Content: content}).Block()
}
func (b Block) ToolSearchToolResult() (ToolSearchToolResultBlock, bool) {
	if b.kind != BlockToolSearchToolResult || b.toolSearch == nil {
		return ToolSearchToolResultBlock{}, false
	}
	return b.toolSearch.clone(), true
}
